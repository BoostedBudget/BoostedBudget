import type { CategoryHierarchyNode } from '@boostedbudget/api-client/models'
import { CASH_FLOW_COLORS, CATEGORY_COLORS } from '@/shared/charts'

/**
 * ─── Node budget (#1626) ────────────────────────────────────────────────────────────────────────
 *
 * Allocated explicitly rather than enforced by a single global cap, so the floor is PROVABLE.
 * A cap alone is not enough: with 32 income leaves plus an overflow node, 10 expense parents plus
 * an overflow, a hub and a savings node you are already at 46 before a single direct bucket, so
 * "cap the parents at 10" does not converge.
 */
export const INCOME_NODE_BUDGET = 12 // <= 11 leaves + 1 overflow
export const EXPENSE_NODE_BUDGET = 31 // everything under the hub
export const RESERVED_NODE_BUDGET = 2 // hub + exactly one of savings/deficit (mutually exclusive)
export const TOTAL_NODE_BUDGET = INCOME_NODE_BUDGET + EXPENSE_NODE_BUDGET + RESERVED_NODE_BUDGET

const MAX_INCOME_LEAVES = INCOME_NODE_BUDGET - 1
const CUSTOM_TOKEN = 'custom:'
/** Mirrors effectiveCategoryExpr's sentinel for a transaction with no primary category. */
const UNCATEGORIZED_KEY = 'UNCATEGORIZED'

export type SankeyViewMode =
  | { kind: 'parents' }
  | { kind: 'expanded' }
  | { kind: 'focus'; key: string }

/**
 * What clicking a node does. A single `navigable` boolean cannot express three destinations, and
 * the third one — "nothing" — is not a degenerate case: a direct bucket or an overflow node has no
 * expressible transactions filter at all, so it must never claim to navigate.
 */
export type NodeAction =
  | { kind: 'focus'; key: string }
  | {
    kind: 'navigate'
    pfcPrimaries?: string[]
    pfcDetaileds?: string[]
    customCategoryIds?: string[]
    /**
     * #1626 Phase 2. The uncategorized sentinel has no category value to send, so it navigates by
     * its own flag. Deliberately a separate field rather than a magic member of pfcPrimaries: the
     * server ORs a dedicated `uncategorized` predicate, and sending 'UNCATEGORIZED' as a category
     * would look like a working filter while matching zero rows.
     */
    uncategorized?: true
  }
  | { kind: 'none' }

export type SankeyNodeKind = 'income' | 'expense' | 'hub' | 'savings' | 'deficit' | 'other' | 'direct'

export interface SankeyNode {
  id: string
  name: string
  itemStyle: { color: string }
  nodeKind: SankeyNodeKind
  action: NodeAction
  /**
   * The node's REAL depth in the category hierarchy, 1-based. Carried explicitly because the
   * income fan is flattened — every income node's id is `in:l1:*` whatever its true depth — so
   * deriving depth from the id would report every income navigation as level 1. Used only by the
   * navigation analytics event, which must carry structure and never a category name or id.
   */
  depth: number
}

export interface SankeyLink {
  source: string
  target: string
  value: number
}

export interface SankeyData {
  nodes: SankeyNode[]
  links: SankeyLink[]
  /** Max of the FULL income and expense sums — the true hub throughput, before any truncation. */
  hubTotal: number
}

const isCustom = (key: string) => key.startsWith(CUSTOM_TOKEN)
const stripToken = (key: string) => key.slice(CUSTOM_TOKEN.length)
const isPFCDetailed = (key: string, parentKey: string) => key.startsWith(`${parentKey}_`)

/**
 * A real category leaf's filter. A PFC detailed sends `pfc_detaileds` ONLY — adding its primary
 * would widen the result to the whole primary, so the list would not match the node just clicked.
 */
function navigateAction(key: string, parentKey: string | undefined): NodeAction {
  // UNCATEGORIZED is a sentinel this analytics layer SYNTHESISES for a transaction with no primary
  // at all; it is not a value any column holds (effective_pfc_primary is NULL for those rows), so
  // `pfc_primaries=UNCATEGORIZED` would match zero rows and the list would contradict the node the
  // user just clicked. #1626 Phase 2 added a dedicated `uncategorized` filter whose predicate is
  // `category_override_kind IS NULL AND pfc_primary IS NULL` — the sentinel's own definition — so
  // the node navigates by that flag instead of by a category value it does not have.
  if (key === UNCATEGORIZED_KEY) return { kind: 'navigate', uncategorized: true }
  if (isCustom(key)) return { kind: 'navigate', customCategoryIds: [stripToken(key)] }
  if (parentKey && !isCustom(parentKey) && isPFCDetailed(key, parentKey)) {
    return { kind: 'navigate', pfcDetaileds: [key] }
  }
  return { kind: 'navigate', pfcPrimaries: [key] }
}

/** Rendered tree: a pruned copy carrying whatever a degradation rung left visible. */
interface Rendered {
  key: string
  display: string
  total: number
  /** Own spend PLUS anything collapsed into this node — what keeps flow conserving. */
  direct: number
  children: Rendered[]
  /** True when this node had children before a rung hid them: it still FOCUSES rather than navigates. */
  hadChildren: boolean
}

/**
 * prune renders a subtree to at most `depth` further levels, keeping at most `maxChildren` children
 * per node. Anything not rendered is COLLAPSED into an ancestor rather than dropped — truncation
 * must never lose money, or the hub stops balancing.
 */
function prune(n: CategoryHierarchyNode, depth: number, maxChildren: number): Rendered {
  const kids = n.children ?? []
  const base: Rendered = {
    key: n.category,
    display: n.display,
    total: n.total,
    direct: n.direct_total,
    children: [],
    hadChildren: kids.length > 0,
  }
  if (depth <= 0 || kids.length === 0) {
    // Everything beneath collapses into this node's own direct total.
    base.direct = n.total
    return base
  }

  const sorted = [...kids].sort((a, b) => b.total - a.total)
  const kept = sorted.slice(0, maxChildren)
  const dropped = sorted.slice(maxChildren)

  base.children = kept.map(k => prune(k, depth - 1, maxChildren))
  if (dropped.length > 0) {
    const overflow = dropped.reduce((s, d) => s + d.total, 0)
    base.children.push({
      key: `__other__:${n.category}`,
      display: `Other (${dropped.length})`,
      total: overflow,
      direct: overflow,
      children: [],
      hadChildren: false,
    })
  }
  return base
}

/** Renders the focus view: the focused node's ancestor path, its subtree, siblings collapsed. */
function pruneForFocus(roots: CategoryHierarchyNode[], focusKey: string, depth: number, maxChildren: number): Rendered[] | null {
  const path = findPath(roots, focusKey)
  if (!path) return null

  const build = (siblings: CategoryHierarchyNode[], level: number): Rendered[] => {
    const onPath = path[level]
    const out: Rendered[] = []
    let collapsed = 0
    let collapsedCount = 0

    for (const s of siblings) {
      if (s.category !== onPath.category) {
        // COLLAPSE IS TERMINAL: one replacement node per collapsed subtree, and traversal stops.
        // Descending into it as well would let the same subtree contribute twice.
        collapsed += s.total
        collapsedCount++
        continue
      }
      if (level === path.length - 1) {
        out.push(prune(s, depth, maxChildren)) // the focused node: fan out its subtree
      } else {
        const kids = s.children ?? []
        const node: Rendered = {
          key: s.category, display: s.display, total: s.total,
          direct: s.direct_total, children: build(kids, level + 1), hadChildren: kids.length > 0,
        }
        out.push(node)
      }
    }
    if (collapsedCount > 0) {
      out.push({
        key: `__other__:level${level}`,
        display: `Other (${collapsedCount})`,
        total: collapsed, direct: collapsed, children: [], hadChildren: false,
      })
    }
    return out
  }

  return build(roots, 0)
}

function findPath(roots: CategoryHierarchyNode[], key: string): CategoryHierarchyNode[] | null {
  for (const r of roots) {
    if (r.category === key) return [r]
    const sub = findPath(r.children ?? [], key)
    if (sub) return [r, ...sub]
  }
  return null
}

/** Emits nodes and links for a rendered expense forest, and reports how many nodes it used. */
function emitExpenses(forest: Rendered[]): { nodes: SankeyNode[]; links: SankeyLink[] } {
  const nodes: SankeyNode[] = []
  const links: SankeyLink[] = []
  let colorIdx = 0

  const walk = (n: Rendered, parentId: string, ancestry: string[], parentKey?: string) => {
    const isOverflow = n.key.startsWith('__other__:')
    const id = isOverflow
      ? `out:other:${ancestry.join('|') || 'l1'}`
      : `out:l${ancestry.length + 1}:${[...ancestry, n.key].join('|')}`

    const action: NodeAction = isOverflow
      ? { kind: 'none' }
      : n.hadChildren
        // Focus-ness survives degradation: a parent whose children a rung hid must not flip to
        // navigate just because it currently renders as a leaf.
        ? { kind: 'focus', key: n.key }
        : navigateAction(n.key, parentKey)

    nodes.push({
      id,
      name: n.display,
      itemStyle: { color: CATEGORY_COLORS[colorIdx++ % CATEGORY_COLORS.length] },
      nodeKind: isOverflow ? 'other' : 'expense',
      action,
      depth: ancestry.length + 1,
    })
    links.push({ source: parentId, target: id, value: n.total })

    if (n.children.length === 0) return

    const nextAncestry = [...ancestry, n.key]
    for (const c of n.children) walk(c, id, nextAncestry, n.key)

    // A node with rendered children AND own spend needs somewhere to put that spend, or its
    // inflow exceeds the sum of its outflows.
    if (n.direct > 0) {
      const directId = `out:direct:${nextAncestry.join('|')}`
      nodes.push({
        id: directId,
        name: `${n.display} (direct)`,
        itemStyle: { color: CATEGORY_COLORS[colorIdx++ % CATEGORY_COLORS.length] },
        nodeKind: 'direct',
        action: { kind: 'none' },
        // A direct bucket sits alongside its parent's children, so it is one level deeper.
        depth: nextAncestry.length + 1,
      })
      links.push({ source: id, target: directId, value: n.direct })
    }
  }

  for (const root of forest) walk(root, 'hub', [])
  return { nodes, links }
}

/**
 * The degradation ladder. Each rung is re-counted; the first that fits wins. The floor is the
 * consolidated view, which renders one node per parent and therefore always fits.
 */
function renderExpenseFan(roots: CategoryHierarchyNode[], mode: SankeyViewMode) {
  if (mode.kind === 'focus') {
    for (const [depth, maxChildren] of [[2, 6], [1, 6], [1, 3], [0, 0]] as const) {
      const forest = pruneForFocus(roots, mode.key, depth, maxChildren)
      if (!forest) break // focused key no longer exists — fall through to the consolidated view
      const emitted = emitExpenses(forest)
      if (emitted.nodes.length <= EXPENSE_NODE_BUDGET) return emitted
    }
  }

  const rungs: Array<[depth: number, maxChildren: number, maxRoots: number]> =
    mode.kind === 'parents'
      ? [[0, 0, 10]]
      : [[2, 6, 30], [1, 6, 30], [1, 6, 10], [0, 0, 30], [0, 0, 10]]

  let last: { nodes: SankeyNode[]; links: SankeyLink[] } = { nodes: [], links: [] }
  for (const [depth, maxChildren, maxRoots] of rungs) {
    const sorted = [...roots].sort((a, b) => b.total - a.total)
    const kept = sorted.slice(0, maxRoots).map(r => prune(r, depth, maxChildren))
    const dropped = sorted.slice(maxRoots)
    if (dropped.length > 0) {
      const overflow = dropped.reduce((s, d) => s + d.total, 0)
      kept.push({
        key: '__other__:roots', display: `Other (${dropped.length})`,
        total: overflow, direct: overflow, children: [], hadChildren: false,
      })
    }
    last = emitExpenses(kept)
    if (last.nodes.length <= EXPENSE_NODE_BUDGET) return last
  }
  return last
}

/**
 * Income LEAVES, flattened depth-first.
 *
 * The endpoint returns a TREE for both directions, and production income is shaped
 * INCOME -> INCOME_WAGES / INCOME_DIVIDENDS. Rendering only the roots would collapse every source
 * into a single "Income" node — the exact opposite of the asymmetric default the endpoint
 * documents, which exists because income's useful distinction lives one level down.
 *
 * A node's own direct income becomes a leaf in its own right, so a parent that has both children
 * and direct receipts contributes all of its money exactly once.
 */
interface IncomeLeaf {
  key: string
  display: string
  total: number
  parentKey?: string
  /** REAL depth in the hierarchy, 1-based. The rendered ids are all `in:l1:*`; this is not. */
  depth: number
  /**
   * True for a parent's OWN receipts emitted as a leaf. Such a node shows only the parent's direct
   * money, but the only filter available for it is the parent's key — which matches every
   * descendant too. So it must not navigate: showing a list bigger than the node is worse than
   * showing no list, because nothing tells the user it is the wrong one (#1626 Phase 2).
   */
  directOnly?: true
}

function incomeLeaves(nodes: CategoryHierarchyNode[], parentKey?: string, depth = 1): IncomeLeaf[] {
  const out: IncomeLeaf[] = []
  for (const n of nodes) {
    const kids = n.children ?? []
    if (kids.length === 0) {
      if (n.total > 0) out.push({ key: n.category, display: n.display, total: n.total, parentKey, depth })
      continue
    }
    out.push(...incomeLeaves(kids, n.category, depth + 1))
    if (n.direct_total > 0) {
      // The parent's own receipts, kept distinct from its children's — and inert, see directOnly.
      out.push({ key: n.category, display: n.display, total: n.direct_total, parentKey, depth, directOnly: true })
    }
  }
  return out
}

/** Income is capped in its own right: it is not restricted to a single primary and can fan wide. */
function renderIncomeFan(sources: CategoryHierarchyNode[]) {
  const nodes: SankeyNode[] = []
  const links: SankeyLink[] = []
  const sorted = incomeLeaves(sources).sort((a, b) => b.total - a.total)
  const kept = sorted.slice(0, MAX_INCOME_LEAVES)
  const dropped = sorted.slice(MAX_INCOME_LEAVES)

  kept.forEach((s, i) => {
    // Namespaced by index as well as key: a parent contributing BOTH children and its own direct
    // receipts appears twice under the same key, and two nodes cannot share an id.
    const id = `in:l1:${s.key}:${i}`
    nodes.push({
      id,
      name: s.display,
      itemStyle: { color: CASH_FLOW_COLORS.income },
      nodeKind: 'income',
      action: s.directOnly ? { kind: 'none' } : navigateAction(s.key, s.parentKey),
      depth: s.depth,
    })
    links.push({ source: id, target: 'hub', value: s.total })
  })

  if (dropped.length > 0) {
    const overflow = dropped.reduce((sum, d) => sum + d.total, 0)
    nodes.push({
      id: 'in:other',
      name: `Other income (${dropped.length})`,
      itemStyle: { color: CASH_FLOW_COLORS.income },
      nodeKind: 'other',
      action: { kind: 'none' },
      depth: 1,
    })
    links.push({ source: 'in:other', target: 'hub', value: overflow })
  }
  return { nodes, links }
}

export function buildSankeyData(
  income: CategoryHierarchyNode[],
  expenses: CategoryHierarchyNode[],
  mode: SankeyViewMode,
): SankeyData {
  const validIncome = income.filter(i => i.total > 0)
  const validExpenses = expenses.filter(e => e.total > 0)
  if (validIncome.length === 0 && validExpenses.length === 0) {
    return { nodes: [], links: [], hubTotal: 0 }
  }

  // Derived from the FULL row set, never the rendered subset: truncation collapses rather than
  // drops, so these stay equal, and computing them here keeps the two fans internally consistent
  // even if their two endpoints were fetched a moment apart.
  const incomeSum = incomeLeaves(validIncome).reduce((s, i) => s + i.total, 0)
  const expenseSum = validExpenses.reduce((s, e) => s + e.total, 0)
  const surplus = incomeSum - expenseSum

  const nodes: SankeyNode[] = [{
    id: 'hub',
    name: 'Total Income',
    itemStyle: { color: CASH_FLOW_COLORS.income },
    nodeKind: 'hub',
    action: { kind: 'none' },
    // Synthetic: not a category, so it has no place in the hierarchy.
    depth: 0,
  }]
  const links: SankeyLink[] = []

  const inFan = renderIncomeFan(validIncome)
  nodes.push(...inFan.nodes)
  links.push(...inFan.links)

  if (surplus < 0) {
    nodes.push({
      id: 'deficit',
      name: 'Drawn from Savings / Credit',
      itemStyle: { color: CASH_FLOW_COLORS.netNegative },
      nodeKind: 'deficit',
      action: { kind: 'none' },
      // Synthetic: not a category, so it has no place in the hierarchy.
      depth: 0,
    })
    links.push({ source: 'deficit', target: 'hub', value: -surplus })
  }

  const outFan = renderExpenseFan(validExpenses, mode)
  nodes.push(...outFan.nodes)
  links.push(...outFan.links)

  if (surplus > 0) {
    nodes.push({
      id: 'savings',
      name: 'Savings',
      itemStyle: { color: CASH_FLOW_COLORS.netPositive },
      nodeKind: 'savings',
      action: { kind: 'none' },
      // Synthetic: not a category, so it has no place in the hierarchy.
      depth: 0,
    })
    links.push({ source: 'hub', target: 'savings', value: surplus })
  }

  return { nodes, links, hubTotal: Math.max(incomeSum, expenseSum) }
}
