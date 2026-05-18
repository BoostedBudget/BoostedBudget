import { useCallback, useEffect, useRef } from 'react'
import { cva } from 'class-variance-authority'
import { cn } from '@/lib/utils'
import { TableHeaderCell } from '../TableHeaderCell'
import type { RangeValue } from '../ColumnFilterDropdown'
import {
  useReactTable,
  getCoreRowModel,
  useVirtualizer,
} from '@/shared/lib/tanstack'
import {
  DndContext,
  closestCenter,
  PointerSensor,
  useSensor,
  useSensors,
} from '@dnd-kit/core'
import {
  SortableContext,
  horizontalListSortingStrategy,
  useSortable,
} from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import type { DataTableProps, SortState, SortDirection, ColumnDef } from './types'
import { bbSortToTanStack } from './adapters/sortAdapter'
import { bbFiltersToTanStack } from './adapters/filterAdapter'
import { useColumnResize } from './useColumnResize'
import { ResizeHandle } from './ResizeHandle'
import { useColumnOrder } from './useColumnOrder'
import { DragHandle } from './DragHandle'
import { ColumnVisibilityMenu } from './ColumnVisibilityMenu'

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
  'border-b border-border align-middle',
  {
    variants: {
      align: { left: 'text-left', right: 'text-right', center: 'text-center' },
      numeric: { true: 'tabular-nums', false: '' },
      density: {
        compact: 'px-2 py-1.5 text-xs',
        comfortable: 'px-4 py-3.5 text-sm max-md:px-3 max-md:py-2.5',
        spacious: 'px-6 py-5 text-sm',
      },
    },
    defaultVariants: { align: 'left', numeric: false, density: 'comfortable' },
  }
)

const theadVariants = cva('', {
  variants: {
    sticky: {
      true: 'sticky top-0 z-20 bg-card',
      false: '',
    },
  },
  defaultVariants: { sticky: false },
})

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

interface SortableColHeaderProps {
  col: {
    id: string
    header?: string | React.ReactNode
    reorderable?: boolean
    align?: 'left' | 'right' | 'center'
    widthClass?: string
    defaultPinned?: 'left' | 'right'
  }
  sortAriaVal: React.AriaAttributes['aria-sort']
  draggingId: string | null
  isFinePointerDnd: boolean
  onDragHandleKeyDown: (colId: string, e: React.KeyboardEvent) => void
  headerContent: React.ReactNode
  resizeHandle: React.ReactNode
}

function SortableColHeader({
  col,
  sortAriaVal,
  draggingId,
  isFinePointerDnd,
  onDragHandleKeyDown,
  headerContent,
  resizeHandle,
}: SortableColHeaderProps) {
  const { setNodeRef, transform, transition, isDragging: isDndDragging, listeners } = useSortable({ id: col.id })

  return (
    <th
      ref={col.reorderable ? setNodeRef : undefined}
      scope="col"
      aria-label={typeof col.header === 'string' && col.reorderable ? col.header : undefined}
      className={cn(
        headerCellVariants({ align: col.align }),
        col.widthClass,
        col.defaultPinned === 'left' && 'sticky left-0 z-30 bg-muted/50',
        col.defaultPinned === 'right' && 'sticky right-0 z-30 bg-muted/50 max-md:relative max-md:right-auto',
        isDndDragging && col.reorderable && 'opacity-50',
      )}
      style={col.reorderable && transform ? {
        transform: CSS.Transform.toString(transform),
        transition: transition ?? undefined,
        zIndex: isDndDragging ? 40 : undefined,
      } : undefined}
      aria-sort={sortAriaVal}
      {...(col.defaultPinned ? { 'data-pinned': col.defaultPinned } : {})}
    >
      <div className="flex items-center gap-1">
        {col.reorderable && (
          <DragHandle
            isDragging={draggingId === col.id}
            onKeyDown={(e) => onDragHandleKeyDown(col.id, e)}
            listeners={isFinePointerDnd ? listeners as Record<string, unknown> : undefined}
            columnLabel={typeof col.header === 'string' ? col.header : undefined}
          />
        )}
        {headerContent}
      </div>
      {resizeHandle}
    </th>
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
  columnOrder: controlledColumnOrder,
  onColumnOrderChange,
  columnVisibility = {},
  onColumnVisibilityChange,
  density = 'comfortable',
  tableId,
  virtualization,
  skeletonRows,
  stickyHeader,
  stickyHeaderTopOffset,
}: DataTableProps<T>) {
  // totalCols is computed after visibleColumns below

  const { sizing, isFinePointer, colRefs, getWidth, handleKeyDown, handlePointerDown } =
    useColumnResize(controlledSizing, onColumnSizingChange, tableId)

  const {
    orderedColumns,
    draggingId,
    liveText,
    isFinePointer: isFinePointerDnd,
    handleDragHandleKeyDown,
    handleDragEnd,
  } = useColumnOrder(columns, controlledColumnOrder, onColumnOrderChange, tableId)

  // Phase 3d: filter out hidden columns for rendering; orderedColumns preserves full order
  const visibleColumns = orderedColumns.filter(
    (col) => !(col.hideable && columnVisibility[col.id] === false)
  )

  const hasHideableColumns = columns.some((col) => col.hideable)
  const totalCols = visibleColumns.length + (rowActions ? 1 : 0)
  const reorderableIds = visibleColumns.filter((c) => c.reorderable).map((c) => c.id)

  const showSkeleton = loading && data.length === 0 && (skeletonRows ?? 0) > 0

  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }))

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

  const scrollRef = useRef<HTMLDivElement>(null)

  const virtualizer = useVirtualizer({
    count: virtualization?.enabled ? rows.length : 0,
    getScrollElement: () => (virtualization?.enabled ? scrollRef.current : null),
    estimateSize: () => virtualization?.estimateRowHeight ?? 40,
    overscan: virtualization?.overscan ?? 8,
    measureElement:
      virtualization?.enabled && virtualization.dynamicRowHeight !== false
        ? (el: Element) => el.getBoundingClientRect().height
        : undefined,
    enabled: virtualization?.enabled ?? false,
  })

  const virtualItems = virtualizer.getVirtualItems()

  const scrollEndFiredRef = useRef(false)
  useEffect(() => {
    if (!virtualization?.enabled || !virtualization.onScrollEnd || !virtualization.hasMore || virtualization.isFetchingMore || virtualItems.length === 0) return
    const lastIndex = virtualItems[virtualItems.length - 1].index
    const prefetch = virtualization.prefetchRows ?? 5
    if (lastIndex >= rows.length - prefetch && !scrollEndFiredRef.current) {
      scrollEndFiredRef.current = true
      virtualization.onScrollEnd()
    } else if (lastIndex < rows.length - prefetch) {
      scrollEndFiredRef.current = false
    }
  }, [virtualItems, rows.length, virtualization])


  const headerIsSticky = stickyHeader === true || virtualization?.enabled === true

  // Shared thead renderer (used by both virtualized and non-virtualized paths)
  const theadEl = (
    <thead
      className={theadVariants({ sticky: headerIsSticky })}
      style={headerIsSticky && stickyHeaderTopOffset ? { top: stickyHeaderTopOffset } : undefined}
    >
      <SortableContext items={reorderableIds} strategy={horizontalListSortingStrategy}>
        <tr className="bg-muted/50">
          {visibleColumns.map((col) => (
            <SortableColHeader
              key={col.id}
              col={col}
              sortAriaVal={getAriaSort(sort, col.id)}
              draggingId={draggingId}
              isFinePointerDnd={isFinePointerDnd}
              onDragHandleKeyDown={handleDragHandleKeyDown}
              headerContent={renderHeaderContent(col, sort, filters, handleSort, handleFilterChange)}
              resizeHandle={col.resizable ? (
                <ResizeHandle
                  columnId={col.id}
                  currentWidth={getWidth(col.id, col.defaultSize)}
                  isCoarsePointer={!isFinePointer}
                  onKeyDown={(e) => handleKeyDown(e, col.id, col.defaultSize)}
                  onPointerDown={(e) =>
                    handlePointerDown(e, col.id, colElRefs.current.get(col.id) ?? null)
                  }
                />
              ) : null}
            />
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
      </SortableContext>
    </thead>
  )

  function renderRowCells(row: T) {
    return (
      <>
        {visibleColumns.map((col) => (
          <td
            key={col.id}
            className={cn(
              bodyCellVariants({
                align: col.align,
                numeric: col.numeric ?? false,
                density,
              }),
              col.defaultPinned === 'left' && 'sticky left-0 z-10 bg-card',
              col.defaultPinned === 'right' && 'sticky right-0 z-10 bg-card max-md:relative max-md:right-auto'
            )}
            {...(maskedColumnIds.includes(col.id) && { 'data-ph-mask': '' })}
            {...(col.defaultPinned && { 'data-pinned': col.defaultPinned })}
          >
            {col.render ? col.render(row) : col.key !== undefined ? String(row[col.key] ?? '') : null}
          </td>
        ))}
        {rowActions && (
          <td
            className={bodyCellVariants({ align: 'right', density })}
            onClick={(e) => e.stopPropagation()}
          >
            {rowActions(row)}
          </td>
        )}
      </>
    )
  }

  // Shared colgroup
  const colgroupEl = (
    <colgroup>
      {visibleColumns.map((col) => (
        <col
          key={col.id}
          ref={setColRef(col.id)}
          style={sizing[col.id] ? { width: `${sizing[col.id]}px` } : undefined}
        />
      ))}
      {rowActions && <col style={{ width: '96px' }} />}
    </colgroup>
  )

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={closestCenter}
      onDragEnd={(event) =>
        handleDragEnd({ active: { id: event.active.id }, over: event.over ? { id: event.over.id } : null })
      }
    >
      <div className="flex flex-col gap-2">
        {/* aria-live region for screen reader announcements during column drag */}
        <div aria-live="polite" aria-atomic="true" className="sr-only">
          {liveText}
        </div>
        {hasHideableColumns && onColumnVisibilityChange && (
          <div className="flex items-center justify-end">
            <ColumnVisibilityMenu
              columns={columns}
              visibility={columnVisibility}
              onChange={onColumnVisibilityChange}
              tableId={tableId}
            />
          </div>
        )}

        {virtualization?.enabled ? (
          // ── VIRTUALIZED PATH ──────────────────────────────────────────
          <div
            ref={scrollRef}
            className="overflow-auto"
            style={{ height: virtualization.scrollContainerHeight ?? '600px' }}
            data-testid="virtual-scroll-container"
          >
            <table
              className={cn(tableVariants({ loading: loading && !showSkeleton }), className)}
              aria-busy={loading ? true : undefined}
              style={{ width: '100%', borderCollapse: 'collapse' }}
            >
              {caption && (
                <caption className="caption-top text-left text-sm font-semibold text-foreground mb-2">
                  {caption}
                </caption>
              )}
              {colgroupEl}
              {theadEl}
              <tbody
                style={showSkeleton ? undefined : {
                  position: 'relative',
                  height: `${virtualizer.getTotalSize()}px`,
                }}
              >
                {showSkeleton ? (
                  <>
                    <tr>
                      <td colSpan={totalCols} className="sr-only">
                        <span role="status">Loading…</span>
                      </td>
                    </tr>
                    {Array.from({ length: skeletonRows! }).map((_, i) => (
                      <tr key={`skeleton-${i}`} aria-hidden="true">
                        {visibleColumns.map((col) => (
                          <td
                            key={col.id}
                            className={cn(
                              bodyCellVariants({
                                align: col.align,
                                numeric: col.numeric ?? false,
                                density,
                              }),
                              col.defaultPinned === 'left' && 'sticky left-0 z-10 bg-card',
                              col.defaultPinned === 'right' && 'sticky right-0 z-10 bg-card max-md:relative max-md:right-auto'
                            )}
                            {...(maskedColumnIds.includes(col.id) && { 'data-ph-mask': '' })}
                            {...(col.defaultPinned && { 'data-pinned': col.defaultPinned })}
                          >
                            <div className="h-4 w-3/4 rounded bg-skeleton motion-safe:animate-pulse motion-reduce:animate-none" />
                          </td>
                        ))}
                        {rowActions && (
                          <td className={bodyCellVariants({ align: 'right', density })}>
                            <div className="h-4 w-12 ml-auto rounded bg-skeleton motion-safe:animate-pulse motion-reduce:animate-none" />
                          </td>
                        )}
                      </tr>
                    ))}
                  </>
                ) : null}
                {!showSkeleton && virtualItems.map((virtualItem) => {
                  const tanStackRow = rows[virtualItem.index]
                  if (!tanStackRow) return null
                  const row = tanStackRow.original
                  return (
                    <tr
                      key={rowKey(row)}
                      ref={virtualization.dynamicRowHeight !== false ? virtualizer.measureElement : undefined}
                      data-index={virtualItem.index}
                      data-row-id={rowKey(row)}
                      className={cn(
                        'transition-colors',
                        onRowClick && 'cursor-pointer hover:bg-muted/50',
                        rowClassName?.(row)
                      )}
                      onClick={onRowClick ? () => onRowClick(row) : undefined}
                      style={{
                        position: 'absolute',
                        top: 0,
                        left: 0,
                        width: '100%',
                        transform: `translateY(${virtualItem.start}px)`,
                      }}
                    >
                      {renderRowCells(row)}
                    </tr>
                  )
                })}
                {/* Sentinel row — always rendered at bottom of virtual space */}
                {(virtualization.isFetchingMore || virtualization.hasMore) && (
                  <tr
                    style={{
                      position: 'absolute',
                      top: 0,
                      left: 0,
                      width: '100%',
                      transform: `translateY(${virtualizer.getTotalSize()}px)`,
                    }}
                  >
                    <td
                      data-testid="virtual-sentinel"
                      colSpan={totalCols}
                      style={{ textAlign: 'center', padding: '12px' }}
                    >
                      {virtualization.isFetchingMore ? 'Loading more transactions…' : null}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        ) : (
          // ── NON-VIRTUALIZED PATH ────────────────────────────────────
          <table
            className={cn(tableVariants({ loading: loading && !showSkeleton }), className)}
            aria-busy={loading ? true : undefined}
          >
            {caption && (
              <caption className="caption-top text-left text-sm font-semibold text-foreground mb-2">
                {caption}
              </caption>
            )}
            {colgroupEl}
            {theadEl}
            <tbody>
              {showSkeleton ? (
                <>
                  <tr>
                    <td colSpan={totalCols} className="sr-only">
                      <span role="status">Loading…</span>
                    </td>
                  </tr>
                  {Array.from({ length: skeletonRows! }).map((_, i) => (
                    <tr key={`skeleton-${i}`} aria-hidden="true">
                      {visibleColumns.map((col) => (
                        <td
                          key={col.id}
                          className={cn(
                            bodyCellVariants({
                              align: col.align,
                              numeric: col.numeric ?? false,
                              density,
                            }),
                            col.defaultPinned === 'left' && 'sticky left-0 z-10 bg-card',
                            col.defaultPinned === 'right' && 'sticky right-0 z-10 bg-card max-md:relative max-md:right-auto'
                          )}
                          {...(maskedColumnIds.includes(col.id) && { 'data-ph-mask': '' })}
                          {...(col.defaultPinned && { 'data-pinned': col.defaultPinned })}
                        >
                          <div className="h-4 w-3/4 rounded bg-skeleton motion-safe:animate-pulse motion-reduce:animate-none" />
                        </td>
                      ))}
                      {rowActions && (
                        <td className={bodyCellVariants({ align: 'right', density })}>
                          <div className="h-4 w-12 ml-auto rounded bg-skeleton motion-safe:animate-pulse motion-reduce:animate-none" />
                        </td>
                      )}
                    </tr>
                  ))}
                </>
              ) : rows.length === 0 ? (
                <tr>
                  <td colSpan={totalCols}>{emptyState}</td>
                </tr>
              ) : (
                rows.map((tanStackRow) => {
                  const row = tanStackRow.original
                  return (
                    <tr
                      key={rowKey(row)}
                      data-row-id={rowKey(row)}
                      className={cn(
                        'transition-colors',
                        onRowClick && 'cursor-pointer hover:bg-muted/50',
                        rowClassName?.(row)
                      )}
                      onClick={onRowClick ? () => onRowClick(row) : undefined}
                    >
                      {renderRowCells(row)}
                    </tr>
                  )
                })
              )}
            </tbody>
          </table>
        )}
      </div>
    </DndContext>
  )
}
