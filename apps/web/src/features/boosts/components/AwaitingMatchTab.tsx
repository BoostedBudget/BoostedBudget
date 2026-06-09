import { CheckCircle2, XCircle, Clock } from 'lucide-react'
import type { TransferSuggestion } from '@boostedbudget/api-client/models'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/ui/empty-state'
import { ErrorState } from '@/components/ui/error-state'
import { ReadOnlyGuard } from '@/auth/ReadOnlyGuard'
import { DataTable, useTablePreferences } from '@/shared/components/DataTable'
import type { ColumnDef } from '@/shared/components/DataTable'
import { formatCurrency, formatDate } from '../constants'

const COLUMN_IDS = ['accepted_date', 'amount', 'status_info', 'actions']

export interface AwaitingMatchTabProps {
  items: TransferSuggestion[]
  isLoading: boolean
  onConfirmMatch: (id: string, actualAmount: number) => void
  /** Cancel an accepted suggestion, transitioning it to rejected (leaves the tab) */
  onCancelAccepted: (id: string) => void
  isPending: boolean
  error?: Error | null
  onRetry?: () => void
}

export function AwaitingMatchTab({
  items,
  isLoading,
  onConfirmMatch,
  onCancelAccepted,
  isPending,
  error,
  onRetry,
}: AwaitingMatchTabProps) {
  const { sizing, onSizingChange } = useTablePreferences('boosts-awaiting', COLUMN_IDS)

  if (error) {
    return (
      <ErrorState
        title="Couldn't load awaiting transfers"
        description="There was a problem fetching transfers waiting for a match."
        onRetry={onRetry}
      />
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
        const amountStr = formatCurrency(item.amount, item.currency_code)
        const destPart = item.to_account_name
          ? ` to ${item.to_account_name} of ${amountStr}`
          : ` of ${amountStr}`
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
      loading={isLoading}
      skeletonRows={3}
      maskedColumnIds={['amount', 'status_info']}
      emptyState={
        <EmptyState
          icon={<Clock />}
          title="No transfers waiting to be matched"
          description="Accepted suggestions appear here while we watch for your transfer."
        />
      }
    />
  )
}
