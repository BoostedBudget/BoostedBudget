import { useCallback, useMemo } from 'react'
import { Edit, Tag, Clock } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { ReadOnlyGuard } from '@/auth'
import type { Transaction } from '../types'
import type { Account } from '../../accounts/types'
import { getCategoryColor } from '@/lib/category-colors'
import { getEffectiveCategory, getEffectiveCategoryLabel, getEffectiveDetailedCategory } from '@/shared/categories/getEffectiveCategory'
import { getDetailedLabel, getPfcDetailedFilterOptions } from '@/shared/categories/pfcOptions'
import { HierarchicalCategoryTreeSelect } from '@/shared/components/HierarchicalCategoryTreeSelect'
import type { CustomCategoryOption } from '@/shared/components/HierarchicalCategoryTreeSelect/HierarchicalCategoryTreeSelect'
import { CategoryIcon } from '@/shared/components/IconPicker'
import { formatTransactionAmount, isOutflow } from '@/shared/transactions/amount'
import { EmptyState } from '@/components/ui/empty-state'
import { DataTable, useTablePreferences } from '@/shared/components/DataTable'
import type { ColumnDef, FilterState, SortState } from '@/shared/components/DataTable'
import type { FilterOption, RangeValue } from '@/shared/components/ColumnFilterDropdown'

const TRANSACTION_COLUMN_IDS = ['date', 'merchant', 'account', 'category', 'sub_category', 'amount'] as const

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
  transactions: Transaction[]
  accountsMap: Record<string, Account>
  onEdit?: (transaction: Transaction) => void
  onCategorize?: (transaction: Transaction) => void
  onRowClick?: (transaction: Transaction) => void
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

function formatDate(dateStr: string | null | undefined): string {
  if (!dateStr || typeof dateStr !== 'string') return 'Invalid Date'
  const datePortion = dateStr.split('T')[0]
  if (!/^\d{4}-\d{2}-\d{2}$/.test(datePortion)) return 'Invalid Date'
  const [year, month, day] = datePortion.split('-').map(Number)
  if (isNaN(year) || isNaN(month) || isNaN(day)) return 'Invalid Date'
  const transactionDate = new Date(year, month - 1, day)
  if (isNaN(transactionDate.getTime())) return 'Invalid Date'
  return new Intl.DateTimeFormat('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
  }).format(transactionDate)
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

  const columns: ColumnDef<Transaction>[] = [
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
      render: (row) => (
        <div>
          <div className="font-medium text-foreground">
            {row.merchant_name || row.name}
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
      ),
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
        const eff = getEffectiveCategory(row)
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
                style={{ backgroundColor: customMatch.color ?? '#6B7280', color: '#fff' }}
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
        const detailed = getEffectiveDetailedCategory(row)
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
      render: (row) => (
        <div className="flex flex-col items-end gap-1">
          <span
            className={cn(
              'font-semibold tabular-nums',
              isOutflow(row.amount)
                ? 'transaction-table__amount--debit text-destructive'
                : row.amount < 0
                  ? 'transaction-table__amount--credit text-success'
                  : 'text-foreground'
            )}
          >
            {formatTransactionAmount(row.amount).text}
          </span>
          {row.pending && (
            <Badge variant="warning" size="sm">
              <Clock className="h-2.5 w-2.5" aria-hidden="true" />
              Pending
            </Badge>
          )}
        </div>
      ),
      filter: onAmountRangeChange ? { type: 'range', unit: 'currency' } : undefined,
    },
  ]

  return (
    <DataTable<Transaction>
      data={transactions}
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
          row.pending &&
            'bg-warning/10 hover:bg-warning/20'
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
          ? (row) => (
              <div className="flex items-center justify-end gap-2">
                {onEdit && (
                  <ReadOnlyGuard message="Resubscribe to edit transactions">
                    <button
                      type="button"
                      onClick={() => onEdit(row)}
                      disabled={loading}
                      className="inline-flex h-8 w-8 cursor-pointer items-center justify-center rounded-md border-none bg-transparent p-0 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50 [&_svg]:h-4 [&_svg]:w-4"
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
                      onClick={() => onCategorize(row)}
                      disabled={loading}
                      className="inline-flex h-8 w-8 cursor-pointer items-center justify-center rounded-md border-none bg-transparent p-0 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50 [&_svg]:h-4 [&_svg]:w-4"
                      aria-label="Categorize transaction"
                      title="Categorize transaction"
                    >
                      <Tag aria-hidden="true" />
                    </button>
                  </ReadOnlyGuard>
                )}
              </div>
            )
          : undefined
      }
    />
  )
}
