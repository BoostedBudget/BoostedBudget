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
  isContainedMatchLogic,
  isContainingMatchLogic,
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
  it('names shared groups and distinguishes automatic Other from Uncategorized', () => {
    expect(formatBudgetCategoryLabel({ category_kind: 'group', group_name: 'Daily life' })).toBe('Daily life')
    expect(formatBudgetCategoryLabel({ category_kind: 'remainder' })).toBe('Other')
  })
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
  it('uses expected income, not observed income, for unallocated plans', () => {
    const m = computeBudgetMetrics({ ...budget([]), income_amount: 200, expected_income: 1000 })
    expect(m.unallocated_funds).toBe(1000)
    expect(m.is_fully_allocated).toBe(false)
  })
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

  it('excludes a transitive custom grandchild from headline totals (Issue #1630 Task B2)', () => {
    // Same shape as the domain-level filterTopLevel test below, exercised through
    // computeBudgetMetrics — this is the headline-total-level assertion the backend/frontend
    // divergence was actually about: the two totals must agree on identical data.
    const m = computeBudgetMetrics(
      budget([
        { ...baseCat, id: 'x', category_kind: 'pfc', pfc_primary: 'FOOD_AND_DRINK', allocated_amount: 300, spent_amount: 150 },
        {
          ...baseCat,
          id: 'g',
          category_kind: 'custom',
          custom_category_id: 'grandchild',
          custom_parent_id: 'unbudgeted-parent-c',
          custom_grandparent_pfc_primary: 'FOOD_AND_DRINK',
          allocated_amount: 50,
          spent_amount: 20,
        },
      ])
    )
    expect(m.total_allocated).toBe(300)
    expect(m.total_spent).toBe(150)
  })

  it('treats pfc_detailed: "" as absent — matching backend domain.hasPFCDetailed (#1630)', () => {
    // Before this fix, Go/SQL treated a present-but-empty pfc_detailed as a CHILD (so this
    // row was excluded from the backend total), while `!cat.pfc_detailed` here already
    // treated '' as falsy (so the frontend included it) — the two headline totals disagreed
    // on identical data. Both rows are primary-only (parent) shapes, so both count in full;
    // this is a duplicate-parent-identity shape, not a sub-allocation.
    const m = computeBudgetMetrics(
      budget([
        { ...baseCat, id: 'p', category_kind: 'pfc', pfc_primary: 'FOOD_AND_DRINK', allocated_amount: 300, spent_amount: 150 },
        { ...baseCat, id: 'c', category_kind: 'pfc', pfc_primary: 'FOOD_AND_DRINK', pfc_detailed: '', allocated_amount: 50, spent_amount: 20 },
      ])
    )
    expect(m.total_allocated).toBe(350)
    expect(m.total_spent).toBe(170)
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

  // Issue #1630 Task B2: pfc X <- custom C (custom_parent_pfc_primary = X, NOT itself
  // budgeted here) <- custom G (custom_parent_id = C, budgeted). Only X and G have budget
  // rows; C has none at all. Mirrors backend domain_test.go's "custom grandchild nested under
  // budgeted PFC primary two hops through an unbudgeted parent" — the backend's widened
  // roll-up rolls G's transactions into X's SpentAmount via a second join hop regardless of
  // whether C is budgeted, so the frontend must exclude G from the top-level total too or the
  // two headline totals disagree on identical data.
  it('excludes a transitive custom grandchild nested under a budgeted PFC root two hops through an unbudgeted parent', () => {
    const result = filterTopLevel([
      { ...baseCat, id: 'x', category_kind: 'pfc', pfc_primary: 'FOOD_AND_DRINK', allocated_amount: 300, spent_amount: 150 },
      {
        ...baseCat,
        id: 'g',
        category_kind: 'custom',
        custom_category_id: 'grandchild',
        custom_parent_id: 'unbudgeted-parent-c',
        custom_grandparent_pfc_primary: 'FOOD_AND_DRINK',
        allocated_amount: 50,
        spent_amount: 20,
      },
    ])
    expect(result.map((c) => c.id)).toEqual(['x'])
  })

  it('does NOT exclude the transitive grandchild when its grandparent PFC primary is NOT budgeted', () => {
    const result = filterTopLevel([
      { ...baseCat, id: 'x', category_kind: 'pfc', pfc_primary: 'RENT_AND_UTILITIES', allocated_amount: 300, spent_amount: 150 },
      {
        ...baseCat,
        id: 'g',
        category_kind: 'custom',
        custom_category_id: 'grandchild',
        custom_parent_id: 'unbudgeted-parent-c',
        custom_grandparent_pfc_primary: 'FOOD_AND_DRINK',
        allocated_amount: 50,
        spent_amount: 20,
      },
    ])
    expect(result.map((c) => c.id).sort()).toEqual(['g', 'x'])
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

  it('true for a transitive custom grandchild whose grandparent PFC primary matches this parent (Issue #1630 Task B2)', () => {
    const grandchild = {
      ...baseCat,
      id: 'g',
      category_kind: 'custom',
      custom_category_id: 'grandchild',
      custom_parent_id: 'unbudgeted-parent-c',
      custom_grandparent_pfc_primary: 'FOOD_AND_DRINK',
      allocated_amount: 50,
      spent_amount: 20,
    } as BudgetCategory
    expect(isSubAllocationOfParent(grandchild, parent)).toBe(true)
  })

  it('false for a transitive custom grandchild whose grandparent PFC primary is a DIFFERENT root', () => {
    const grandchild = {
      ...baseCat,
      id: 'g',
      category_kind: 'custom',
      custom_category_id: 'grandchild',
      custom_parent_id: 'unbudgeted-parent-c',
      custom_grandparent_pfc_primary: 'RENT_AND_UTILITIES',
      allocated_amount: 50,
      spent_amount: 20,
    } as BudgetCategory
    expect(isSubAllocationOfParent(grandchild, parent)).toBe(false)
  })

  it('{ directOnly: true } reports false for the same transitive grandchild that is true without it (delete-dialog fix)', () => {
    const grandchild = {
      ...baseCat,
      id: 'g',
      category_kind: 'custom',
      custom_category_id: 'grandchild',
      custom_parent_id: 'unbudgeted-parent-c',
      custom_grandparent_pfc_primary: 'FOOD_AND_DRINK',
      allocated_amount: 50,
      spent_amount: 20,
    } as BudgetCategory
    expect(isSubAllocationOfParent(grandchild, parent)).toBe(true)
    expect(isSubAllocationOfParent(grandchild, parent, { directOnly: true })).toBe(false)
  })

  it('{ directOnly: true } still reports true for an actual direct child', () => {
    const child = { ...baseCat, id: 'c', category_kind: 'pfc', pfc_primary: 'FOOD_AND_DRINK', pfc_detailed: 'FOOD_AND_DRINK_FAST_FOOD', allocated_amount: 50, spent_amount: 20 } as BudgetCategory
    expect(isSubAllocationOfParent(child, parent, { directOnly: true })).toBe(true)
  })
})

describe('containment predicates — exported for shared use (#1686)', () => {
  // The two predicates below are the single source of truth for "can this parent adopt this
  // child" (isContainingMatchLogic) and "can this child be adopted" (isContainedMatchLogic).
  // They are exported so the display grouping pass (groupBudgetCategoriesByPrimary) can apply
  // the SAME gates the remainder arithmetic (isSubAllocationOfParent) applies — otherwise a
  // card can show a child it does not count (issue #1686).

  it('isContainingMatchLogic treats undefined, "", category_only and or as containing', () => {
    expect(isContainingMatchLogic(undefined)).toBe(true)
    expect(isContainingMatchLogic('')).toBe(true)
    expect(isContainingMatchLogic('category_only')).toBe(true)
    expect(isContainingMatchLogic('or')).toBe(true)
  })

  it('isContainingMatchLogic rejects and and tag_only (no plain-category envelope)', () => {
    expect(isContainingMatchLogic('and')).toBe(false)
    expect(isContainingMatchLogic('tag_only')).toBe(false)
  })

  it('isContainedMatchLogic treats undefined, "", category_only and and as contained', () => {
    expect(isContainedMatchLogic(undefined)).toBe(true)
    expect(isContainedMatchLogic('')).toBe(true)
    expect(isContainedMatchLogic('category_only')).toBe(true)
    expect(isContainedMatchLogic('and')).toBe(true)
  })

  it('isContainedMatchLogic rejects or and tag_only (child can escape the category clause)', () => {
    expect(isContainedMatchLogic('or')).toBe(false)
    expect(isContainedMatchLogic('tag_only')).toBe(false)
  })

  it('isSubAllocationOfParent rejects an orphan detailed parent (a detailed row cannot adopt a child)', () => {
    // Issue #1686: a detailed row with no matching primary-only row is a SIBLING of any custom
    // sharing its pfc_primary, never its parent. Both child kinds must be refused.
    const orphanParent = {
      ...baseCat,
      id: 'orphan',
      category_kind: 'pfc',
      pfc_primary: 'RENT_AND_UTILITIES',
      pfc_detailed: 'RENT_AND_UTILITIES_GAS_AND_ELECTRICITY',
      allocated_amount: 215,
      spent_amount: 192.08,
    } as BudgetCategory
    const pfcChild = {
      ...baseCat,
      id: 'pfc-child',
      category_kind: 'pfc',
      pfc_primary: 'RENT_AND_UTILITIES',
      pfc_detailed: 'RENT_AND_UTILITIES_ELECTRIC',
      allocated_amount: 50,
      spent_amount: 20,
    } as BudgetCategory
    const customChild = {
      ...baseCat,
      id: 'custom-child',
      category_kind: 'custom',
      custom_category_id: 'hoa',
      custom_parent_pfc_primary: 'RENT_AND_UTILITIES',
      allocated_amount: 423.2,
      spent_amount: 423.2,
    } as BudgetCategory
    expect(isSubAllocationOfParent(pfcChild, orphanParent)).toBe(false)
    expect(isSubAllocationOfParent(customChild, orphanParent)).toBe(false)
  })

  it('isSubAllocationOfParent rejects a non-contained pair (or child under a category_only parent)', () => {
    const parent = { ...baseCat, id: 'p', category_kind: 'pfc', pfc_primary: 'FOOD_AND_DRINK', allocated_amount: 300, spent_amount: 150 } as BudgetCategory
    const child = { ...baseCat, id: 'c', category_kind: 'pfc', pfc_primary: 'FOOD_AND_DRINK', pfc_detailed: 'FOOD_AND_DRINK_FAST_FOOD', allocated_amount: 50, spent_amount: 20, match_logic: 'or' } as BudgetCategory
    expect(isSubAllocationOfParent(child, parent)).toBe(false)
  })
})
