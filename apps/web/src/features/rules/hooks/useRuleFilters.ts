import type { SortState } from '@/shared/components/DataTable'
import { useSortUrlState } from '@/shared/hooks/useSortUrlState'
import { RULE_SORTABLE_COLUMNS } from '../components/RuleTable'

export interface UseRuleFiltersResult {
  sort: SortState | null
  isDefaultSort: boolean
  setSort: (next: SortState | null) => void
  resetSort: () => void
  toggleSortColumn: (columnId: string) => void
}

export function useRuleFilters(): UseRuleFiltersResult {
  const { sort, setSort, resetSort, toggleSortColumn, isDefaultSort } = useSortUrlState({
    defaultSort: null,
    validColumns: RULE_SORTABLE_COLUMNS,
  })

  return {
    sort,
    isDefaultSort,
    setSort,
    resetSort,
    toggleSortColumn,
  }
}
