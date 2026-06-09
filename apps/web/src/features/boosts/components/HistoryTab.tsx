import type { TransferSuggestion } from '@boostedbudget/api-client/models'
import { DataTable, TablePagination, useTablePreferences } from '@/shared/components/DataTable'
import type { ColumnDef } from '@/shared/components/DataTable'
import { SuggestionStatusBadge } from './SuggestionStatusBadge'

const COLUMN_IDS = ['date', 'amount', 'status', 'reason']

const formatCurrency = (amount: number, currency: string) =>
  new Intl.NumberFormat('en-US', { style: 'currency', currency, maximumFractionDigits: 0 }).format(amount)

const formatDate = (iso: string) =>
  new Date(iso).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })

export interface HistoryTabProps {
  items: TransferSuggestion[]
  /** Total count from the server (for pagination) */
  total: number
  limit: number
  offset: number
  isLoading: boolean
  /** Called with the new offset value */
  onPageChange: (offset: number) => void
}

export function HistoryTab({
  items,
  total,
  limit,
  offset,
  isLoading,
  onPageChange,
}: HistoryTabProps) {
  const { sizing, onSizingChange } = useTablePreferences('boosts-history', COLUMN_IDS)

  const currentPage = Math.floor(offset / limit) + 1
  const totalPages = Math.ceil(total / limit)
  const showPagination = total > limit

  if (isLoading) {
    return (
      <div className="animate-pulse space-y-3 p-4">
        {[0, 1, 2].map((i) => <div key={i} className="h-12 rounded bg-muted" />)}
      </div>
    )
  }

  if (items.length === 0) {
    return (
      <div className="py-16 text-center text-muted-foreground">
        <p className="text-base font-medium">No completed or past suggestions yet</p>
        <p className="text-sm mt-1">Your accepted, rejected, and expired suggestions will appear here.</p>
      </div>
    )
  }

  const columns: ColumnDef<TransferSuggestion>[] = [
    {
      id: 'date',
      header: 'Date',
      defaultSize: 140,
      minSize: 100,
      render: (item) => formatDate(item.created_at),
    },
    {
      id: 'amount',
      header: 'Amount',
      defaultSize: 120,
      minSize: 90,
      render: (item) => formatCurrency(item.amount, item.currency_code),
    },
    {
      id: 'status',
      header: 'Status',
      defaultSize: 120,
      minSize: 90,
      render: (item) => (
        <SuggestionStatusBadge status={item.status as 'pending' | 'accepted' | 'completed' | 'rejected' | 'expired'} />
      ),
    },
    {
      id: 'reason',
      header: 'Notes',
      resizable: true,
      defaultSize: 280,
      minSize: 160,
      render: (item) => <span className="text-sm text-muted-foreground">{item.reason ?? item.user_notes ?? '—'}</span>,
    },
  ]

  return (
    <div>
      <DataTable<TransferSuggestion>
        data={items}
        columns={columns}
        rowKey={(item) => item.id}
        tableId="boosts-history"
        sort={null}
        onSortChange={() => {}}
        filters={{}}
        onFiltersChange={() => {}}
        columnSizing={sizing}
        onColumnSizingChange={onSizingChange}
        maskedColumnIds={['amount']}
      />
      {showPagination && (
        <div className="p-4 border-t border-border">
          <TablePagination
            currentPage={currentPage}
            totalPages={totalPages}
            hasMore={currentPage < totalPages}
            onPageChange={(page) => onPageChange((page - 1) * limit)}
          />
        </div>
      )}
    </div>
  )
}
