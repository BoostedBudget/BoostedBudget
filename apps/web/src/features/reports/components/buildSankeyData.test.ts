import { describe, it, expect } from 'bun:test'
import {
  buildSankeyData,
  EXPENSE_LADDER,
  FOCUS_LADDER,
  EXPENSE_NODE_BUDGET,
  INCOME_NODE_BUDGET,
  TOTAL_NODE_BUDGET,
} from './buildSankeyData'
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

  /**
   * Wide enough to force an expense OVERFLOW node: the parents-only floor keeps 30 roots, so 42
   * roots must collapse the remaining 12 into one `other`. Totals descend so which roots survive
   * is deterministic rather than a tie-break.
   */
  function overflowingExpenses(): CategoryHierarchyNode[] {
    return Array.from({ length: 42 }, (_, i) => node(`P${i}`, `Parent ${i}`, 1000 - i))
  }

  /** Wide enough to force an INCOME overflow node: 11 leaves are kept, so 15 leaves collapse 4. */
  function overflowingIncome(): CategoryHierarchyNode[] {
    return Array.from({ length: 15 }, (_, i) => node(`INCOME_${i}`, `Source ${i}`, 500 - i))
  }

  it('every synthetic node is inert — none of them has an expressible filter', () => {
    // Covers the hub, savings, deficit, direct AND overflow buckets: none of them names a set of
    // transactions any filter can select, so none may claim it will navigate somewhere matching
    // its own amount.
    //
    // ⚠ THE EXPECTED SET IS PINNED PER FIXTURE, AND THAT IS THE POINT (#1626 Phase 2, review
    // finding X4). This test used to iterate whatever synthetic nodes happened to exist and assert
    // only that `hub` was among them — and neither of its two fixtures could produce an `other`
    // node at all, so an overflow bucket was free to acquire a navigation action while this stayed
    // green. Measured, not assumed: giving both overflow emitters a `navigate` action left all 27
    // tests in this file passing. Requiring an exact SET means a kind that stops being emitted
    // fails just as loudly as one that starts.
    const cases: Array<{ name: string; data: ReturnType<typeof buildSankeyData>; expect: string[] }> = [
      { name: 'surplus', data: buildSankeyData(income, mixedExpenses(), { kind: 'expanded' }), expect: ['hub', 'savings', 'direct'] },
      {
        name: 'deficit',
        data: buildSankeyData([node('INCOME', 'Income', 10)], [node('RENT_AND_UTILITIES', 'Rent', 900)], { kind: 'expanded' }),
        expect: ['hub', 'deficit'],
      },
      { name: 'expense overflow', data: buildSankeyData(income, overflowingExpenses(), { kind: 'expanded' }), expect: ['hub', 'deficit', 'other'] },
      { name: 'income overflow', data: buildSankeyData(overflowingIncome(), mixedExpenses(), { kind: 'expanded' }), expect: ['hub', 'savings', 'other', 'direct'] },
    ]

    const synthetic = new Set(['hub', 'savings', 'deficit', 'other', 'direct'])
    for (const c of cases) {
      const seen = new Set<string>()
      for (const n of c.data.nodes.filter(x => synthetic.has(x.nodeKind))) {
        seen.add(n.nodeKind)
        expect(n.action, `${c.name}: ${n.nodeKind} node ${n.id} must be inert`).toEqual({ kind: 'none' })
      }
      expect([...seen].sort(), `${c.name}: synthetic kinds present`).toEqual([...c.expect].sort())
    }

    // And the two overflow fixtures must really have overflowed, so a budget change that stops
    // producing an `other` node is a failure here rather than a silent loss of coverage.
    expect(cases[2].data.nodes.filter(n => n.nodeKind === 'other').map(n => n.id)).toEqual(['out:other:l1'])
    expect(cases[3].data.nodes.filter(n => n.nodeKind === 'other').map(n => n.id)).toEqual(['in:other'])
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

  // The app cannot observe whether a shortfall was funded from savings, credit, or an overdraft,
  // so the node must not assert one — it displays as "Shortfall", not "Drawn from Savings /
  // Credit". The id stays 'deficit' (referenced by the test above and unchanged here); only the
  // display name changed.
  it('a shortfall displays as "Shortfall", not a claim about where the money came from', () => {
    const data = buildSankeyData([node('INCOME', 'Income', 100)], [node('RENT_AND_UTILITIES', 'Rent', 300)], { kind: 'parents' })
    const deficitNode = data.nodes.find(n => n.id === 'deficit')
    expect(deficitNode?.name).toBe('Shortfall')
  })

  // Regression pin, not the guarantee itself: buildSankeyData derives Saved/Shortfall from ONE
  // signed value (surplus = incomeSum - expenseSum) with mutually exclusive `< 0` / `> 0`
  // branches, so no input can ever emit both — that is structural to the code, not something this
  // test enforces. This pins the observable behaviour so a future refactor that broke the
  // single-signed-value structure would be caught here.
  it('a positive net emits Savings only, a negative net emits a deficit only, and zero emits neither', () => {
    const positive = buildSankeyData([node('INCOME', 'Income', 500)], [node('RENT_AND_UTILITIES', 'Rent', 300)], { kind: 'parents' })
    expect(positive.nodes.some(n => n.id === 'savings')).toBe(true)
    expect(positive.nodes.some(n => n.id === 'deficit')).toBe(false)

    const negative = buildSankeyData([node('INCOME', 'Income', 100)], [node('RENT_AND_UTILITIES', 'Rent', 300)], { kind: 'parents' })
    expect(negative.nodes.some(n => n.id === 'deficit')).toBe(true)
    expect(negative.nodes.some(n => n.id === 'savings')).toBe(false)

    const exact = buildSankeyData([node('INCOME', 'Income', 300)], [node('RENT_AND_UTILITIES', 'Rent', 300)], { kind: 'parents' })
    expect(exact.nodes.some(n => n.id === 'savings')).toBe(false)
    expect(exact.nodes.some(n => n.id === 'deficit')).toBe(false)
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

  // #1626 Phase 2, a parity defect of my own. incomeLeaves emits a parent's DIRECT receipts as a
  // leaf in their own right — correct, it is money that must be counted exactly once — but the
  // only filter available for that leaf is the parent's key, which matches every descendant too.
  // So the node showed 500 and the list it navigated to would have shown 5000. There is no
  // "direct-only" filter to express it, so the node is inert until there is: showing the wrong
  // list is worse than showing no list, because the user cannot tell it is the wrong one.
  it("a parent's own direct income is inert, because its filter would include its children", () => {
    const withDirect: CategoryHierarchyNode[] = [
      node('INCOME', 'Income', 5000, 500, [node('INCOME_WAGES', 'Wages', 4500)]),
    ]
    const data = buildSankeyData(withDirect, [node('FOOD_AND_DRINK', 'Food And Drink', 5000)], { kind: 'parents' })

    const directLeaf = data.nodes.find(n => n.name === 'Income')
    expect(directLeaf).toBeDefined()
    expect(directLeaf?.action).toEqual({ kind: 'none' })

    // Its child is a true leaf and stays navigable — the fix must be narrow.
    expect(data.nodes.find(n => n.name === 'Wages')?.action)
      .toEqual({ kind: 'navigate', pfcDetaileds: ['INCOME_WAGES'] })
  })

  // The analytics event reports a node's depth, and every income node's id is `in:l1:*` regardless
  // of where it actually sits — the fan is flattened. Deriving depth from the id would report all
  // income navigations as level 1: a plausible number that is simply wrong, which is worse than no
  // number. So real depth is carried on the node.
  it('carries each income leaf its REAL depth, not the depth its id implies', () => {
    const deep: CategoryHierarchyNode[] = [
      node('INCOME', 'Income', 1000, 0, [
        node('INCOME_WAGES', 'Wages', 1000, 0, [node('custom:bonus', 'Bonus', 1000)]),
      ]),
    ]
    const data = buildSankeyData(deep, [node('FOOD_AND_DRINK', 'Food And Drink', 1000)], { kind: 'parents' })

    const bonus = data.nodes.find(n => n.name === 'Bonus')
    expect(bonus?.id.startsWith('in:l1:')).toBe(true)
    expect(bonus?.categoryDepth).toBe(3)
  })

  // #1630: ECharts' sankey series reads a node-data field literally named `depth` as its LAYOUT
  // LAYER, not as arbitrary payload — that collision is what put income on the wrong side of the
  // chart. This field must therefore never be reintroduced under that name.
  it('never emits a node-data field named `depth` — ECharts reads that name as its layout layer', () => {
    const data = buildSankeyData(incomeTree, [node('FOOD_AND_DRINK', 'Food And Drink', 5500)], { kind: 'parents' })
    for (const n of data.nodes) {
      expect(Object.prototype.hasOwnProperty.call(n, 'depth')).toBe(false)
    }
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

  // #1626 Phase 2. This used to assert { kind: 'none' } and the reasoning was correct at the time:
  // 'UNCATEGORIZED' is a sentinel this analytics layer synthesises, not a value any column holds,
  // so `pfc_primaries=UNCATEGORIZED` matched ZERO rows and the list would have contradicted the
  // node the user just clicked. Phase 2 added the `uncategorized` filter — which is what a
  // dedicated flag rather than a magic category value buys — so the node navigates now.
  it('uncategorized navigates by its own flag, never by a category value', () => {
    const data = buildSankeyData(income, withUncategorized, { kind: 'parents' })
    const uncat = data.nodes.find(n => n.id === 'out:l1:UNCATEGORIZED')

    expect(uncat?.action).toEqual({ kind: 'navigate', uncategorized: true })
    // ⚠ Specifically NOT pfc_primaries: sending the sentinel as a category value is the bug this
    // asserts against, and it would look like a working filter while returning nothing.
    expect((uncat?.action as { pfcPrimaries?: string[] }).pfcPrimaries).toBeUndefined()
  })

  it('uncategorized still counts toward the totals', () => {
    const data = buildSankeyData(income, withUncategorized, { kind: 'parents' })
    const hubOut = data.links.filter(l => l.source === 'hub').reduce((s, l) => s + l.value, 0)
    const hubIn = data.links.filter(l => l.target === 'hub').reduce((s, l) => s + l.value, 0)
    expect(hubOut).toBeCloseTo(hubIn, 6)
  })
})

describe('buildSankeyData — degradation ladder conservation (#1626 Phase 2, item 3)', () => {
  /**
   * Generates N roots, each with M children, each with K grandchildren — scaled at the CALL SITE
   * from the real budget constants rather than hand-picked numbers, so the matrix below tracks the
   * ladder if either budget or the rung table changes instead of silently drifting onto a different
   * rung while still reporting green (the exact failure mode this test exists to rule out).
   */
  // Built bottom-up so total == direct_total + sum(children.total) at every level, matching the
  // real API's own guarantee (see the "buildSankeyData — focus edge cases" fixture comment) — a
  // fixture that violates it would make the conservation assertions below measure the fixture's
  // own arithmetic instead of the builder's.
  function genTree(roots: number, children: number, grandchildren: number): CategoryHierarchyNode[] {
    const grandTotal = 1
    const childDirect = 1
    const childTotal = childDirect + grandchildren * grandTotal
    const rootDirect = 1
    const rootTotal = rootDirect + children * childTotal
    return Array.from({ length: roots }, (_, i) =>
      node(`P${i}`, `Parent ${i}`, rootTotal, rootDirect, Array.from({ length: children }, (_, j) =>
        node(`C${i}_${j}`, `Child ${i}.${j}`, childTotal, childDirect, Array.from({ length: grandchildren }, (_, k) =>
          node(`G${i}_${j}_${k}`, `Grand ${i}.${j}.${k}`, grandTotal))))))
  }

  // Swept across a spread of scales derived from EXPENSE_NODE_BUDGET rather than any single
  // hand-tuned shape: no entry here is chosen to land on a PARTICULAR rung (that is the FORBIDDEN
  // approach — see the brief), it is the coverage assertion below, over the whole matrix, that
  // proves every reachable rung ran — and would fail loudly, not silently, if the sweep stopped
  // being broad enough.
  const rootCounts = [
    1, 2, 4,
    Math.round(EXPENSE_NODE_BUDGET / 4), Math.round(EXPENSE_NODE_BUDGET / 3), Math.round(EXPENSE_NODE_BUDGET / 2),
    EXPENSE_NODE_BUDGET, EXPENSE_NODE_BUDGET * 2,
  ]
  const childCounts = [0, 1, 2, 6, Math.round(EXPENSE_NODE_BUDGET / 2), EXPENSE_NODE_BUDGET]
  const grandchildCounts = [0, 1, 3]

  const shapes = rootCounts.flatMap(roots =>
    childCounts.flatMap(children =>
      grandchildCounts.map(grandchildren => ({ roots, children, grandchildren }))))

  it('conserves flow and respects both budgets at every generated shape, and exercises every rung', () => {
    const observedRungs = new Set<number>()

    for (const { roots, children, grandchildren } of shapes) {
      const expenses = genTree(roots, children, grandchildren)
      const data = buildSankeyData(income, expenses, { kind: 'expanded' })
      const shapeLabel = `roots=${roots} children=${children} grandchildren=${grandchildren}`

      const hubOut = data.links.filter(l => l.source === 'hub').reduce((s, l) => s + l.value, 0)
      const hubIn = data.links.filter(l => l.target === 'hub').reduce((s, l) => s + l.value, 0)
      expect(hubOut, shapeLabel).toBeCloseTo(hubIn, 6)
      expectConservation(data)

      const incomeNodes = data.nodes.filter(n => n.nodeKind === 'income' || n.id === 'in:other')
      const expenseNodes = data.nodes.filter(n => n.id.startsWith('out:') || n.nodeKind === 'direct')
      expect(incomeNodes.length, shapeLabel).toBeLessThanOrEqual(INCOME_NODE_BUDGET)
      expect(expenseNodes.length, shapeLabel).toBeLessThanOrEqual(EXPENSE_NODE_BUDGET)
      expect(data.nodes.length, shapeLabel).toBeLessThanOrEqual(TOTAL_NODE_BUDGET)

      expect(data.expenseRungIndex, shapeLabel).toBeDefined()
      observedRungs.add(data.expenseRungIndex as number)
    }

    // EVERY rung must be reachable — no `.slice`, no exclusions. The ladder previously carried a
    // fifth rung, [0, 0, 10], that could never run: the rung before it, [0, 0, 30],
    // depth-0-collapses every kept root to exactly one node and adds at most one overflow node, so
    // it renders at most maxRoots + 1 nodes, and 30 + 1 === EXPENSE_NODE_BUDGET means it always
    // fits and never falls through. It was reported by an earlier revision of this test and then
    // DELETED, because a documented degradation step that cannot run is a lie about the ladder and
    // it forced this assertion to carry a permanent exclusion. The floor is now [0, 0, 30], which
    // always fits — which is exactly what a floor has to do.
    //
    // The assertion below is what makes the whole matrix trustworthy: without it, a matrix that
    // only ever hit rung 0 would look like full coverage of the ladder.
    const lastRungMaxRoots = EXPENSE_LADDER[EXPENSE_LADDER.length - 1]?.[2]
    expect(lastRungMaxRoots).toBe(EXPENSE_NODE_BUDGET - 1)

    const reachableRungIndices = new Set(EXPENSE_LADDER.map((_, i) => i))
    expect(observedRungs).toEqual(reachableRungIndices)
  })
})

describe('buildSankeyData — terminal collapse vs recursive rendering (#1626 Phase 2, item 4)', () => {
  // Unlike the existing focus test, the collapsed sibling here (TRANSPORTATION) has its OWN
  // children. A leaf sibling cannot distinguish "collapse renders one replacement node" from
  // "the whole subtree still gets rendered", because a leaf has no subtree to wrongly render — both
  // behaviours produce identical output. This fixture is the minimal one that can tell them apart.
  const treeWithNestedSibling = [
    node('TRANSPORTATION', 'Transportation', 500, 100, [
      node('TRANSPORTATION_GAS', 'Gas', 300),
      node('TRANSPORTATION_PARKING', 'Parking', 100),
    ]),
    node('FOOD_AND_DRINK', 'Food And Drink', 165, 40, [
      node('custom:eo', 'Eating Out', 125, 100, [node('custom:cf', 'Coffee Runs', 25)]),
    ]),
  ]

  it('collapses a sibling subtree into exactly ONE node, never one per descendant', () => {
    const data = buildSankeyData(income, treeWithNestedSibling, { kind: 'focus', key: 'custom:eo' })

    // Terminal collapse: one "Other" node stands in for the whole TRANSPORTATION subtree.
    const others = data.nodes.filter(n => n.nodeKind === 'other')
    expect(others).toHaveLength(1)
    const collapsedLink = data.links.find(l => l.target === others[0]?.id)
    expect(collapsedLink?.value).toBeCloseTo(500, 6)

    // Recursive rendering would leak the subtree's own nodes; terminal collapse must not.
    expect(data.nodes.some(n => n.id.includes('TRANSPORTATION'))).toBe(false)

    // The focused path itself still renders normally.
    const ids = data.nodes.map(n => n.id)
    expect(ids).toContain('out:l1:FOOD_AND_DRINK')
    expect(ids).toContain('out:l2:FOOD_AND_DRINK|custom:eo')

    // Conservation once more: the collapsed subtree's full total must still reach the hub, so a
    // "collapse" that actually DROPPED the subtree instead of folding it in would fail here too.
    const hubOut = data.links.filter(l => l.source === 'hub').reduce((s, l) => s + l.value, 0)
    const hubIn = data.links.filter(l => l.target === 'hub').reduce((s, l) => s + l.value, 0)
    expect(hubOut).toBeCloseTo(hubIn, 6)
    expectConservation(data)
  })
})

describe('buildSankeyData — focus ladder coverage (#1626 gap G10)', () => {
  /**
   * The FOCUS mirror of the root-ladder sweep above. The root ladder is exported specifically so a
   * test cannot hand-type a rung index, and it has a sweep asserting every rung was exercised;
   * `FOCUS_LADDER` had neither until this suite — it was a function-local const, so the only way to
   * name a focus rung was to type the integer, and nothing said whether the matrix had reached it.
   *
   * ⚠ `expenseRungLadder === 'focus'` IS THE LOAD-BEARING PART. A focus view can also FALL THROUGH
   * to the root ladder (when the focused key is absent, or when no focus rung fits), and that
   * reports a small integer here too. Collecting `expenseRungIndex` alone would credit the focus
   * ladder for a root-ladder render and claim coverage the sweep does not have.
   */
  function focusTree(roots: number, children: number, grandchildren: number): CategoryHierarchyNode[] {
    const grandTotal = 1
    const childDirect = 1
    const childTotal = childDirect + grandchildren * grandTotal
    const rootDirect = 1
    const rootTotal = rootDirect + children * childTotal
    return Array.from({ length: roots }, (_, i) =>
      node(`P${i}`, `Parent ${i}`, rootTotal, rootDirect, Array.from({ length: children }, (_, j) =>
        node(`C${i}_${j}`, `Child ${i}.${j}`, childTotal, childDirect, Array.from({ length: grandchildren }, (_, k) =>
          node(`G${i}_${j}_${k}`, `Grand ${i}.${j}.${k}`, grandTotal))))))
  }

  // Scaled from EXPENSE_NODE_BUDGET rather than hand-picked, exactly like the root sweep: no entry
  // is chosen to land on a particular rung. Both focus depths are swept — an L1 root and an L2
  // child — because pruneForFocus renders the ancestor PATH, so the level the focused key sits at
  // changes the node count independently of its subtree.
  const rootCounts = [1, 2, Math.round(EXPENSE_NODE_BUDGET / 3), EXPENSE_NODE_BUDGET]
  const childCounts = [0, 1, 2, 6, Math.round(EXPENSE_NODE_BUDGET / 2), EXPENSE_NODE_BUDGET]
  const grandchildCounts = [0, 1, 3, 6, Math.round(EXPENSE_NODE_BUDGET / 2), EXPENSE_NODE_BUDGET]

  const shapes = rootCounts.flatMap(roots =>
    childCounts.flatMap(children =>
      grandchildCounts.flatMap(grandchildren =>
        // Focusing C0_0 is only meaningful when it exists.
        (children > 0 ? ['P0', 'C0_0'] : ['P0']).map(focusKey =>
          ({ roots, children, grandchildren, focusKey })))))

  it('conserves flow and respects both budgets at every focused shape, and exercises every focus rung', () => {
    const observedRungs = new Set<number>()

    for (const { roots, children, grandchildren, focusKey } of shapes) {
      const expenses = focusTree(roots, children, grandchildren)
      const data = buildSankeyData(income, expenses, { kind: 'focus', key: focusKey })
      const shapeLabel = `roots=${roots} children=${children} grandchildren=${grandchildren} focus=${focusKey}`

      const hubOut = data.links.filter(l => l.source === 'hub').reduce((s, l) => s + l.value, 0)
      const hubIn = data.links.filter(l => l.target === 'hub').reduce((s, l) => s + l.value, 0)
      expect(hubOut, shapeLabel).toBeCloseTo(hubIn, 6)
      expectConservation(data)

      const expenseNodes = data.nodes.filter(n => n.id.startsWith('out:') || n.nodeKind === 'direct')
      expect(expenseNodes.length, shapeLabel).toBeLessThanOrEqual(EXPENSE_NODE_BUDGET)
      expect(data.nodes.length, shapeLabel).toBeLessThanOrEqual(TOTAL_NODE_BUDGET)

      expect(data.expenseRungIndex, shapeLabel).toBeDefined()
      // The FLOOR property, asserted per shape: FOCUS_LADDER's last rung always fits for a
      // production-depth tree, so a focus view whose key exists must never fall through to the root
      // ladder. This is the assertion that would fail first if a fourth tree level appeared or
      // EXPENSE_NODE_BUDGET shrank — the two things FOCUS_LADDER's comment names as invalidating
      // its floor.
      expect(data.expenseRungLadder, shapeLabel).toBe('focus')
      observedRungs.add(data.expenseRungIndex as number)
    }

    // EVERY rung of FOCUS_LADDER, no exclusions — the same standard the root ladder is held to,
    // where an unreachable fifth rung was DELETED rather than excluded. This sweep found the same
    // thing on this ladder: rungs [1, 3] and [0, 0] were unreachable for every shape here and were
    // deleted, which is why the set below is exactly {0, 1}. Without this assertion, a matrix that
    // only ever hit rung 0 would look like full coverage.
    const reachableRungIndices = new Set(FOCUS_LADDER.map((_, i) => i))
    expect(observedRungs).toEqual(reachableRungIndices)
    expect(reachableRungIndices).toEqual(new Set([0, 1]))
  })
})
