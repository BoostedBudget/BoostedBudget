import type { TransferSuggestion } from '@boostedbudget/api-client/models'
import { DataTable, TablePagination, useTablePreferences } from '@/shared/components/DataTable'
import type { ColumnDef } from '@/shared/components/DataTable'
import { SuggestionStatusBadge } from './SuggestionStatusBadge'

const COLUMN_IDS = ['event_date', 'amount', 'status', 'source', 'notes']

const formatCurrency = (amount: number, currency: string) =>
  new Intl.NumberFormat('en-US', { style: 'currency', currency, maximumFractionDigits: 0 }).format(amount)

const formatDate = (iso: string) =>
  new Date(iso).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })

/** Most-informative timestamp: completed > acted-on > updated > created */
const eventDate = (item: TransferSuggestion): string =>
  item.completed_at ?? item.user_action_at ?? item.updated_at ?? item.created_at

/** Human-readable completion source label */
const formatCompletionSource = (source?: string): string => {
  if (source === 'exact_match') return 'Auto-matched'
  if (source === 'user_confirmed') return 'You confirmed'
  return '—'
}

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
      id: 'event_date',
      header: 'Date',
      defaultSize: 140,
      minSize: 100,
      render: (item) => formatDate(eventDate(item)),
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
      id: 'source',
      header: 'Source',
      defaultSize: 140,
      minSize: 110,
      render: (item) => (
        <span className="text-sm text-muted-foreground">{formatCompletionSource(item.completion_source)}</span>
      ),
    },
    {
      id: 'notes',
      header: 'Notes',
      resizable: true,
      defaultSize: 240,
      minSize: 140,
      render: (item) => {
        const note = item.user_notes ?? item.reason
        const matchedAt = item.matched_transaction_at
        if (matchedAt) {
          return (
            <span className="text-sm text-muted-foreground">
              Matched transaction on {formatDate(matchedAt)}
              {note ? ` · ${note}` : ''}
            </span>
          )
        }
        return <span className="text-sm text-muted-foreground">{note ?? '—'}</span>
      },
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
