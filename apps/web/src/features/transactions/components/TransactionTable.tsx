import { useCallback, useMemo } from 'react'
import { Edit, Tag, Clock, Undo2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { ReadOnlyGuard } from '@/auth'
import type { Transaction, FeedRow } from '../types'
import { isInvestmentRow, isSettledPending } from '../types'
import { investmentSubcategoryLabel } from '../investmentSubcategory'
import { ActivityTypeChip } from '@/shared/components/ActivityTypeChip'
import type { Account } from '../../accounts/types'
import { getCategoryColor, DEFAULT_CATEGORY_COLOR } from '@/lib/category-colors'
import { getEffectiveCategory, getEffectiveCategoryLabel, getEffectiveDetailedCategory } from '@/shared/categories/getEffectiveCategory'
import { getDetailedLabel, getPfcDetailedFilterOptions } from '@/shared/categories/pfcOptions'
import { HierarchicalCategoryTreeSelect } from '@/shared/components/HierarchicalCategoryTreeSelect'
import type { CustomCategoryOption } from '@/shared/components/HierarchicalCategoryTreeSelect/HierarchicalCategoryTreeSelect'
import { CategoryIcon } from '@/shared/components/IconPicker'
import { formatTransactionAmount, isOutflow } from '@/shared/transactions/amount'
import { formatDate } from '@/shared/transactions/dates'
import { getMerchantDisplayName } from '@/shared/transactions/merchant'
import { EmptyState } from '@/components/ui/empty-state'
import { DataTable, useTablePreferences } from '@/shared/components/DataTable'
import type { ColumnDef, FilterState, SortState } from '@/shared/components/DataTable'
import type { FilterOption, RangeValue } from '@/shared/components/ColumnFilterDropdown'

const TRANSACTION_COLUMN_IDS = ['date', 'merchant', 'account', 'category', 'sub_category', 'amount'] as const
const ACTION_BUTTON_CLASS = 'inline-flex h-8 w-8 cursor-pointer items-center justify-center rounded-md border-none bg-transparent p-0 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50 [&_svg]:h-4 [&_svg]:w-4'

export type TransactionSortColumn = 'date' | 'merchant' | 'amount' | 'category'
export type SortDirection = 'asc' | 'desc'

// eslint-disable-next-line react-refresh/only-export-components
export const TRANSACTION_SORTABLE_COLUMNS = new Set([
  'date',
  'merchant',
  'amount',
  'category',
] as const)

export interface TransactionTableProps {
  transactions: FeedRow[]
  accountsMap: Record<string, Account>
  onEdit?: (transaction: Transaction) => void
  onCategorize?: (transaction: Transaction) => void
  onRowClick?: (row: FeedRow) => void
  loading?: boolean
  sort?: SortState
  onSortChange?: (sort: SortState) => void
  className?: string
  accountOptions?: FilterOption[]
  accountFilter?: string[]
  onAccountFilterChange?: (value: string[]) => void
  amountRange?: RangeValue | null
  onAmountRangeChange?: (value: RangeValue | null) => void
  /** Selected PFC primary enum values (hierarchical category filter) */
  selectedPrimaries?: string[]
  onPrimariesChange?: (values: string[]) => void
  /** Selected PFC detailed sub-category enum values (hierarchical category filter) */
  selectedDetaileds?: string[]
  onDetailedsChange?: (values: string[]) => void
  /** Custom categories + selection for the hierarchical category filter (#1203) */
  customCategories?: CustomCategoryOption[]
  selectedCustomIds?: string[]
  onCustomIdsChange?: (ids: string[]) => void
  /** Infinite scroll: called when user scrolls near the bottom */
  onScrollEnd?: () => void
  /** Infinite scroll: whether more pages exist */
  hasMore?: boolean
  /** Infinite scroll: whether a fetch is in progress */
  isFetchingMore?: boolean
}

/**
 * Issue #1569 T12. A row the PROVIDER withdrew after it had already posted.
 *
 * `reversed` is only ever present when the request opted in (include_reversed); every read
 * excludes tombstoned rows otherwise. A transaction removed while still PENDING is a different
 * class and is never returned at all, by any value of that flag — its posted twin is already in
 * the feed, so showing both would double-count the same money. The backend enforces that twice
 * (at the query and in Transaction.Reversed()); this predicate deliberately does not try to
 * re-derive it, because a client-side guess about which tombstone class a row belongs to would
 * be a second, weaker copy of a rule that already has two.
 *
 * Investment rows never carry the flag.
 */
function isReversedRow(row: FeedRow): boolean {
  // `!row.pending` is belt-and-braces against a settled pending (see isSettledPending). Such a
  // row is dropped before it reaches here, but the predicate must be safe on its own if reused.
  return !isInvestmentRow(row) && row.reversed === true && !row.pending
}

export function TransactionTable({
  transactions,
  accountsMap,
  onEdit,
  onCategorize,
  onRowClick,
  loading = false,
  sort = null,
  onSortChange,
  className = '',
  accountOptions,
  accountFilter,
  onAccountFilterChange,
  amountRange,
  onAmountRangeChange,
  selectedPrimaries,
  onPrimariesChange,
  selectedDetaileds,
  onDetailedsChange,
  customCategories,
  selectedCustomIds,
  onCustomIdsChange,
  onScrollEnd,
  hasMore,
  isFetchingMore,
}: TransactionTableProps) {
  const hasActions = !!(onEdit || onCategorize)

  const {
    sizing,
    onSizingChange,
    columnOrder,
    onColumnOrderChange,
    columnVisibility,
    onColumnVisibilityChange,
  } = useTablePreferences('transactions', [...TRANSACTION_COLUMN_IDS])

  // #1569 T12 fix round 1 (I2). Last line of defence: a settled pending is dropped before it can
  // be rendered. It is already dropped upstream in useInfiniteTransactions, and twice more in the
  // backend — this guard is here because THIS component is what turns a row into a visible
  // amount, and the cost of the check is one predicate against the cost of double-counting money.
  const visibleRows = useMemo(
    () => transactions.filter((row) => !isSettledPending(row)),
    [transactions],
  )

  const pfcDetailedOptions = useMemo(() => getPfcDetailedFilterOptions(), [])

  const filters: FilterState = {
    account: accountFilter ?? [],
    amount: amountRange ?? null,
    sub_category: selectedDetaileds ?? [],
  }

  const handleFiltersChange = useCallback(
    (newFilters: FilterState) => {
      if (newFilters.account !== filters.account) onAccountFilterChange?.(newFilters.account as string[])
      if (newFilters.amount !== filters.amount) onAmountRangeChange?.(newFilters.amount as RangeValue | null)
      if (newFilters.sub_category !== filters.sub_category) onDetailedsChange?.(newFilters.sub_category as string[])
    },
    [filters.account, filters.amount, filters.sub_category, onAccountFilterChange, onAmountRangeChange, onDetailedsChange]
  )

  const columns: ColumnDef<FeedRow>[] = [
    {
      id: 'date',
      header: 'Date',
      sortable: !!onSortChange,
      resizable: true,
      reorderable: true,
      defaultSize: 110,
      minSize: 90,
      maxSize: 160,
      render: (row) => (
        <span className="whitespace-nowrap text-muted-foreground">
          {formatDate(row.authorized_date || row.date)}
        </span>
      ),
    },
    {
      id: 'merchant',
      header: 'Description',
      sortable: !!onSortChange,
      resizable: true,
      reorderable: true,
      defaultSize: 240,
      minSize: 160,
      render: (row) => {
        if (isInvestmentRow(row)) {
          // `??` alone misses production rows where ticker_symbol/security_name are '' rather
          // than null/undefined (#1631) — a blank cell used to render as the literal " · ".
          // row.name (the transaction's own name) is the fallback for exactly those rows: it is
          // the only field with content when the security metadata is blank.
          const ticker = row.ticker_symbol && row.ticker_symbol.length > 0 ? row.ticker_symbol : null
          const securityName =
            row.security_name && row.security_name.length > 0 ? row.security_name : null
          if (!ticker && !securityName) {
            return (
              <div className="font-medium text-foreground">{row.name || '—'}</div>
            )
          }
          return (
            <div className="font-medium text-foreground">
              {ticker ?? '—'} · {securityName ?? '—'}
            </div>
          )
        }
        return (
          <div>
            <div className="font-medium text-foreground">
              {getMerchantDisplayName(row)}
            </div>
            {row.tags && row.tags.length > 0 && (
              <div className="mt-1 flex flex-wrap gap-1">
                {row.tags.map((tag) => (
                  <Badge key={`${row.id}-${tag}`} variant="info" size="sm">
                    {tag}
                  </Badge>
                ))}
              </div>
            )}
          </div>
        )
      },
    },
    {
      id: 'account',
      header: 'Account',
      resizable: true,
      reorderable: true,
      hideable: true,
      defaultSize: 160,
      minSize: 120,
      maxSize: 280,
      render: (row) => {
        const account = accountsMap[row.account_id]
        const accentColor = account?.custom_color
        return (
          <Badge variant="outline" className="gap-1.5">
            {accentColor && (
              <span
                className="h-1.5 w-1.5 shrink-0 rounded-full"
                style={{ backgroundColor: accentColor }}
                aria-hidden="true"
              />
            )}
            {account?.custom_emoji && <span>{account.custom_emoji}</span>}
            {account ? account.nickname || account.name : 'Unknown'}
          </Badge>
        )
      },
      filter:
        accountOptions && onAccountFilterChange
          ? { type: 'select', options: accountOptions, multiSelect: true, searchable: true }
          : undefined,
    },
    {
      id: 'category',
      header: 'Category',
      sortable: !!onSortChange,
      resizable: true,
      reorderable: true,
      hideable: true,
      defaultSize: 160,
      minSize: 120,
      maxSize: 280,
      render: (row) => {
        if (isInvestmentRow(row)) {
          return <ActivityTypeChip activityType={row.activity_type} />
        }
        const txn = row as unknown as Transaction
        const eff = getEffectiveCategory(txn)
        const categoryLabel = getEffectiveCategoryLabel(eff)
        // For a custom category, show its icon + color (Issue #1203) when we can
        // resolve it from the loaded custom categories; otherwise fall back to the
        // generic color dot.
        const customMatch =
          eff?.kind === 'custom'
            ? customCategories?.find((c) => c.id === eff.custom_category_id)
            : undefined
        if (customMatch) {
          return (
            <span className="inline-flex items-center gap-1.5">
              <span
                className="inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-full"
                style={{ backgroundColor: customMatch.color ?? DEFAULT_CATEGORY_COLOR, color: '#fff' }}
                aria-hidden
              >
                <CategoryIcon name={customMatch.icon} className="h-2.5 w-2.5" />
              </span>
              {categoryLabel}
            </span>
          )
        }
        return (
          <span className="inline-flex items-center gap-1.5">
            <span
              className="h-2 w-2 shrink-0 rounded-full"
              style={{ backgroundColor: getCategoryColor(categoryLabel) }}
            />
            {categoryLabel}
          </span>
        )
      },
      customFilter: onPrimariesChange ? (
        <HierarchicalCategoryTreeSelect
          selectedPrimaries={selectedPrimaries ?? []}
          onPrimariesChange={onPrimariesChange}
          selectedDetaileds={selectedDetaileds ?? []}
          onDetailedsChange={onDetailedsChange}
          customCategories={customCategories}
          selectedCustomIds={selectedCustomIds ?? []}
          onCustomIdsChange={onCustomIdsChange}
          aria-label="Filter by category"
        />
      ) : undefined,
      isFilterActive:
        (selectedPrimaries?.length ?? 0) +
          (selectedDetaileds?.length ?? 0) +
          (selectedCustomIds?.length ?? 0) >
        0,
    },
    {
      id: 'sub_category',
      header: 'Sub-category',
      resizable: true,
      reorderable: true,
      hideable: true,
      defaultSize: 140,
      minSize: 100,
      maxSize: 260,
      render: (row) => {
        // Investment branch FIRST, so the `as unknown as Transaction` cast below stops applying to
        // investment rows at all (#1631). Before this, the cast reached for PFC fields the feed's
        // UNION hard-codes NULL, so every investment row rendered "—" while `investment_subtype`
        // sat unused on the wire.
        if (isInvestmentRow(row)) {
          const label = investmentSubcategoryLabel(row.activity_type, row.investment_subtype)
          if (!label) return <span className="text-sm text-muted-foreground/40">—</span>
          return <span className="text-sm text-muted-foreground">{label}</span>
        }
        const txn = row as unknown as Transaction
        const detailed = getEffectiveDetailedCategory(txn)
        if (!detailed) return <span className="text-sm text-muted-foreground/40">—</span>
        const label = getDetailedLabel(detailed) ?? detailed
        return <span className="text-sm text-muted-foreground">{label}</span>
      },
      filter: onDetailedsChange
        ? { type: 'select', options: pfcDetailedOptions, multiSelect: true, searchable: true }
        : undefined,
    },
    {
      id: 'amount',
      header: 'Amount',
      sortable: !!onSortChange,
      resizable: true,
      reorderable: true,
      defaultPinned: 'right',
      align: 'right',
      numeric: true,
      defaultSize: 140,
      minSize: 110,
      maxSize: 200,
      render: (row) => {
        const isInvestment = isInvestmentRow(row)
        const reversed = isReversedRow(row)
        return (
          // data-reversed is STRUCTURAL on purpose. Session replay records attribute values
          // verbatim and no mask can cover them, so this marker carries a boolean and nothing
          // else — never the amount, the merchant, or the reversal date.
          <div className="flex flex-col items-end gap-1" data-reversed={reversed ? 'true' : undefined}>
            <span
              className={cn(
                'font-semibold tabular-nums',
                // The strike-through is ADDITIONAL to the badge below, never instead of it:
                // information is not carried by styling alone.
                //
                // text-foreground, NOT text-muted-foreground: muted is 4.73:1 on light card with
                // no margin to spare, and the most important number on a reversed row must not be
                // the least readable thing on it. At full foreground it is 19.79:1 light /
                // 17.16:1 dark — the same as any other amount.
                reversed && 'line-through text-foreground',
                !reversed && (isOutflow(row.amount)
                  ? 'transaction-table__amount--debit text-destructive'
                  : row.amount < 0
                    ? 'transaction-table__amount--credit text-success'
                    : 'text-foreground')
              )}
            >
              {formatTransactionAmount(row.amount).text}
            </span>
            {reversed ? (
              <Badge variant="outline" size="sm">
                <Undo2 className="h-2.5 w-2.5" aria-hidden="true" />
                Reversed
              </Badge>
            ) : isInvestment ? (
              <Badge variant="info" size="sm">Investment</Badge>
            ) : row.pending ? (
              <Badge variant="warning" size="sm">
                <Clock className="h-2.5 w-2.5" aria-hidden="true" />
                Pending
              </Badge>
            ) : null}
          </div>
        )
      },
      filter: onAmountRangeChange ? { type: 'range', unit: 'currency' } : undefined,
    },
  ]

  return (
    <DataTable<FeedRow>
      data={visibleRows}
      columns={columns}
      rowKey={(row) => row.id}
      sort={sort}
      onSortChange={onSortChange!}
      filters={filters}
      onFiltersChange={handleFiltersChange}
      loading={loading}
      skeletonRows={10}
      onRowClick={onRowClick}
      columnSizing={sizing}
      onColumnSizingChange={onSizingChange}
      columnOrder={columnOrder}
      onColumnOrderChange={onColumnOrderChange}
      columnVisibility={columnVisibility}
      onColumnVisibilityChange={onColumnVisibilityChange}
      rowClassName={(row) =>
        cn(
          onRowClick && 'transaction-table__row--clickable',
          // A reversed row carries NO row-level treatment, and that is a contrast decision.
          // The first version used `opacity-70`. Rows have no background of their own (DataTable
          // is bg-card), so the opacity composited straight onto the ground and took every cell
          // with it: the struck amount fell to 2.71:1 in light and 4.02:1 on dark card, against a
          // 4.5:1 floor — 14px BOLD is not WCAG "large text" — and the row's icon buttons fell
          // below 1.4.11's 3:1. Tuning the value was rejected in favour of removing it: with no
          // row treatment at all, a reversed row composites on exactly the same ground as every
          // other row, so nothing here can regress a ratio. "Undone" is carried by the
          // strike-through and the Reversed badge, which is where it belongs.
          // #1569 task 20 (A3). This was `bg-warning/10 hover:bg-warning/20`, and it failed WCAG
          // AA in three of its four states. A row has no background of its own — DataTable paints
          // bg-card — so the tint composited onto the card and EVERY cell was read against the
          // result: text-muted-foreground (the date column, the category label) fell to 4.38:1
          // light, 4.05:1 on light hover and 4.44:1 on dark hover, against a 4.5:1 floor. 14px
          // regular is not WCAG "large text".
          //
          // Tuning the alpha was rejected, for the reason the reversed-row note above gives and
          // task 12 gave before it: `/5` buys 4.55:1, and 0.05 against a rounding boundary is not
          // a margin. The marker is a left rule on the first cell instead — it paints INSIDE one
          // cell's own box rather than under the row's text, so no ratio anywhere on the row moves
          // and the hover state is DataTable's ordinary one. "Pending" is still carried by the
          // badge and its clock icon in the amount column, which is where the information belongs.
          row.pending && '[&>td:first-child]:border-l-4 [&>td:first-child]:border-l-warning'
        )
      }
      className={cn(loading && 'transaction-table--loading', className)}
      maskedColumnIds={['amount', 'merchant', 'account']}
      virtualization={onScrollEnd ? {
        enabled: true,
        estimateRowHeight: 56,
        overscan: 8,
        dynamicRowHeight: true,
        scrollContainerHeight: 'calc(100dvh - 240px)',
        onScrollEnd,
        hasMore,
        isFetchingMore,
        prefetchRows: 5,
      } : undefined}
      emptyState={
        <EmptyState
          icon={<Tag />}
          title="No transactions found"
          description="Transactions will appear here once your accounts sync."
        />
      }
      rowActions={
        hasActions
          ? (row) => {
              if (isInvestmentRow(row)) return null
              return (
                <div className="flex items-center justify-end gap-2">
                  {onEdit && (
                    <ReadOnlyGuard message="Resubscribe to edit transactions">
                      <button
                        type="button"
                        onClick={() => onEdit(row as unknown as Transaction)}
                        disabled={loading}
                        className={ACTION_BUTTON_CLASS}
                        aria-label="Edit transaction"
                        title="Edit transaction"
                      >
                        <Edit aria-hidden="true" />
                      </button>
                    </ReadOnlyGuard>
                  )}
                  {onCategorize && (
                    <ReadOnlyGuard message="Resubscribe to categorize transactions">
                      <button
                        type="button"
                        onClick={() => onCategorize(row as unknown as Transaction)}
                        disabled={loading}
                        className={ACTION_BUTTON_CLASS}
                        aria-label="Categorize transaction"
                        title="Categorize transaction"
                      >
                        <Tag aria-hidden="true" />
                      </button>
                    </ReadOnlyGuard>
                  )}
                </div>
              )
            }
          : undefined
      }
    />
  )
}
