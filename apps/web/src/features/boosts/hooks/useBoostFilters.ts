import type { SortState } from '@/shared/components/DataTable'
import { useSortUrlState } from '@/shared/hooks/useSortUrlState'
import { BOOST_SORTABLE_COLUMNS } from '../components/BoostTable'

export interface UseBoostFiltersResult {
  sort: SortState | null
  isDefaultSort: boolean
  setSort: (next: SortState | null) => void
  resetSort: () => void
  toggleSortColumn: (columnId: string) => void
}

export function useBoostFilters(): UseBoostFiltersResult {
  return useSortUrlState({
    defaultSort: null,
    validColumns: BOOST_SORTABLE_COLUMNS,
  })
}
