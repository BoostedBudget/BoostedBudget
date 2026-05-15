import type { ColumnDef } from '../types'

/**
 * DEAD CODE in Phase 1 — preserved for Phase 3 (resize/reorder state).
 *
 * This function translates BoostedBudget's BBColumnDef array into TanStack ColumnDef array,
 * but it is not currently used: DataTable passes columns: [] to useReactTable because
 * rendering is owned by the facade loop below (never flexRender), and manualSorting: true
 * means TanStack doesn't call accessorFn or any column.cell callback.
 *
 * When Phase 3 adds resize/reorder state management, this function will be re-imported and
 * used to provide the column metadata TanStack needs for those features.
 *
 * Do not delete — mark for Phase 3 re-engagement.
 */
export function bbColumnsToTanStack<T>(
  _columns: ColumnDef<T>[]
): Array<Record<string, unknown>> {
  // Return empty array — rendering and sort metadata are owned by DataTable facade.
  // Phase 3: uncomment the original implementation below when resize/reorder state is added.
  return []

  /*
  return columns.map((col): TanStackColumnDef<T> => {
    const accessorFn: (row: T) => unknown = col.sortAccessor
      ? col.sortAccessor
      : col.key !== undefined
        ? (row: T) => (row as Record<string, unknown>)[col.key as string]
        : () => null

    return {
      id: col.id,
      accessorFn,
      header: typeof col.header === 'string' ? col.header : col.id,
      enableSorting: col.sortable ?? false,
    } as TanStackColumnDef<T>
  })
  */
}
