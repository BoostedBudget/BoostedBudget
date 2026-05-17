/**
 * Re-exports from @tanstack/react-table.
 *
 * This file is the single import point for TanStack Table within BoostedBudget.
 * Importing from here (rather than directly from the package) ensures:
 *   1. Rollup's manualChunks regex fires and emits a standalone `tanstack` chunk.
 *   2. The tanstack budget entry in .size-limit.json always has a file to measure.
 *   3. Phase 1 migration can widen these exports incrementally without refactoring consumers.
 *
 * See docs/decisions/2026-05-15-tanstack-table-adoption.md
 */
export {
  useReactTable,
  getCoreRowModel,
  getSortedRowModel,
  getFilteredRowModel,
  getPaginationRowModel,
  flexRender,
} from '@tanstack/react-table'

export type {
  ColumnDef as TanStackColumnDef,
  Table as TanStackTable,
  Row as TanStackRow,
  Cell as TanStackCell,
  Header as TanStackHeader,
  SortingState,
  ColumnFiltersState,
  PaginationState,
  VisibilityState,
  RowSelectionState,
} from '@tanstack/react-table'

export { useVirtualizer } from '@tanstack/react-virtual'
export type { Virtualizer, VirtualItem } from '@tanstack/react-virtual'
