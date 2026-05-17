# DataTable

Headless TanStack Table v8 facade for all tabular UIs in BoostedBudget. The component owns
`<td>` rendering directly — never via `flexRender` — so `data-ph-mask` GDPR masking is an
unconditional invariant under DataTable's control. Consumers declare columns with
`BBColumnDef<T>` (our narrowed type alias `ColumnDef<T>`), never TanStack's own `ColumnDef`.

## Column API (`ColumnDef<T>`)

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Unique column identifier (required) |
| `header` | `string \| ReactNode` | Header label. Must be `string` for sortable/filterable columns. |
| `key` | `keyof T` | Field to render when no `render` function is provided |
| `render` | `(row: T) => ReactNode` | Custom cell renderer; takes precedence over `key` |
| `align` | `'left' \| 'right' \| 'center'` | Cell/header text alignment (default: `'left'`) |
| `widthClass` | `string` | Tailwind width class applied to `<th>` (e.g. `'w-48'`) |
| `numeric` | `boolean` | Enables `tabular-nums` on cells |
| `sortable` | `boolean` | Enables sort button in header (`header` must be `string`) |
| `filter` | `SelectFilter \| RangeFilter` | See below |
| `resizable` | `boolean` | Opt-in resize handle (Phase 3a) |
| `defaultSize` | `number` | Default pixel width for resize reset (Home key) |
| `reorderable` | `boolean` | Opt-in drag-and-drop / keyboard reorder (Phase 3b) |
| `defaultPinned` | `'left' \| 'right'` | Sticky column pinning; right-pin degrades on mobile (Phase 3c) |
| `hideable` | `boolean` | Appears in ColumnVisibilityMenu when true (Phase 3d) |

**Filter shapes:**

```ts
{ type: 'select'; options: Array<{ label: string; value: string }>; multiSelect?: boolean; searchable?: boolean }
{ type: 'range'; unit?: 'number' | 'currency' | 'percent' }
```

## DataTableProps

| Prop | Type | Description |
|---|---|---|
| `data` | `T[]` | Row data (consumer owns fetching) |
| `columns` | `ColumnDef<T>[]` | Column definitions |
| `rowKey` | `(row: T) => string` | Unique key per row |
| `sort` | `SortState` | Controlled sort (`{ columnId, direction } \| null`) |
| `onSortChange` | `(s: SortState) => void` | Sort change callback |
| `filters` | `FilterState` | Controlled filters (`Record<string, string[] \| RangeValue \| null>`) |
| `onFiltersChange` | `(f: FilterState) => void` | Filter change callback |
| `loading` | `boolean` | Dims table and disables pointer events |
| `emptyState` | `ReactNode` | Rendered when `data` is empty |
| `onRowClick` | `(row: T) => void` | Row click handler; adds hover + cursor styles |
| `maskedColumnIds` | `string[]` | Column ids to mark with `data-ph-mask=""` |
| `rowActions` | `(row: T) => ReactNode` | Appends an Actions column (click does not bubble to row) |
| `columnSizing` | `Record<string, number>` | Controlled pixel widths by column id |
| `onColumnSizingChange` | `(s: Record<string, number>) => void` | Sizing change callback |
| `columnOrder` | `string[]` | Controlled column id order |
| `onColumnOrderChange` | `(o: string[]) => void` | Order change callback |
| `columnVisibility` | `Record<string, boolean>` | Controlled visibility map (only `hideable` columns participate) |
| `onColumnVisibilityChange` | `(v: Record<string, boolean>) => void` | Visibility change callback; required to render ColumnVisibilityMenu |
| `density` | `'compact' \| 'comfortable' \| 'spacious'` | Cell padding preset (default: `'comfortable'`) |

## Persistence (`useTablePreferences`)

```ts
function useTablePreferences(
  tableId: string,
  knownColumnIds: string[]
): UseTablePreferencesResult
```

Returns `{ sizing, onSizingChange, columnOrder, onColumnOrderChange, columnVisibility, onColumnVisibilityChange }` — drop these directly into `<DataTable>`.

**Storage key**: `bb:table-prefs:${userId}:${tableId}` — namespaced by Clerk user ID so
preferences are per-user even on shared devices.

**Version gate**: the stored blob includes `version: knownColumnIds.join(',')`. On read, if
the stored version doesn't match the current `knownColumnIds` (e.g. a column was added or
removed), preferences are reset to defaults and re-persisted. This prevents stale column
order arrays from referencing IDs that no longer exist.

**Null user**: when `useUser()` returns no authenticated user, all setters are no-ops and
defaults are returned. Safe to call unconditionally.

**Usage:**

```tsx
const tableId = 'transactions'
const columnIds = columns.map((c) => c.id)
const prefs = useTablePreferences(tableId, columnIds)

<DataTable
  data={rows}
  columns={columns}
  rowKey={(r) => r.id}
  sort={sort}
  onSortChange={setSort}
  filters={filters}
  onFiltersChange={setFilters}
  columnSizing={prefs.sizing}
  onColumnSizingChange={prefs.onSizingChange}
  columnOrder={prefs.columnOrder}
  onColumnOrderChange={prefs.onColumnOrderChange}
  columnVisibility={prefs.columnVisibility}
  onColumnVisibilityChange={prefs.onColumnVisibilityChange}
/>
```

## Server-state contract

`manualSorting: true`, `manualFiltering: true`, and `manualPagination: true` are hardcoded
in `useReactTable`. DataTable never transforms or paginates `data` — it renders exactly the
rows it receives. Sort/filter changes fire `onSortChange`/`onFiltersChange`, which consumers
use to update their React Query query params and re-fetch.

## Accessibility guarantees

- `aria-sort` is set on `<th>` (not on the sort button inside it), per ARIA spec.
- Resize handle has `role="separator"` with keyboard support (arrow keys + Home to reset).
- Column reorder fires `aria-live="polite"` announcements for screen readers.
- `ColumnVisibilityMenu` uses `role="menu"` + `role="menuitemcheckbox"`.
- Target: WCAG 2.1 AA.

## PII / PostHog masking

`data-ph-mask=""` is applied unconditionally by DataTable's own `<td>` renderer for every
column id present in `maskedColumnIds`. This happens at the JSX level (`{...(isMasked && { 'data-ph-mask': '' })}`), never delegated to `flexRender`. This is a non-negotiable invariant:
adding a masked column via `flexRender` bypasses the guarantee and must not be done.

## Quick example

```tsx
type Transaction = { id: string; date: string; payee: string; amount: number }

const columns: ColumnDef<Transaction>[] = [
  { id: 'date',   header: 'Date',    key: 'date',   sortable: true },
  { id: 'payee',  header: 'Payee',   key: 'payee',  sortable: true },
  { id: 'amount', header: 'Amount',  key: 'amount', numeric: true, align: 'right' },
]

<DataTable<Transaction>
  data={transactions}
  columns={columns}
  rowKey={(r) => r.id}
  sort={sort}
  onSortChange={setSort}
  filters={filters}
  onFiltersChange={setFilters}
  maskedColumnIds={['amount']}
/>
```
