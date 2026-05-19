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

export type DataTableVirtualizationOpts = {
  enabled: boolean
  /** px; used as initial estimate for measureElement */
  estimateRowHeight: number
  /** default 8 */
  overscan?: number
  /** CSS value, e.g. 'calc(100dvh - 240px)' */
  scrollContainerHeight?: string
  /** fires when last virtual item index >= count - prefetchRows */
  onScrollEnd?: () => void
  /** default 5; rows from end before firing onScrollEnd */
  prefetchRows?: number
  /** shows loading sentinel row */
  isFetchingMore?: boolean
  /** shows sentinel row (but not loading spinner) */
  hasMore?: boolean
  /** default true; uses virtualizer.measureElement for dynamic row heights */
  dynamicRowHeight?: boolean
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
  /**
   * Opt-in row virtualization. When absent or enabled:false, DataTable renders
   * exactly as today — no behavior change for other consumers.
   */
  virtualization?: DataTableVirtualizationOpts
  /**
   * When provided AND `loading` is true AND `data` is empty, render this many
   * shimmer placeholder rows instead of the empty-state. Pass the React Query
   * flag that goes true only when there is no cached data yet — `isPending` from
   * `useQuery` or `isLoading` from `useInfiniteQuery`. Using `isFetching` will
   * flicker back to skeleton on background refetches with empty filter results.
   * Each skeleton row contains one `<td>` per visible column (plus one for
   * `rowActions` when present) so column widths stay stable via `<colgroup>`.
   */
  skeletonRows?: number
  /**
   * When true, `<thead>` is sticky to the top of the nearest scrolling ancestor.
   * Implicitly true when `virtualization.enabled === true`.
   * Use `stickyHeaderTopOffset` to clear a fixed page header above the table
   * (e.g., `"4rem"` for the global app header at `h-16`).
   */
  stickyHeader?: boolean
  /**
   * CSS length value for the sticky header's `top` offset.
   * Overrides the default `top-0` from the CVA variant.
   * Only applied when `stickyHeader` is true or when `virtualization.enabled` is true.
   */
  stickyHeaderTopOffset?: string
}
