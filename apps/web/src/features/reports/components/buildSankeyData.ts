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
   * deriving depth from the id would report every income navigation as level 1. Consumed by the
   * navigation analytics events (`CashFlowSankeyChart.tsx`), which must carry structure and never a
   * category name or id.
   *
   * ⚠ MUST NEVER BE NAMED `depth`. ECharts' sankey series reads a node-data field literally named
   * `depth` as the LAYOUT LAYER — "the layer of the current node… starting from 0" per its own
   * docs — not as arbitrary payload. This app once shipped a field named `depth` here that existed
   * purely for analytics, and because `CashFlowSankeyChart.tsx` passed the internal node objects
   * straight through to ECharts (`data: sankeyData.nodes as any`), ECharts silently adopted it as
   * the render layer: synthetic nodes (depth 0) pinned to the far left, income leaves (their real
   * category depth, ≥1) scattered among the expense layers on the right. The links were always
   * correct; only this name collision inverted the visual layout. See
   * `.superpowers/sdd/brainstorming-writing-plans-using-super-fancy-melody/sankey-root-cause.md`.
   * The chart component must also project this object down to `id`/`name`/`itemStyle` before
   * handing it to ECharts, so no internal field — this one included — can leak into chart config.
   */
  categoryDepth: number
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
  /**
   * Which rung rendered the expense fan — an index into `EXPENSE_LADDER` or into `FOCUS_LADDER`,
   * disambiguated by `expenseRungLadder`. Undefined only for an empty chart, which renders no fan
   * at all.
   *
   * ⚠ The previous version of this comment said the field was "undefined ... when the
   * focus-specific ladder rendered the fan instead". It never was: `buildSankeyData` assigns
   * `outFan.rungIndex` unconditionally, so a focus render reported its own ladder's index here with
   * nothing to say so. That ambiguity is what `expenseRungLadder` now removes (#1626 gap G10).
   *
   * DIAGNOSTIC ONLY (#1626 Phase 2, item 3): both fields exist so a test can name the rung that ran
   * without inferring it from output shape — several rungs can produce output consistent with a
   * small tree, so inference can silently "cover" a rung that never actually ran. No production
   * code may branch on either value.
   */
  expenseRungIndex?: number
  /** Which ladder `expenseRungIndex` indexes. DIAGNOSTIC ONLY, see above. */
  expenseRungLadder?: 'root' | 'focus'
}

/**
 * A synthetic node with no place in the category hierarchy (hub, deficit, savings): inert
 * (`action: { kind: 'none' }`) and depth 0, since it names no category.
 */
function syntheticNode(id: string, name: string, color: string, nodeKind: SankeyNodeKind): SankeyNode {
  return { id, name, itemStyle: { color }, nodeKind, action: { kind: 'none' }, categoryDepth: 0 }
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
 * An "Other (N)" overflow bucket: a leaf standing in for whatever a degradation rung collapsed.
 * total === direct always — an overflow bucket never has rendered children of its own to owe money
 * to, so the whole collapsed amount is its own direct spend.
 */
function overflowNode(key: string, count: number, amount: number): Rendered {
  return { key, display: `Other (${count})`, total: amount, direct: amount, children: [], hadChildren: false }
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
    base.children.push(overflowNode(`__other__:${n.category}`, dropped.length, overflow))
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
      out.push(overflowNode(`__other__:level${level}`, collapsedCount, collapsed))
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
      categoryDepth: ancestry.length + 1,
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
        categoryDepth: nextAncestry.length + 1,
      })
      links.push({ source: id, target: directId, value: n.direct })
    }
  }

  for (const root of forest) walk(root, 'hub', [])
  return { nodes, links }
}

/**
 * The root-mode degradation ladder, used by the 'expanded' view and as the fallback when a focus
 * view's key no longer exists (see the `mode.kind === 'focus'` fallthrough below). Exported so a
 * test can name a rung by INDEX rather than restating this table (#1626 Phase 2, item 3): a
 * hand-typed rung index in a test would silently point at the wrong rung the moment this table
 * changes, and nothing would say so.
 *
 * EVERY rung here is reachable, and the rung sweep in buildSankeyData.test.ts asserts exactly that
 * with no exclusions. A sixth rung, [0, 0, 10], used to sit after the floor and could never run:
 * [0, 0, 30] depth-0-collapses every kept root to one node and adds at most one overflow node, so
 * it renders at most maxRoots + 1 === EXPENSE_NODE_BUDGET nodes and therefore always fits. It was
 * deleted rather than kept as documentation of a degradation step that cannot happen.
 *
 * ⚠ If you raise EXPENSE_NODE_BUDGET or change the floor's maxRoots, the floor stops being
 * guaranteed to fit and renderExpenseFan falls back to its last-computed result. That is safe (it
 * still returns a graph) but it is no longer a ladder — add a real floor instead.
 */
export const EXPENSE_LADDER: Array<[depth: number, maxChildren: number, maxRoots: number]> =
  [[2, 6, 30], [1, 6, 30], [1, 6, 10], [0, 0, 30]]

/**
 * The FOCUS ladder — the rungs a focus view degrades through, `[depth, maxChildren]` for the
 * focused node's own subtree. It has no `maxRoots`: a focus view renders the ancestor path and
 * collapses every sibling to one node per level (see `pruneForFocus`), so the root count is not a
 * dimension it can trade away.
 *
 * EXPORTED for the same reason `EXPENSE_LADDER` is (#1626 gap G10): a test must be able to sweep the
 * ladder and assert it exercised EVERY rung without hand-typing a rung index — an index typed into
 * a test is a claim about the table that stops being true the moment the table changes, and several
 * rungs produce output consistent with a small tree, so the rung that ran cannot be inferred from
 * the shape of the result. Pair it with `SankeyData.expenseRungLadder`, which says which of the two
 * ladders the index belongs to.
 *
 * ⚠ IT HAD TWO MORE RUNGS, `[1, 3]` and `[0, 0]`, AND NEITHER COULD EVER RUN — deleted by #1626 gap
 * G10 for the same reason EXPENSE_LADDER's fifth rung was (see its comment): a documented
 * degradation step that cannot happen is a lie about the ladder, and keeping it would force the
 * coverage sweep to carry a permanent exclusion, which is what let the root ladder's dead rung
 * survive unnoticed. The arithmetic: `[1, 6]` renders the focused node, at most 6 children plus one
 * overflow node, and one `(direct)` node — at most 9 — and each ancestor LEVEL above it adds the
 * on-path node, one collapsed-siblings node and at most one `(direct)` node. The API's tree is
 * three levels deep (`custom_category_single_parent` plus
 * `enforce_custom_category_two_levels`; see analytics_category_path.go), so the whole focus view
 * is at most ~18 nodes against an EXPENSE_NODE_BUDGET of 31: `[1, 6]` always fits and nothing after
 * it is reachable. `pruneForFocus` collapses siblings to ONE node per level, so no amount of
 * breadth changes that — only depth would, and depth is capped in the schema.
 *
 * ⚠ If EXPENSE_NODE_BUDGET shrinks, or the tree gets a fourth level, `[1, 6]` stops being a
 * guaranteed floor and a focus view that overflows falls through to EXPENSE_LADDER instead. That is
 * safe (it still returns a conserved graph) but it is no longer a focus view — add a real floor
 * rung then, and the sweep in buildSankeyData.test.ts will require it to be reachable.
 */
export const FOCUS_LADDER: Array<[depth: number, maxChildren: number]> =
  [[2, 6], [1, 6]]

interface ExpenseFanResult {
  nodes: SankeyNode[]
  links: SankeyLink[]
  /**
   * Index into `EXPENSE_LADDER` when `rungLadder` is 'root' (mode 'parents' or 'expanded', or a
   * focus view that fell through), and into `FOCUS_LADDER` when it is 'focus'. DIAGNOSTIC ONLY, see
   * `SankeyData.expenseRungIndex` — no production code may branch on either field.
   */
  rungIndex: number
  /**
   * Which ladder `rungIndex` indexes. Without it the index is ambiguous in focus mode — a focus
   * view can render from `FOCUS_LADDER` or fall through to `EXPENSE_LADDER`, and both report small
   * integers — so a sweep collecting indices could credit a focus rung for a fall-through and
   * report coverage it does not have.
   */
  rungLadder: 'root' | 'focus'
}

/**
 * The degradation ladder. Each rung is re-counted; the first that fits wins. The floor is the
 * consolidated view, which renders one node per parent and therefore always fits.
 */
function renderExpenseFan(roots: CategoryHierarchyNode[], mode: SankeyViewMode): ExpenseFanResult {
  if (mode.kind === 'focus') {
    for (let i = 0; i < FOCUS_LADDER.length; i++) {
      const [depth, maxChildren] = FOCUS_LADDER[i]
      const forest = pruneForFocus(roots, mode.key, depth, maxChildren)
      if (!forest) break // focused key no longer exists — fall through to the consolidated view
      const emitted = emitExpenses(forest)
      if (emitted.nodes.length <= EXPENSE_NODE_BUDGET) {
        return { ...emitted, rungIndex: i, rungLadder: 'focus' }
      }
    }
  }

  const rungs: Array<[depth: number, maxChildren: number, maxRoots: number]> =
    mode.kind === 'parents' ? [[0, 0, 10]] : EXPENSE_LADDER

  let last: { nodes: SankeyNode[]; links: SankeyLink[] } = { nodes: [], links: [] }
  let lastIndex = 0
  for (let i = 0; i < rungs.length; i++) {
    const [depth, maxChildren, maxRoots] = rungs[i]
    const sorted = [...roots].sort((a, b) => b.total - a.total)
    const kept = sorted.slice(0, maxRoots).map(r => prune(r, depth, maxChildren))
    const dropped = sorted.slice(maxRoots)
    if (dropped.length > 0) {
      const overflow = dropped.reduce((s, d) => s + d.total, 0)
      kept.push(overflowNode('__other__:roots', dropped.length, overflow))
    }
    last = emitExpenses(kept)
    lastIndex = i
    if (last.nodes.length <= EXPENSE_NODE_BUDGET) return { ...last, rungIndex: i, rungLadder: 'root' }
  }
  return { ...last, rungIndex: lastIndex, rungLadder: 'root' }
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
      categoryDepth: s.depth,
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
      categoryDepth: 1,
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

  const nodes: SankeyNode[] = [syntheticNode('hub', 'Total Income', CASH_FLOW_COLORS.income, 'hub')]
  const links: SankeyLink[] = []

  const inFan = renderIncomeFan(validIncome)
  nodes.push(...inFan.nodes)
  links.push(...inFan.links)

  if (surplus < 0) {
    // The app has no way to know whether a shortfall was funded from savings, credit, or an
    // overdraft — "Drawn from Savings / Credit" asserted a source we cannot observe. "Shortfall"
    // states only what we actually know: expenses exceeded income for the period.
    nodes.push(syntheticNode('deficit', 'Shortfall', CASH_FLOW_COLORS.netNegative, 'deficit'))
    links.push({ source: 'deficit', target: 'hub', value: -surplus })
  }

  const outFan = renderExpenseFan(validExpenses, mode)
  nodes.push(...outFan.nodes)
  links.push(...outFan.links)
  // The index and the ladder it belongs to travel TOGETHER (see ExpenseFanResult): a focus view
  // that rendered directly reports an index into FOCUS_LADDER, and one that fell through reports an
  // index into EXPENSE_LADDER. Both are diagnostic-only rather than something to branch on.
  const expenseRungIndex = outFan.rungIndex
  const expenseRungLadder = outFan.rungLadder

  if (surplus > 0) {
    nodes.push(syntheticNode('savings', 'Savings', CASH_FLOW_COLORS.netPositive, 'savings'))
    links.push({ source: 'hub', target: 'savings', value: surplus })
  }

  return { nodes, links, hubTotal: Math.max(incomeSum, expenseSum), expenseRungIndex, expenseRungLadder }
}
