import { CheckCircle2, XCircle } from 'lucide-react'
import type { TransferSuggestion } from '@boostedbudget/api-client/models'
import { Button } from '@/components/ui/button'
import { ReadOnlyGuard } from '@/auth/ReadOnlyGuard'
import { DataTable, useTablePreferences } from '@/shared/components/DataTable'
import type { ColumnDef } from '@/shared/components/DataTable'

const COLUMN_IDS = ['accepted_date', 'amount', 'status_info', 'actions']

const formatCurrency = (amount: number, currency: string) =>
  new Intl.NumberFormat('en-US', { style: 'currency', currency, maximumFractionDigits: 0 }).format(amount)

const formatDate = (iso: string) =>
  new Date(iso).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })

export interface AwaitingMatchTabProps {
  items: TransferSuggestion[]
  isLoading: boolean
  onConfirmMatch: (id: string, actualAmount: number) => void
  /** Cancel an accepted suggestion, transitioning it to rejected (leaves the tab) */
  onCancelAccepted: (id: string) => void
  isPending: boolean
}

export function AwaitingMatchTab({
  items,
  isLoading,
  onConfirmMatch,
  onCancelAccepted,
  isPending,
}: AwaitingMatchTabProps) {
  const { sizing, onSizingChange } = useTablePreferences('boosts-awaiting', COLUMN_IDS)

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
        <p className="text-base font-medium">No transfers waiting to be matched</p>
        <p className="text-sm mt-1">Accepted suggestions appear here while we watch for your transfer.</p>
      </div>
    )
  }

  const columns: ColumnDef<TransferSuggestion>[] = [
    {
      id: 'accepted_date',
      header: 'Accepted On',
      defaultSize: 150,
      minSize: 110,
      render: (item) => formatDate(item.user_action_at ?? item.updated_at),
    },
    {
      id: 'amount',
      header: 'Amount',
      defaultSize: 120,
      minSize: 90,
      render: (item) => formatCurrency(item.amount, item.currency_code),
    },
    {
      id: 'status_info',
      header: 'Status',
      resizable: true,
      defaultSize: 280,
      minSize: 160,
      render: (item) => {
        const destPart = item.to_account_name
          ? ` to ${item.to_account_name} of ${formatCurrency(item.amount, item.currency_code)}`
          : ''
        return (
          <span className="text-sm text-muted-foreground">
            Accepted on {formatDate(item.user_action_at ?? item.updated_at)} — watching for transfer{destPart}
          </span>
        )
      },
    },
    {
      id: 'actions',
      header: 'Actions',
      defaultSize: 200,
      minSize: 170,
      render: (item) => (
        <ReadOnlyGuard>
          <div className="flex items-center gap-2">
            <Button
              size="sm"
              variant="default"
              disabled={isPending || !item.can_confirm_match}
              title={!item.can_confirm_match ? 'Waiting for an automatic match before confirming' : undefined}
              onClick={() => onConfirmMatch(item.id, item.actual_amount ?? item.amount)}
              aria-label="Mark Done"
            >
              <CheckCircle2 className="h-3.5 w-3.5 mr-1" />
              Mark Done
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={isPending}
              onClick={() => onCancelAccepted(item.id)}
              aria-label="Cancel"
            >
              <XCircle className="h-3.5 w-3.5 mr-1" />
              Cancel
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
      tableId="boosts-awaiting"
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
