import { describe, it, expect } from 'bun:test'
import { buildSankeyData, EXPENSE_NODE_BUDGET, INCOME_NODE_BUDGET, TOTAL_NODE_BUDGET } from './buildSankeyData'
import type { CategoryHierarchyNode } from '@boostedbudget/api-client/models'

function node(
  category: string,
  display: string,
  total: number,
  directTotal = total,
  children: CategoryHierarchyNode[] = [],
): CategoryHierarchyNode {
  return { category, display, total, direct_total: directTotal, percentage: 0, transaction_count: 1, children }
}

/** A tree with every shape that matters: direct spend on a parent, a PFC child, a custom child, a grandchild. */
function mixedExpenses(): CategoryHierarchyNode[] {
  return [
    node('TRANSPORTATION', 'Transportation', 500),
    node('FOOD_AND_DRINK', 'Food And Drink', 225, 40, [
      node('custom:eo', 'Eating Out', 125, 100, [node('custom:cf', 'Coffee Runs', 25)]),
      node('FOOD_AND_DRINK_FAST_FOOD', 'Fast Food', 60),
    ]),
  ]
}

const income = [node('INCOME_WAGES', 'Wages', 1000)]

/**
 * Every node with RENDERED children must satisfy inflow == sum of outflows.
 *
 * Defined over RENDERED children, not underlying ones: a node whose children a degradation rung
 * hid is a rendered LEAF — its inbound link is its whole total and it has no outflows, which is
 * correct. Conflating the two makes the invariant unsatisfiable in the consolidated view.
 */
function expectConservation(data: ReturnType<typeof buildSankeyData>) {
  const outflow = new Map<string, number>()
  const inflow = new Map<string, number>()
  for (const l of data.links) {
    outflow.set(l.source, (outflow.get(l.source) ?? 0) + l.value)
    inflow.set(l.target, (inflow.get(l.target) ?? 0) + l.value)
  }
  for (const n of data.nodes) {
    const out = outflow.get(n.id)
    if (out === undefined) continue // rendered leaf
    const inn = inflow.get(n.id)
    if (inn === undefined) continue // a source node (income root / deficit)
    expect(out).toBeCloseTo(inn, 6)
  }
}

describe('buildSankeyData — hierarchy', () => {
  it('consolidated view renders parents only, and each parent keeps its full roll-up', () => {
    const data = buildSankeyData(income, mixedExpenses(), { kind: 'parents' })

    const expenseNodes = data.nodes.filter(n => n.nodeKind === 'expense')
    expect(expenseNodes.map(n => n.name).sort()).toEqual(['Food And Drink', 'Transportation'])

    const fd = data.links.find(l => l.target === 'out:l1:FOOD_AND_DRINK')
    expect(fd?.value).toBeCloseTo(225, 6)
    expectConservation(data)
  })

  it('a parent whose children are hidden is a rendered leaf but still FOCUSES, not navigates', () => {
    const data = buildSankeyData(income, mixedExpenses(), { kind: 'parents' })
    const fd = data.nodes.find(n => n.id === 'out:l1:FOOD_AND_DRINK')
    expect(fd?.action).toEqual({ kind: 'focus', key: 'FOOD_AND_DRINK' })

    const transport = data.nodes.find(n => n.id === 'out:l1:TRANSPORTATION')
    expect(transport?.action.kind).toBe('navigate')
  })

  it('expanded view renders all three levels and a direct bucket for a parent with own spend', () => {
    const data = buildSankeyData(income, mixedExpenses(), { kind: 'expanded' })
    const ids = data.nodes.map(n => n.id)

    expect(ids).toContain('out:l2:FOOD_AND_DRINK|custom:eo')
    expect(ids).toContain('out:l3:FOOD_AND_DRINK|custom:eo|custom:cf')
    expect(ids).toContain('out:direct:FOOD_AND_DRINK')

    const direct = data.links.find(l => l.target === 'out:direct:FOOD_AND_DRINK')
    expect(direct?.value).toBeCloseTo(40, 6)
    expectConservation(data)
  })

  it('every synthetic node is inert — none of them has an expressible filter', () => {
    // Covers the hub, savings and deficit as well as direct/overflow buckets: none of them names a
    // set of transactions any filter can select, so none may claim it will navigate somewhere
    // matching its own amount.
    const surplus = buildSankeyData(income, mixedExpenses(), { kind: 'expanded' })
    const deficit = buildSankeyData(
      [node('INCOME', 'Income', 10)],
      [node('RENT_AND_UTILITIES', 'Rent', 900)],
      { kind: 'expanded' },
    )
    const synthetic = new Set(['hub', 'savings', 'deficit', 'other', 'direct'])
    for (const data of [surplus, deficit]) {
      const seen = new Set<string>()
      for (const n of data.nodes.filter(x => synthetic.has(x.nodeKind))) {
        seen.add(n.nodeKind)
        expect(n.action).toEqual({ kind: 'none' })
      }
      expect(seen.has('hub')).toBe(true)
    }
    expect(surplus.nodes.some(n => n.nodeKind === 'savings')).toBe(true)
    expect(deficit.nodes.some(n => n.nodeKind === 'deficit')).toBe(true)
  })

  it('a PFC detailed leaf navigates by pfc_detaileds only, never widened to its primary', () => {
    const data = buildSankeyData(income, mixedExpenses(), { kind: 'expanded' })
    const ff = data.nodes.find(n => n.id === 'out:l2:FOOD_AND_DRINK|FOOD_AND_DRINK_FAST_FOOD')
    expect(ff?.action).toEqual({ kind: 'navigate', pfcDetaileds: ['FOOD_AND_DRINK_FAST_FOOD'] })
  })

  it('a custom leaf navigates by custom_category_ids with the token stripped', () => {
    const data = buildSankeyData(income, mixedExpenses(), { kind: 'expanded' })
    const coffee = data.nodes.find(n => n.id === 'out:l3:FOOD_AND_DRINK|custom:eo|custom:cf')
    expect(coffee?.action).toEqual({ kind: 'navigate', customCategoryIds: ['cf'] })
  })

  it('focus view keeps the ancestor path and collapses siblings exactly once', () => {
    const data = buildSankeyData(income, mixedExpenses(), { kind: 'focus', key: 'custom:eo' })
    const ids = data.nodes.map(n => n.id)

    expect(ids).toContain('out:l1:FOOD_AND_DRINK')
    expect(ids).toContain('out:l2:FOOD_AND_DRINK|custom:eo')
    expect(ids).toContain('out:l3:FOOD_AND_DRINK|custom:eo|custom:cf')

    // The focused node's sibling collapses into ONE replacement, and traversal stops there —
    // a collapsed subtree must not also contribute a deeper "other".
    const others = data.nodes.filter(n => n.nodeKind === 'other')
    expect(others.length).toBeLessThanOrEqual(2)
    expect(new Set(others.map(o => o.id)).size).toBe(others.length)
    expectConservation(data)
  })

  it('a parentless custom category does not collide with its own direct bucket', () => {
    // parent === child is reachable: a parentless custom category is its own L1.
    const tree = [node('custom:solo', 'Solo', 90, 30, [node('custom:kid', 'Kid', 60)])]
    const data = buildSankeyData(income, tree, { kind: 'expanded' })
    expect(new Set(data.nodes.map(n => n.id)).size).toBe(data.nodes.length)
    expectConservation(data)
  })
})

describe('buildSankeyData — node budget', () => {
  /** A deliberately hostile tree: many parents, many children, many grandchildren. */
  function hugeTree(): CategoryHierarchyNode[] {
    return Array.from({ length: 30 }, (_, i) =>
      node(`P${i}`, `Parent ${i}`, 1000 - i, 1, Array.from({ length: 10 }, (_, j) =>
        node(`custom:c${i}_${j}`, `Child ${i}.${j}`, 10, 1, Array.from({ length: 5 }, (_, k) =>
          node(`custom:g${i}_${j}_${k}`, `Grand ${i}.${j}.${k}`, 1))))))
  }

  it('never exceeds the global budget, counting EVERY synthetic node', () => {
    for (const mode of [{ kind: 'parents' as const }, { kind: 'expanded' as const }]) {
      const data = buildSankeyData(income, hugeTree(), mode)
      expect(data.nodes.length).toBeLessThanOrEqual(TOTAL_NODE_BUDGET)
    }
  })

  it('consolidates the DEEPEST level first — depth shrinks before breadth', () => {
    const data = buildSankeyData(income, hugeTree(), { kind: 'expanded' })
    // With 30 parents x 10 children x 5 grandchildren there is no room for depth at all, so the
    // ladder must have fallen all the way to the parents-only floor rather than truncating
    // breadth while keeping a deep, mostly-hidden tree.
    expect(data.nodes.some(n => n.id.startsWith('out:l3:'))).toBe(false)
    expectConservation(data)
  })

  it('the income fan is capped in its own right, with its own overflow node', () => {
    const manyIncome = Array.from({ length: 40 }, (_, i) => node(`INCOME_${i}`, `Source ${i}`, 100 - i))
    const data = buildSankeyData(manyIncome, mixedExpenses(), { kind: 'parents' })

    const incomeNodes = data.nodes.filter(n => n.nodeKind === 'income' || n.id === 'in:other')
    expect(incomeNodes.length).toBeLessThanOrEqual(INCOME_NODE_BUDGET)
    expect(data.nodes.some(n => n.id === 'in:other')).toBe(true)

    // Truncation must COLLAPSE, never DROP: the hub's inflow still equals the full income total.
    const hubIn = data.links.filter(l => l.target === 'hub').reduce((s, l) => s + l.value, 0)
    expect(hubIn).toBeCloseTo(manyIncome.reduce((s, n) => s + n.total, 0), 6)
  })

  it('the expense fan alone respects its own share of the budget', () => {
    // The previous version of this test compared the exported constants to each other, which is
    // tautological — it would pass even if buildSankeyData ignored every budget. This asserts the
    // allocation is actually ENFORCED: the expense fan must fit its 31, not merely the global 45,
    // or a wide expense tree would eat the income fan's share.
    const data = buildSankeyData(income, hugeTree(), { kind: 'expanded' })
    const expenseNodes = data.nodes.filter(
      n => n.id.startsWith('out:') || n.nodeKind === 'direct',
    )
    expect(expenseNodes.length).toBeLessThanOrEqual(EXPENSE_NODE_BUDGET)

    const incomeNodes = data.nodes.filter(n => n.nodeKind === 'income' || n.id === 'in:other')
    expect(incomeNodes.length).toBeLessThanOrEqual(INCOME_NODE_BUDGET)
  })
})

describe('buildSankeyData — savings and deficit', () => {
  it('savings is derived from the FULL row set, not the rendered subset', () => {
    const manyIncome = Array.from({ length: 40 }, (_, i) => node(`INCOME_${i}`, `Source ${i}`, 100))
    const data = buildSankeyData(manyIncome, [node('FOOD_AND_DRINK', 'Food And Drink', 1000)], { kind: 'parents' })

    const savings = data.links.find(l => l.target === 'savings')
    expect(savings?.value).toBeCloseTo(4000 - 1000, 6)
    expectConservation(data)
  })

  it('deficit appears as a hub INFLOW when expenses exceed income', () => {
    const data = buildSankeyData([node('INCOME', 'Income', 100)], [node('RENT_AND_UTILITIES', 'Rent', 300)], { kind: 'parents' })
    const deficit = data.links.find(l => l.source === 'deficit')
    expect(deficit?.value).toBeCloseTo(200, 6)
    expectConservation(data)
  })

  it('empty input yields an empty chart rather than a phantom hub', () => {
    const data = buildSankeyData([], [], { kind: 'parents' })
    expect(data.nodes).toHaveLength(0)
    expect(data.links).toHaveLength(0)
  })
})

describe('buildSankeyData — focus edge cases', () => {
  // The server guarantees total == direct_total + sum(children.total) at every node — it is
  // asserted there by walking the whole tree — so fixtures must honour it too. A fixture that does
  // not is testing a shape the API cannot produce, and the conservation assertions below would be
  // measuring the fixture's arithmetic rather than the builder's.
  const tree = [
    node('TRANSPORTATION', 'Transportation', 500),
    node('FOOD_AND_DRINK', 'Food And Drink', 165, 40, [
      node('custom:eo', 'Eating Out', 125, 100, [node('custom:cf', 'Coffee Runs', 25)]),
    ]),
  ]

  it('a stale focus key degrades to a full view rather than an empty chart', () => {
    // Reachable: the user focuses a category, then changes the date range to a period in which
    // that category has no spend. Rendering nothing would read as "you spent nothing", which is a
    // claim about the user's money that a missing key does not support.
    const data = buildSankeyData(income, tree, { kind: 'focus', key: 'custom:deleted' })

    expect(data.nodes.some(n => n.nodeKind === 'expense')).toBe(true)
    const hubOut = data.links.filter(l => l.source === 'hub').reduce((s, l) => s + l.value, 0)
    const hubIn = data.links.filter(l => l.target === 'hub').reduce((s, l) => s + l.value, 0)
    expect(hubOut).toBeCloseTo(hubIn, 6)
  })

  it('focusing the DEEPEST node still conserves flow along its whole ancestor path', () => {
    const data = buildSankeyData(income, tree, { kind: 'focus', key: 'custom:cf' })

    const ids = data.nodes.map(n => n.id)
    expect(ids).toContain('out:l1:FOOD_AND_DRINK')
    expect(ids).toContain('out:l2:FOOD_AND_DRINK|custom:eo')

    const outflow = new Map<string, number>()
    const inflow = new Map<string, number>()
    for (const l of data.links) {
      outflow.set(l.source, (outflow.get(l.source) ?? 0) + l.value)
      inflow.set(l.target, (inflow.get(l.target) ?? 0) + l.value)
    }
    for (const n of data.nodes) {
      const out = outflow.get(n.id)
      const inn = inflow.get(n.id)
      if (out === undefined || inn === undefined) continue
      expect(out).toBeCloseTo(inn, 6)
    }
  })
})

describe('buildSankeyData — the income fan is hierarchical too', () => {
  // Production income is shaped INCOME -> INCOME_WAGES etc: the endpoint returns a TREE for both
  // directions. Rendering only the roots collapses every source into one "Income" node, which is
  // the opposite of the asymmetric default the endpoint documents — income's useful distinction
  // lives one level down.
  const incomeTree: CategoryHierarchyNode[] = [
    node('INCOME', 'Income', 5000, 0, [
      node('INCOME_WAGES', 'Wages', 4000),
      node('INCOME_DIVIDENDS', 'Dividends', 1000),
    ]),
    node('custom:side', 'Side hustle', 500),
  ]

  it('renders income LEAVES, not just roots', () => {
    const data = buildSankeyData(incomeTree, [node('FOOD_AND_DRINK', 'Food And Drink', 5500)], { kind: 'parents' })
    const names = data.nodes.filter(n => n.nodeKind === 'income').map(n => n.name).sort()

    expect(names).toEqual(['Dividends', 'Side hustle', 'Wages'])
    expect(names).not.toContain('Income')
  })

  it('a parent with its own direct income keeps that money as its own leaf', () => {
    const withDirect: CategoryHierarchyNode[] = [
      node('INCOME', 'Income', 5000, 500, [node('INCOME_WAGES', 'Wages', 4500)]),
    ]
    const data = buildSankeyData(withDirect, [node('FOOD_AND_DRINK', 'Food And Drink', 5000)], { kind: 'parents' })

    const hubIn = data.links.filter(l => l.target === 'hub').reduce((s, l) => s + l.value, 0)
    expect(hubIn).toBeCloseTo(5000, 6)
  })

  it('a PFC detailed income leaf navigates by pfc_detaileds, not its primary', () => {
    const data = buildSankeyData(incomeTree, [node('FOOD_AND_DRINK', 'Food And Drink', 5500)], { kind: 'parents' })
    const wages = data.nodes.find(n => n.name === 'Wages')
    expect(wages?.action).toEqual({ kind: 'navigate', pfcDetaileds: ['INCOME_WAGES'] })

    const side = data.nodes.find(n => n.name === 'Side hustle')
    expect(side?.action).toEqual({ kind: 'navigate', customCategoryIds: ['side'] })
  })
})

describe('buildSankeyData — uncategorized spend', () => {
  // The SQL synthesises 'UNCATEGORIZED' as the L1 key whenever a transaction has no primary at
  // all, so uncategorized money is NOT dropped from the payload — it arrives as an ordinary
  // category and must render as an ordinary node. Hiding it would understate the expense fan and
  // make the hub stop balancing.
  const withUncategorized = [
    node('FOOD_AND_DRINK', 'Food And Drink', 300),
    node('UNCATEGORIZED', 'Uncategorized', 120),
  ]

  it('renders uncategorized spend as a visible node', () => {
    const data = buildSankeyData(income, withUncategorized, { kind: 'parents' })
    const names = data.nodes.filter(n => n.nodeKind === 'expense').map(n => n.name)
    expect(names).toContain('Uncategorized')

    const link = data.links.find(l => l.target === 'out:l1:UNCATEGORIZED')
    expect(link?.value).toBeCloseTo(120, 6)
  })

  it('uncategorized is NOT navigable, because no transactions filter can express it', () => {
    const data = buildSankeyData(income, withUncategorized, { kind: 'parents' })
    const uncat = data.nodes.find(n => n.id === 'out:l1:UNCATEGORIZED')

    // 'UNCATEGORIZED' is a sentinel this analytics layer synthesises; it is not a value any column
    // holds. effective_pfc_primary is NULL for those rows, so `pfc_primaries=UNCATEGORIZED` would
    // match ZERO rows — the list would contradict the node the user just clicked. Same rule as the
    // direct and overflow buckets: no expressible filter means no navigation.
    expect(uncat?.action).toEqual({ kind: 'none' })
  })

  it('uncategorized still counts toward the totals', () => {
    const data = buildSankeyData(income, withUncategorized, { kind: 'parents' })
    const hubOut = data.links.filter(l => l.source === 'hub').reduce((s, l) => s + l.value, 0)
    const hubIn = data.links.filter(l => l.target === 'hub').reduce((s, l) => s + l.value, 0)
    expect(hubOut).toBeCloseTo(hubIn, 6)
  })
})
