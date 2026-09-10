/**
 * computeBudgetMetrics top-level-only totals (Issue #1203).
 * formatBudgetCategoryLabel — savings kind (#1300).
 */
import { describe, it, expect } from 'bun:test'
import { computeBudgetMetrics, filterTopLevel, formatBudgetCategoryLabel, type Budget, type BudgetCategory } from './types'

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
