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

  it('a direct bucket never claims to navigate — it has no expressible filter', () => {
    const data = buildSankeyData(income, mixedExpenses(), { kind: 'expanded' })
    for (const n of data.nodes.filter(x => x.nodeKind === 'direct' || x.nodeKind === 'other')) {
      expect(n.action).toEqual({ kind: 'none' })
    }
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

  it('budgets are allocated so the floor is provable, not asserted', () => {
    expect(INCOME_NODE_BUDGET + EXPENSE_NODE_BUDGET + 2).toBe(TOTAL_NODE_BUDGET)
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
