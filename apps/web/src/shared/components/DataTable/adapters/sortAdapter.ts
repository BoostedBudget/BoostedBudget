import type { SortingState } from '@/shared/lib/tanstack'
import type { SortState } from '../types'

/**
 * Convert BoostedBudget's SortState (or null) to TanStack's SortingState array.
 */
export function bbSortToTanStack(sort: SortState): SortingState {
  if (!sort) return []
  return [{ id: sort.columnId, desc: sort.direction === 'desc' }]
}

/**
 * Convert TanStack's SortingState array back to BoostedBudget's SortState.
 * Returns null when the array is empty (meaning "no sort active").
 */
export function tanStackSortToBb(sorting: SortingState): SortState {
  if (sorting.length === 0) return null
  const { id, desc } = sorting[0]
  return { columnId: id, direction: desc ? 'desc' : 'asc' }
}
