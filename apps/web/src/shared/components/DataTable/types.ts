import type { ReactNode } from 'react'
import type { RangeValue } from '../ColumnFilterDropdown'

export type { RangeValue }

export type SortDirection = 'asc' | 'desc'
export type SortState = { columnId: string; direction: SortDirection } | null
export type FilterState = Record<string, string[] | RangeValue | null>

export type ColumnDef<T> = {
  id: string
  header: string | ReactNode
  align?: 'left' | 'right' | 'center'
  widthClass?: string
  sortable?: boolean
  sortAccessor?: (row: T) => string | number | Date
  key?: keyof T
  render?: (row: T) => ReactNode
  numeric?: boolean
  filter?:
    | {
        type: 'select'
        options: Array<{ label: string; value: string }>
        multiSelect?: boolean
        searchable?: boolean
      }
    | { type: 'range'; unit?: 'number' | 'currency' | 'percent' }
}

export interface DataTableProps<T> {
  data: T[]
  columns: ColumnDef<T>[]
  rowKey: (row: T) => string
  sort: SortState
  onSortChange: (s: SortState) => void
  filters: FilterState
  onFiltersChange: (f: FilterState) => void
  emptyState?: ReactNode
  caption?: string
  className?: string
  loading?: boolean
  onRowClick?: (row: T) => void
  rowClassName?: (row: T) => string
  maskedColumnIds?: string[]
  rowActions?: (row: T) => ReactNode
}
