import type { ReactNode } from 'react'
import type { RangeValue } from '../ColumnFilterDropdown'
import type { RowSelectionState } from '@/shared/lib/tanstack'

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
  /** Phase 3a: opt-in per-column resize handle */
  resizable?: boolean
  /** Default pixel width used for resize reset (Home key) and initial sizing */
  defaultSize?: number
  /** Phase 3b: opt-in column reordering via drag-and-drop / keyboard */
  reorderable?: boolean
  /** Phase 3c: opt-in sticky column pinning. Left-pin always honored; right-pin degrades to non-sticky on mobile (max-md). */
  defaultPinned?: 'left' | 'right'
  /** Phase 3d: opt-in column visibility toggle. Column appears in ColumnVisibilityMenu when true. */
  hideable?: boolean
}

export interface DataTableProps<T> {
  data: T[]
  columns: ColumnDef<T>[]
  rowKey: (row: T) => string
  /** Optional table identifier forwarded to analytics events (e.g. 'transactions'). */
  tableId?: string
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
  /** Row selection state (Phase 1 skeleton — no UI rendered yet). */
  rowSelection?: RowSelectionState
  /** Called when row selection changes (Phase 1 skeleton — no UI rendered yet). */
  onRowSelectionChange?: (selection: RowSelectionState) => void
  /**
   * Phase 3a: controlled column sizing. Keys are column IDs, values are pixel widths.
   * If omitted, DataTable manages sizing internally with useState.
   */
  columnSizing?: Record<string, number>
  onColumnSizingChange?: (sizing: Record<string, number>) => void
  /**
   * Phase 3b: controlled column order (array of column ids).
   * If omitted, DataTable manages order internally with useState.
   */
  columnOrder?: string[]
  onColumnOrderChange?: (order: string[]) => void
  /**
   * Phase 3d: controlled column visibility. Keys are column IDs, values are booleans.
   * Only columns with hideable:true participate; others are always visible.
   */
  columnVisibility?: Record<string, boolean>
  onColumnVisibilityChange?: (visibility: Record<string, boolean>) => void
  /**
   * Phase 3d: table density — controls cell padding.
   * 'compact' | 'comfortable' (default) | 'spacious'
   */
  density?: 'compact' | 'comfortable' | 'spacious'
}
