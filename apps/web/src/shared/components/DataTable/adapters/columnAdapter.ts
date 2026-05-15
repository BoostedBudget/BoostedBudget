import type { TanStackColumnDef } from '@/shared/lib/tanstack'
import type { ColumnDef } from '../types'

/**
 * Translate BoostedBudget's BBColumnDef array into TanStack ColumnDef array
 * for use with useReactTable.
 *
 * NOTE: The `render` and `key` fields are intentionally NOT mapped to TanStack's
 * `cell` accessor — cell rendering stays entirely in the DataTable facade so that
 * data-ph-mask (GDPR/PII) attributes remain under our complete control.
 *
 * TanStack only receives the minimal metadata it needs: id, header label,
 * enableSorting flag, and an accessorFn so its row model can build rows from data.
 */
export function bbColumnsToTanStack<T>(
  columns: ColumnDef<T>[]
): TanStackColumnDef<T>[] {
  return columns.map((col): TanStackColumnDef<T> => {
    // Use accessorFn variant so TanStack can build a row model.
    // The cell rendering function itself is NOT set here — we own <td> markup entirely.
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
}
