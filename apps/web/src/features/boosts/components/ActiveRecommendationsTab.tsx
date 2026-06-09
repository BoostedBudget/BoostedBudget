import { Check, X } from 'lucide-react'
import type { TransferSuggestion } from '@boostedbudget/api-client/models'
import { Button } from '@/components/ui/button'
import { ReadOnlyGuard } from '@/auth/ReadOnlyGuard'
import { DataTable, useTablePreferences } from '@/shared/components/DataTable'
import type { ColumnDef } from '@/shared/components/DataTable'

const COLUMN_IDS = ['date', 'amount', 'destination', 'context', 'actions']

const formatCurrency = (amount: number, currency: string) =>
  new Intl.NumberFormat('en-US', { style: 'currency', currency, maximumFractionDigits: 0 }).format(amount)

const formatDate = (iso: string) =>
  new Date(iso).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })

export interface ActiveRecommendationsTabProps {
  items: TransferSuggestion[]
  isLoading: boolean
  onAccept: (id: string) => void
  onReject: (id: string) => void
  isPending: boolean
}

export function ActiveRecommendationsTab({
  items,
  isLoading,
  onAccept,
  onReject,
  isPending,
}: ActiveRecommendationsTabProps) {
  const { sizing, onSizingChange } = useTablePreferences('boosts-active', COLUMN_IDS)

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
        <p className="text-base font-medium">No active recommendations</p>
        <p className="text-sm mt-1">Your budgets are on track — we'll notify you when there's a move to make.</p>
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
      id: 'destination',
      header: 'Move To',
      defaultSize: 160,
      minSize: 110,
      render: (item) => (
        <span className="text-sm font-medium">{item.to_account_name ?? '—'}</span>
      ),
    },
    {
      id: 'context',
      header: 'Context',
      resizable: true,
      defaultSize: 240,
      minSize: 140,
      render: (item) => (
        <div className="flex flex-col gap-0.5">
          {item.rule_name && <span className="text-sm text-muted-foreground">{item.rule_name}</span>}
          {item.reason && <span className="text-xs text-muted-foreground">{item.reason}</span>}
          {!item.rule_name && !item.reason && <span className="text-sm text-muted-foreground">—</span>}
        </div>
      ),
    },
    {
      id: 'actions',
      header: 'Actions',
      defaultSize: 160,
      minSize: 140,
      render: (item) => (
        <ReadOnlyGuard>
          <div className="flex items-center gap-2">
            <Button
              size="sm"
              variant="default"
              disabled={isPending}
              onClick={() => onAccept(item.id)}
              aria-label="Accept transfer suggestion"
            >
              <Check className="h-3.5 w-3.5 mr-1" />
              Accept
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={isPending}
              onClick={() => onReject(item.id)}
              aria-label="Reject transfer suggestion"
            >
              <X className="h-3.5 w-3.5 mr-1" />
              Reject
            </Button>
          </div>
        </ReadOnlyGuard>
      ),
    },
  ]

  return (
    <DataTable<TransferSuggestion>
      data={items}
      columns={columns}
      rowKey={(item) => item.id}
      tableId="boosts-active"
      sort={null}
      onSortChange={() => {}}
      filters={{}}
      onFiltersChange={() => {}}
      columnSizing={sizing}
      onColumnSizingChange={onSizingChange}
      maskedColumnIds={['amount']}
    />
  )
}
