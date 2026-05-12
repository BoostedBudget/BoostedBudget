import { useCallback } from 'react'
import { Edit, Tag, Clock } from 'lucide-react'
import { cn } from '@/lib/utils'
import { ReadOnlyGuard } from '@/auth'
import type { Transaction } from '../types'
import type { Account } from '../../accounts/types'
import { getCategoryColor } from '@/lib/category-colors'
import { getEffectiveCategory, getEffectiveCategoryLabel } from '@/shared/categories/getEffectiveCategory'
import { formatTransactionAmount, isOutflow } from '@/shared/transactions/amount'
import { DataTable } from '@/shared/components/DataTable'
import type { ColumnDef, FilterState, SortState } from '@/shared/components/DataTable'
import type { FilterOption, RangeValue } from '@/shared/components/ColumnFilterDropdown'

export type TransactionSortColumn = 'date' | 'merchant' | 'amount' | 'category'
export type SortDirection = 'asc' | 'desc'

export interface TransactionTableProps {
  transactions: Transaction[]
  accountsMap: Record<string, Account>
  onEdit?: (transaction: Transaction) => void
  onCategorize?: (transaction: Transaction) => void
  onRowClick?: (transaction: Transaction) => void
  loading?: boolean
  sortable?: boolean
  sortColumn?: TransactionSortColumn
  sortDirection?: SortDirection
  onSort?: (column: TransactionSortColumn, direction: SortDirection) => void
  className?: string
  accountOptions?: FilterOption[]
  accountFilter?: string[]
  onAccountFilterChange?: (value: string[]) => void
  categoryOptions?: FilterOption[]
  categoryFilter?: string[]
  onCategoryFilterChange?: (value: string[]) => void
  amountRange?: RangeValue | null
  onAmountRangeChange?: (value: RangeValue | null) => void
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
  sortable = false,
  sortColumn,
  sortDirection = 'asc',
  onSort,
  className = '',
  accountOptions,
  accountFilter,
  onAccountFilterChange,
  categoryOptions,
  categoryFilter,
  onCategoryFilterChange,
  amountRange,
  onAmountRangeChange,
}: TransactionTableProps) {
  const hasActions = !!(onEdit || onCategorize)

  const handleSortChange = useCallback(
    (newSort: SortState) => {
      if (!onSort || !sortable) return
      if (newSort) {
        onSort(newSort.columnId as TransactionSortColumn, newSort.direction)
      } else if (sortColumn) {
        onSort(sortColumn, sortDirection === 'asc' ? 'desc' : 'asc')
      }
    },
    [onSort, sortable, sortColumn, sortDirection]
  )

  const sort: SortState =
    sortColumn && sortable ? { columnId: sortColumn, direction: sortDirection } : null

  const filters: FilterState = {
    account: accountFilter ?? [],
    category: categoryFilter ?? [],
    amount: amountRange ?? null,
  }

  const handleFiltersChange = useCallback(
    (newFilters: FilterState) => {
      if (newFilters.account !== filters.account) {
        onAccountFilterChange?.(newFilters.account as string[])
      }
      if (newFilters.category !== filters.category) {
        onCategoryFilterChange?.(newFilters.category as string[])
      }
      if (newFilters.amount !== filters.amount) {
        onAmountRangeChange?.(newFilters.amount as RangeValue | null)
      }
    },
    [filters.account, filters.category, filters.amount, onAccountFilterChange, onCategoryFilterChange, onAmountRangeChange]
  )

  const columns: ColumnDef<Transaction>[] = [
    {
      id: 'date',
      header: 'Date',
      sortable,
      widthClass: 'whitespace-nowrap',
      render: (row) => (
        <span className="text-muted-foreground">
          {formatDate(row.authorized_date || row.date)}
        </span>
      ),
    },
    {
      id: 'merchant',
      header: 'Description',
      sortable,
      widthClass: 'min-w-[200px] max-md:min-w-[150px]',
      render: (row) => (
        <div>
          <div className="font-medium text-foreground">
            {row.merchant_name || row.name}
          </div>
          {row.tags && row.tags.length > 0 && (
            <div className="mt-1 flex flex-wrap gap-1">
              {row.tags.map((tag) => (
                <span
                  key={tag}
                  className="inline-flex items-center rounded bg-blue-50 px-1.5 py-0.5 text-[0.625rem] font-medium text-blue-700 dark:bg-blue-900 dark:text-blue-300"
                >
                  {tag}
                </span>
              ))}
            </div>
          )}
        </div>
      ),
    },
    {
      id: 'account',
      header: 'Account',
      render: (row) => {
        const account = accountsMap[row.account_id]
        const accountDisplayName = account ? account.nickname || account.name : 'Unknown'
        const accountBackgroundColor = account?.custom_color || '#E5E7EB'
        return (
          <span
            className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium text-gray-800"
            style={{ backgroundColor: accountBackgroundColor }}
          >
            {account?.custom_emoji && <span>{account.custom_emoji}</span>}
            {accountDisplayName}
          </span>
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
      sortable,
      render: (row) => {
        const effective = getEffectiveCategory(row)
        const categoryLabel = getEffectiveCategoryLabel(effective)
        const categoryColor = getCategoryColor(categoryLabel)
        return (
          <span className="inline-flex items-center gap-1.5">
            <span
              className="h-2 w-2 shrink-0 rounded-full"
              style={{ backgroundColor: categoryColor }}
            />
            {categoryLabel}
          </span>
        )
      },
      filter:
        categoryOptions && onCategoryFilterChange
          ? { type: 'select', options: categoryOptions, multiSelect: true, searchable: true }
          : undefined,
    },
    {
      id: 'amount',
      header: 'Amount',
      sortable,
      align: 'right',
      numeric: true,
      render: (row) => {
        const { text: amountFormatted } = formatTransactionAmount(row.amount)
        return (
          <div className="flex flex-col items-end gap-1">
            <span
              className={cn(
                'font-semibold tabular-nums',
                isOutflow(row.amount)
                  ? 'transaction-table__amount--debit text-red-600 dark:text-red-400'
                  : row.amount < 0
                    ? 'transaction-table__amount--credit text-green-600 dark:text-green-400'
                    : 'text-foreground'
              )}
            >
              {amountFormatted}
            </span>
            {row.pending && (
              <span className="inline-flex items-center gap-1 rounded bg-yellow-100 px-1.5 py-0.5 text-[0.625rem] font-medium text-yellow-800 dark:bg-yellow-800 dark:text-yellow-200">
                <Clock className="h-2.5 w-2.5" aria-hidden="true" />
                Pending
              </span>
            )}
          </div>
        )
      },
      filter: onAmountRangeChange ? { type: 'range', unit: 'currency' } : undefined,
    },
  ]

  return (
    <DataTable<Transaction>
      data={transactions}
      columns={columns}
      rowKey={(row) => row.id}
      sort={sort}
      onSortChange={handleSortChange}
      filters={filters}
      onFiltersChange={handleFiltersChange}
      loading={loading}
      onRowClick={onRowClick}
      rowClassName={(row) =>
        cn(
          onRowClick && 'transaction-table__row--clickable',
          row.pending &&
            'bg-yellow-50 hover:bg-yellow-100 dark:bg-yellow-900 dark:hover:bg-yellow-800'
        )
      }
      className={cn(loading && 'transaction-table--loading', className)}
      maskedColumnIds={['amount']}
      emptyState={
        <p className="py-12 px-6 text-center text-muted-foreground">
          No transactions found.
        </p>
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
