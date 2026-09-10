/**
 * computeBudgetMetrics top-level-only totals (Issue #1203).
 * formatBudgetCategoryLabel — savings kind (#1300).
 */
import { describe, it, expect } from 'bun:test'
import {
  computeBudgetMetrics,
  computeParentAllocationRemainder,
  computeParentDirectSpend,
  filterTopLevel,
  formatBudgetCategoryLabel,
  isSubAllocationOfParent,
  type Budget,
  type BudgetCategory,
} from './types'

const baseCat = {
  budget_id: 'b1',
  tags: [] as string[],
  match_logic: 'category_only' as const,
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
}

function budget(categories: BudgetCategory[]): Budget {
  return {
    id: 'b1',
    user_id: 'u1',
    name: 'Test',
    period: 'monthly',
    start_date: '2024-01-01',
    amount: 1000,
    currency_code: 'USD',
    is_active: true,
    categories,
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    income_amount: 1000,
    income_sources: [],
    expected_income: 1000,
  } as Budget
}

describe('formatBudgetCategoryLabel — savings kind (#1300)', () => {
  it('returns savings_goal_name when present', () => {
    const cat = {
      category_kind: 'savings',
      savings_goal_name: 'House Down Payment',
    } as Partial<BudgetCategory> as BudgetCategory
    expect(formatBudgetCategoryLabel(cat)).toBe('House Down Payment')
  })

  it('returns "Savings Goal" fallback when savings_goal_name absent', () => {
    const cat = {
      category_kind: 'savings',
    } as Partial<BudgetCategory> as BudgetCategory
    expect(formatBudgetCategoryLabel(cat)).toBe('Savings Goal')
  })
})

describe('computeBudgetMetrics — top-level-only totals', () => {
  it('excludes a PFC detailed sub-allocation when its primary is also budgeted', () => {
    const m = computeBudgetMetrics(
      budget([
        { ...baseCat, id: 'p', category_kind: 'pfc', pfc_primary: 'FOOD_AND_DRINK', allocated_amount: 300, spent_amount: 150 },
        { ...baseCat, id: 'c', category_kind: 'pfc', pfc_primary: 'FOOD_AND_DRINK', pfc_detailed: 'FOOD_AND_DRINK_FAST_FOOD', allocated_amount: 50, spent_amount: 20 },
      ])
    )
    expect(m.total_allocated).toBe(300)
    expect(m.total_spent).toBe(150)
  })

  it('excludes a custom child when its custom parent is also budgeted', () => {
    const m = computeBudgetMetrics(
      budget([
        { ...baseCat, id: 'p', category_kind: 'custom', custom_category_id: 'parent', allocated_amount: 300, spent_amount: 150 },
        { ...baseCat, id: 'c', category_kind: 'custom', custom_category_id: 'child', custom_parent_id: 'parent', allocated_amount: 50, spent_amount: 20 },
      ])
    )
    expect(m.total_allocated).toBe(300)
    expect(m.total_spent).toBe(150)
  })

  it('counts a standalone detailed (no budgeted primary) normally', () => {
    const m = computeBudgetMetrics(
      budget([
        { ...baseCat, id: 'c', category_kind: 'pfc', pfc_primary: 'FOOD_AND_DRINK', pfc_detailed: 'FOOD_AND_DRINK_FAST_FOOD', allocated_amount: 50, spent_amount: 20 },
      ])
    )
    expect(m.total_allocated).toBe(50)
    expect(m.total_spent).toBe(20)
  })
})

describe('filterTopLevel / isSubAllocationWithin — match_logic containment (#1630)', () => {
  // Mirrors backend TestBudgetCategory_IsSubAllocationWithin_MatchLogicContainment
  // (domain/budget.go isContainingMatchLogic/isContainedMatchLogic) — the frontend and
  // backend predicates must agree on which rows are sub-allocations.

  it('excludes a plain category_only child under a category_only parent (baseline #1203)', () => {
    const result = filterTopLevel([
      { ...baseCat, id: 'p', category_kind: 'pfc', pfc_primary: 'FOOD_AND_DRINK', allocated_amount: 300, spent_amount: 150, match_logic: 'category_only' },
      { ...baseCat, id: 'c', category_kind: 'pfc', pfc_primary: 'FOOD_AND_DRINK', pfc_detailed: 'FOOD_AND_DRINK_FAST_FOOD', allocated_amount: 50, spent_amount: 20, match_logic: 'category_only' },
    ])
    expect(result.map((c) => c.id)).toEqual(['p'])
  })

  it('does NOT exclude a child when the PARENT is "and" (no plain-category envelope)', () => {
    const result = filterTopLevel([
      { ...baseCat, id: 'p', category_kind: 'pfc', pfc_primary: 'FOOD_AND_DRINK', allocated_amount: 300, spent_amount: 150, match_logic: 'and' },
      { ...baseCat, id: 'c', category_kind: 'pfc', pfc_primary: 'FOOD_AND_DRINK', pfc_detailed: 'FOOD_AND_DRINK_FAST_FOOD', allocated_amount: 50, spent_amount: 20, match_logic: 'category_only' },
    ])
    expect(result.map((c) => c.id).sort()).toEqual(['c', 'p'])
  })

  it('does NOT exclude a "tag_only" child even when the parent is category_only (child can escape the category clause)', () => {
    const result = filterTopLevel([
      { ...baseCat, id: 'p', category_kind: 'pfc', pfc_primary: 'FOOD_AND_DRINK', allocated_amount: 300, spent_amount: 150, match_logic: 'category_only' },
      { ...baseCat, id: 'c', category_kind: 'pfc', pfc_primary: 'FOOD_AND_DRINK', pfc_detailed: 'FOOD_AND_DRINK_FAST_FOOD', allocated_amount: 50, spent_amount: 20, match_logic: 'tag_only' },
    ])
    expect(result.map((c) => c.id).sort()).toEqual(['c', 'p'])
  })

  it('DOES exclude a child when the parent is "or" and the child is "and" (still contained)', () => {
    const result = filterTopLevel([
      { ...baseCat, id: 'p', category_kind: 'pfc', pfc_primary: 'FOOD_AND_DRINK', allocated_amount: 300, spent_amount: 150, match_logic: 'or' },
      { ...baseCat, id: 'c', category_kind: 'pfc', pfc_primary: 'FOOD_AND_DRINK', pfc_detailed: 'FOOD_AND_DRINK_FAST_FOOD', allocated_amount: 50, spent_amount: 20, match_logic: 'and' },
    ])
    expect(result.map((c) => c.id)).toEqual(['p'])
  })

  it("treats '' as category_only for both parent and child", () => {
    const result = filterTopLevel([
      { ...baseCat, id: 'p', category_kind: 'pfc', pfc_primary: 'FOOD_AND_DRINK', allocated_amount: 300, spent_amount: 150, match_logic: '' as BudgetCategory['match_logic'] },
      { ...baseCat, id: 'c', category_kind: 'pfc', pfc_primary: 'FOOD_AND_DRINK', pfc_detailed: 'FOOD_AND_DRINK_FAST_FOOD', allocated_amount: 50, spent_amount: 20, match_logic: '' as BudgetCategory['match_logic'] },
    ])
    expect(result.map((c) => c.id)).toEqual(['p'])
  })
})

describe('computeParentAllocationRemainder — Issue #1630 Task 4', () => {
  const parent = {
    ...baseCat,
    id: 'p',
    category_kind: 'pfc',
    pfc_primary: 'RENT_AND_UTILITIES',
    allocated_amount: 500,
    spent_amount: 0,
  } as BudgetCategory

  it('subtracts sub-allocation children from the parent\'s own allocation', () => {
    const electric = {
      ...baseCat,
      id: 'electric',
      category_kind: 'pfc',
      pfc_primary: 'RENT_AND_UTILITIES',
      pfc_detailed: 'RENT_AND_UTILITIES_ELECTRIC',
      allocated_amount: 300,
      spent_amount: 0,
    } as BudgetCategory
    const cable = {
      ...baseCat,
      id: 'cable',
      category_kind: 'pfc',
      pfc_primary: 'RENT_AND_UTILITIES',
      pfc_detailed: 'RENT_AND_UTILITIES_CABLE',
      allocated_amount: 200,
      spent_amount: 0,
    } as BudgetCategory
    // $500 utilities contains $300 electric + $200 cable, leaving $0 for anything else.
    expect(computeParentAllocationRemainder(parent, [electric, cable])).toBe(0)
  })

  it('goes NEGATIVE when children over-allocate the parent (legal, persistable state)', () => {
    const electric = { ...baseCat, id: 'electric', category_kind: 'pfc', pfc_primary: 'RENT_AND_UTILITIES', pfc_detailed: 'RENT_AND_UTILITIES_ELECTRIC', allocated_amount: 400, spent_amount: 0 } as BudgetCategory
    const cable = { ...baseCat, id: 'cable', category_kind: 'pfc', pfc_primary: 'RENT_AND_UTILITIES', pfc_detailed: 'RENT_AND_UTILITIES_CABLE', allocated_amount: 200, spent_amount: 0 } as BudgetCategory
    // $600 of children under a $500 parent -> remainder is -$100.
    expect(computeParentAllocationRemainder(parent, [electric, cable])).toBe(-100)
  })

  it('ignores a child that escapes containment via match_logic (never carved out of the parent)', () => {
    const escapee = {
      ...baseCat,
      id: 'escapee',
      category_kind: 'pfc',
      pfc_primary: 'RENT_AND_UTILITIES',
      pfc_detailed: 'RENT_AND_UTILITIES_GAS',
      allocated_amount: 9000,
      spent_amount: 0,
      match_logic: 'tag_only',
    } as BudgetCategory
    expect(computeParentAllocationRemainder(parent, [escapee])).toBe(500)
  })

  it('returns the full allocation when there are no children', () => {
    expect(computeParentAllocationRemainder(parent, [])).toBe(500)
  })
})

describe('computeParentDirectSpend — Issue #1630 Task 4', () => {
  it("subtracts only sub-allocation children's spend from the parent's own rolled spend", () => {
    const parent = { ...baseCat, id: 'p', category_kind: 'pfc', pfc_primary: 'RENT_AND_UTILITIES', allocated_amount: 500, spent_amount: 480 } as BudgetCategory
    const electric = { ...baseCat, id: 'electric', category_kind: 'pfc', pfc_primary: 'RENT_AND_UTILITIES', pfc_detailed: 'RENT_AND_UTILITIES_ELECTRIC', allocated_amount: 300, spent_amount: 300 } as BudgetCategory
    const cable = { ...baseCat, id: 'cable', category_kind: 'pfc', pfc_primary: 'RENT_AND_UTILITIES', pfc_detailed: 'RENT_AND_UTILITIES_CABLE', allocated_amount: 200, spent_amount: 150 } as BudgetCategory
    // parent's own rolled spend ($480) already contains electric+cable's $450 (category match) —
    // the water bill itself only cost $30.
    expect(computeParentDirectSpend(parent, [electric, cable])).toBe(30)
  })

  it('does not subtract a child that escapes containment (its spend was never rolled into the parent)', () => {
    const parent = { ...baseCat, id: 'p', category_kind: 'pfc', pfc_primary: 'RENT_AND_UTILITIES', allocated_amount: 500, spent_amount: 100 } as BudgetCategory
    const escapee = { ...baseCat, id: 'escapee', category_kind: 'pfc', pfc_primary: 'RENT_AND_UTILITIES', pfc_detailed: 'RENT_AND_UTILITIES_GAS', allocated_amount: 50, spent_amount: 9000, match_logic: 'tag_only' } as BudgetCategory
    expect(computeParentDirectSpend(parent, [escapee])).toBe(100)
  })
})

describe('isSubAllocationOfParent — Issue #1630 Task 4', () => {
  const parent = { ...baseCat, id: 'p', category_kind: 'pfc', pfc_primary: 'FOOD_AND_DRINK', allocated_amount: 300, spent_amount: 150 } as BudgetCategory

  it('true for a contained PFC detailed child of this exact primary', () => {
    const child = { ...baseCat, id: 'c', category_kind: 'pfc', pfc_primary: 'FOOD_AND_DRINK', pfc_detailed: 'FOOD_AND_DRINK_FAST_FOOD', allocated_amount: 50, spent_amount: 20 } as BudgetCategory
    expect(isSubAllocationOfParent(child, parent)).toBe(true)
  })

  it('false for a child of a DIFFERENT primary', () => {
    const child = { ...baseCat, id: 'c', category_kind: 'pfc', pfc_primary: 'RENT_AND_UTILITIES', pfc_detailed: 'RENT_AND_UTILITIES_ELECTRIC', allocated_amount: 50, spent_amount: 20 } as BudgetCategory
    expect(isSubAllocationOfParent(child, parent)).toBe(false)
  })

  it('false when the child escapes containment via match_logic', () => {
    const child = { ...baseCat, id: 'c', category_kind: 'pfc', pfc_primary: 'FOOD_AND_DRINK', pfc_detailed: 'FOOD_AND_DRINK_FAST_FOOD', allocated_amount: 50, spent_amount: 20, match_logic: 'or' } as BudgetCategory
    expect(isSubAllocationOfParent(child, parent)).toBe(false)
  })
})
