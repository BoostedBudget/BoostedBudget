import { useCallback, useRef } from 'react'
import { cva } from 'class-variance-authority'
import { cn } from '@/lib/utils'
import { TableHeaderCell } from '../TableHeaderCell'
import type { RangeValue } from '../ColumnFilterDropdown'
import {
  useReactTable,
  getCoreRowModel,
} from '@/shared/lib/tanstack'
import type { DataTableProps, SortState, SortDirection, ColumnDef } from './types'
import { bbSortToTanStack } from './adapters/sortAdapter'
import { bbFiltersToTanStack } from './adapters/filterAdapter'
import { useColumnResize } from './useColumnResize'
import { ResizeHandle } from './ResizeHandle'

const tableVariants = cva(
  'w-full border-collapse overflow-visible rounded-lg border border-border bg-card',
  {
    variants: {
      loading: {
        true: 'pointer-events-none opacity-60',
        false: '',
      },
    },
    defaultVariants: { loading: false },
  }
)

const headerCellVariants = cva(
  'relative border-b border-border px-4 py-3 text-xs font-semibold uppercase tracking-[0.05em] text-muted-foreground',
  {
    variants: {
      align: { left: 'text-left', right: 'text-right', center: 'text-center' },
      action: { true: 'w-24 max-md:w-16', false: '' },
    },
    defaultVariants: { align: 'left', action: false },
  }
)

const bodyCellVariants = cva(
  'border-b border-border px-4 py-3.5 text-sm align-middle max-md:px-3 max-md:py-2.5',
  {
    variants: {
      align: { left: 'text-left', right: 'text-right', center: 'text-center' },
      numeric: { true: 'tabular-nums', false: '' },
    },
    defaultVariants: { align: 'left', numeric: false },
  }
)

function getAriaSort(
  sort: SortState,
  colId: string
): React.AriaAttributes['aria-sort'] {
  if (sort?.columnId !== colId) return undefined
  return sort.direction === 'asc' ? 'ascending' : 'descending'
}

function renderHeaderContent<T>(
  col: ColumnDef<T>,
  sort: SortState,
  filters: Record<string, string[] | RangeValue | null>,
  handleSort: (colId: string, _: SortDirection) => void,
  handleFilterChange: (colId: string, value: string[] | RangeValue | null) => void
) {
  if (typeof col.header !== 'string') {
    // sortable/filter props are silently ignored for ReactNode headers — warn in dev
    if (import.meta.env.DEV && (col.sortable || col.filter)) {
      console.warn(
        `[DataTable] Column "${col.id}" has a ReactNode header but also has sortable/filter props — these will be ignored. Use header: string for sortable or filterable columns.`
      )
    }
    return col.header
  }

  const selectFilter = col.filter?.type === 'select' ? col.filter : undefined

  // Coerce filterValue to match this column's filter kind so type mismatches from
  // the parent's initial FilterState don't cause runtime crashes in ColumnFilterDropdown.
  const rawFilterValue = filters[col.id]
  const filterValue: string[] | RangeValue | null | undefined =
    col.filter?.type === 'select'
      ? (Array.isArray(rawFilterValue) ? rawFilterValue : undefined)
      : col.filter?.type === 'range'
      ? (!Array.isArray(rawFilterValue) ? rawFilterValue : null)
      : rawFilterValue

  return (
    <TableHeaderCell
      label={col.header}
      className={col.align === 'right' ? 'w-full justify-end' : undefined}
      sortable={col.sortable}
      sortColumn={col.id}
      currentSortColumn={sort?.columnId}
      currentSortDirection={sort?.direction ?? 'asc'}
      onSort={handleSort}
      filterable={!!col.filter}
      filterType={col.filter?.type}
      filterOptions={selectFilter?.options}
      filterValue={filterValue}
      multiSelect={selectFilter?.multiSelect}
      searchable={selectFilter?.searchable}
      onFilterChange={(value) => handleFilterChange(col.id, value)}
    />
  )
}

export function DataTable<T,>({
  data,
  columns,
  rowKey,
  sort,
  onSortChange,
  filters,
  onFiltersChange,
  emptyState,
  caption,
  className,
  loading = false,
  onRowClick,
  rowClassName,
  maskedColumnIds = [],
  rowActions,
  // rowSelection and onRowSelectionChange accepted but UI not yet implemented (Phase 1 skeleton)
  rowSelection: _rowSelection,
  onRowSelectionChange: _onRowSelectionChange,
  columnSizing: controlledSizing,
  onColumnSizingChange,
}: DataTableProps<T>) {
  const totalCols = columns.length + (rowActions ? 1 : 0)

  const { sizing, isFinePointer, colRefs, getWidth, handleKeyDown, handlePointerDown } =
    useColumnResize(controlledSizing, onColumnSizingChange)

  // Stable ref for <col> element lookups during pointer drag
  const colElRefs = useRef<Map<string, HTMLTableColElement | null>>(new Map())
  function setColRef(columnId: string) {
    return (el: HTMLTableColElement | null) => {
      colElRefs.current.set(columnId, el)
      colRefs.current.set(columnId, el)
    }
  }

  const handleSort = useCallback(
    (colId: string, _dirFromCell: SortDirection) => {
      const currentSort = sort as SortState
      if (currentSort?.columnId !== colId) {
        onSortChange({ columnId: colId, direction: 'asc' })
      } else if (currentSort.direction === 'asc') {
        onSortChange({ columnId: colId, direction: 'desc' })
      } else {
        onSortChange(null)
      }
    },
    [sort, onSortChange]
  )

  const handleFilterChange = useCallback(
    (colId: string, value: string[] | RangeValue | null) => {
      onFiltersChange({ ...filters, [colId]: value })
    },
    [filters, onFiltersChange]
  )

  // Wire up TanStack's useReactTable for state management.
  // manualSorting/manualFiltering/manualPagination = true → we own data fetching.
  // We still render our own <td> markup (never flexRender) to keep data-ph-mask in our control.
  // Sort and filter events are owned by handleSort/handleFilterChange above (not TanStack's
  // onChange callbacks) — this preserves the data-ph-mask guarantee on <td> and the
  // controlled server-state contract. TanStack state is kept in sync via the state: { sorting, columnFilters } props.
  const table = useReactTable({
    data,
    columns: [],
    getCoreRowModel: getCoreRowModel(),
    manualSorting: true,
    manualFiltering: true,
    manualPagination: true,
    state: {
      sorting: bbSortToTanStack(sort),
      columnFilters: bbFiltersToTanStack(filters),
    },
  })

  // Use TanStack's row model for iteration — row.original gives back the typed T.
  const rows = table.getRowModel().rows

  return (
    <table
      className={cn(tableVariants({ loading }), className)}
      aria-busy={loading ? true : undefined}
    >
      {caption && (
        <caption className="caption-top text-left text-sm font-semibold text-foreground mb-2">
          {caption}
        </caption>
      )}
      <colgroup>
        {columns.map((col) => (
          <col
            key={col.id}
            ref={setColRef(col.id)}
            style={sizing[col.id] ? { width: `${sizing[col.id]}px` } : undefined}
          />
        ))}
        {rowActions && <col style={{ width: '96px' }} />}
      </colgroup>
      <thead>
        <tr className="bg-muted/50">
          {columns.map((col) => (
            <th
              key={col.id}
              scope="col"
              className={cn(
                headerCellVariants({ align: col.align }),
                col.widthClass
              )}
              aria-sort={getAriaSort(sort, col.id)}
            >
              {renderHeaderContent(col, sort, filters, handleSort, handleFilterChange)}
              {col.resizable && (
                <ResizeHandle
                  columnId={col.id}
                  currentWidth={getWidth(col.id, col.defaultSize)}
                  isCoarsePointer={!isFinePointer}
                  onKeyDown={(e) => handleKeyDown(e, col.id, col.defaultSize)}
                  onPointerDown={(e) =>
                    handlePointerDown(e, col.id, colElRefs.current.get(col.id) ?? null)
                  }
                />
              )}
            </th>
          ))}
          {rowActions && (
            <th
              scope="col"
              className={headerCellVariants({ align: 'right', action: true })}
            >
              Actions
            </th>
          )}
        </tr>
      </thead>
      <tbody>
        {rows.length === 0 ? (
          <tr>
            <td colSpan={totalCols}>{emptyState}</td>
          </tr>
        ) : (
          rows.map((tanStackRow) => {
            const row = tanStackRow.original
            return (
              <tr
                key={rowKey(row)}
                className={cn(
                  'transition-colors',
                  onRowClick && 'cursor-pointer hover:bg-muted/50',
                  rowClassName?.(row)
                )}
                onClick={onRowClick ? () => onRowClick(row) : undefined}
              >
                {columns.map((col) => {
                  const isMasked = maskedColumnIds.includes(col.id)
                  const content = col.render
                    ? col.render(row)
                    : col.key !== undefined
                      ? String(row[col.key] ?? '')
                      : null

                  return (
                    <td
                      key={col.id}
                      className={bodyCellVariants({
                        align: col.align,
                        numeric: col.numeric ?? false,
                      })}
                      {...(isMasked && { 'data-ph-mask': '' })}
                    >
                      {content}
                    </td>
                  )
                })}
                {rowActions && (
                  <td
                    className={bodyCellVariants({ align: 'right' })}
                    onClick={(e) => e.stopPropagation()}
                  >
                    {rowActions(row)}
                  </td>
                )}
              </tr>
            )
          })
        )}
      </tbody>
    </table>
  )
}
