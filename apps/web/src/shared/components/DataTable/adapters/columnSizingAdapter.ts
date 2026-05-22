import type { TanStackColumnDef } from '@/shared/lib/tanstack'
import type { ColumnDef } from '../types'

/**
 * Converts BoostedBudget ColumnDef[] to a minimal TanStack ColumnDef[] that carries
 * only the column-sizing metadata (id, size, minSize, maxSize, enableResizing).
 *
 * DataTable still renders its own <td> markup — this adapter exists solely so TanStack's
 * column-sizing state machine (state.columnSizing, header.getSize(), header.getResizeHandler())
 * knows about each column's sizing constraints. flexRender is never called on these defs.
 */
export function toTanStackSizingColumns<T>(columns: ColumnDef<T>[]): TanStackColumnDef<T>[] {
  return columns.map((col) => ({
    id: col.id,
    // TanStack uses `size` as the default/initial width; maps from our `defaultSize`.
    ...(col.defaultSize !== undefined && { size: col.defaultSize }),
    ...(col.minSize !== undefined && { minSize: col.minSize }),
    ...(col.maxSize !== undefined && { maxSize: col.maxSize }),
    // Only columns explicitly marked resizable get the resize handler.
    enableResizing: col.resizable ?? false,
    // accessorFn must be provided so TanStack doesn't warn about missing accessor.
    accessorFn: () => null,
    header: typeof col.header === 'string' ? col.header : col.id,
  })) as TanStackColumnDef<T>[]
}
