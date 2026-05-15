import type { ColumnFiltersState } from '@/shared/lib/tanstack'
import type { FilterState } from '../types'
import type { RangeValue } from '../../ColumnFilterDropdown'

/**
 * Convert BoostedBudget's FilterState to TanStack's ColumnFiltersState array.
 * Null values are dropped (they represent "no filter active" for that column).
 */
export function bbFiltersToTanStack(
  filters: FilterState
): ColumnFiltersState {
  return Object.entries(filters)
    .filter(([, value]) => value !== null)
    .map(([id, value]) => ({ id, value }))
}

/**
 * Convert TanStack's ColumnFiltersState array back to BoostedBudget's FilterState.
 */
export function tanStackFiltersToBb(
  columnFilters: ColumnFiltersState
): FilterState {
  const result: FilterState = {}
  for (const { id, value } of columnFilters) {
    result[id] = value as string[] | RangeValue | null
  }
  return result
}
