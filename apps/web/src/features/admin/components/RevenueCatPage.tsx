import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import * as api from '@boostedbudget/api-client/endpoints/admin-subscriptions/admin-subscriptions'
import type { RevenueCatWebhookReceipt } from '@boostedbudget/api-client/models'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { DataTable, type ColumnDef as BBColumnDef, type SortState, type FilterState } from '@/shared/components/DataTable'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import { RevenueCatHealthSummary } from './RevenueCatHealthSummary'

const PAGE_SIZE = 20
function receiptStage(receipt: RevenueCatWebhookReceipt): string {
  if (receipt.processed_at) return 'Processed'
  if (receipt.last_error) return 'Retry pending'
  return 'Pending'
}
const columns: BBColumnDef<RevenueCatWebhookReceipt>[] = [
  { id: 'event', header: 'Event ID', key: 'event_id' },
  { id: 'user', header: 'User ID', key: 'user_id' },
  { id: 'type', header: 'Event Type', key: 'event_type', sortable: true },
  { id: 'stage', header: 'Stage', render: receiptStage },
  { id: 'deliveries', header: 'Deliveries', key: 'deliveries', sortable: true },
  { id: 'attempts', header: 'Processing Attempts', key: 'attempts', sortable: true },
  { id: 'received', header: 'First Received', render: r => new Date(r.received_at).toLocaleString() },
  { id: 'lastReceived', header: 'Last Received', render: r => new Date(r.last_received_at).toLocaleString() },
  { id: 'processed', header: 'Processed At', render: r => r.processed_at ? new Date(r.processed_at).toLocaleString() : 'Pending' },
  { id: 'error', header: 'Last Error', key: 'last_error' },
]

export function RevenueCatPage() {
  const [page, setPage] = useState(0)
  const [sort, setSort] = useState<SortState>(null)
  const [filters, setFilters] = useState<FilterState>({})
  const queryClient = useQueryClient()
  const trackEvent = useTrackEvent()
  useEffect(() => { trackEvent(ANALYTICS_EVENTS.ADMIN_PAGE_VIEWED, {page: 'revenuecat'}) }, [trackEvent])
  const params = {limit: PAGE_SIZE, offset: page * PAGE_SIZE}
  const receipts = useQuery({queryKey: api.getListAdminRevenueCatWebhooksQueryKey(params), queryFn: () => api.listAdminRevenueCatWebhooks(params)})
  const rows = [...(receipts.data ?? [])]
  if (sort) {
    let key: 'event_type' | 'deliveries' | 'attempts' = 'attempts'
    if (sort.columnId === 'type') key = 'event_type'
    if (sort.columnId === 'deliveries') key = 'deliveries'
    const direction = sort.direction === 'asc' ? 1 : -1
    rows.sort((a, b) => {
      const comparison = typeof a[key] === 'number'
        ? Number(a[key]) - Number(b[key])
        : String(a[key]).localeCompare(String(b[key]))
      return comparison * direction
    })
  }
  const replay = useMutation({
    mutationFn: (eventId: string) => api.replayAdminRevenueCatWebhook(eventId),
    onSuccess: () => {
      toast.info('Replay queued. Access changes appear after provider readback.')
      trackEvent(ANALYTICS_EVENTS.ADMIN_REVENUECAT_REPLAY_QUEUED)
      void queryClient.invalidateQueries({queryKey: ['/api/v1/admin/revenuecat/webhooks']})
      void queryClient.invalidateQueries({queryKey: api.getGetAdminRevenueCatHealthQueryKey()})
    },
    onError: () => toast.error('Could not queue replay. Try again.'),
  })
  return <div className="mx-auto max-w-[1400px] space-y-6 p-4 md:p-8">
    <h1 className="text-3xl font-semibold">RevenueCat Operations</h1>
    <RevenueCatHealthSummary />
    <div className="flex flex-wrap gap-4">
      <a className="text-primary underline" href="/admin/dead-letter?kind=revenuecat_sync">Sync dead letters</a>
      <a className="text-primary underline" href="/admin/dead-letter?kind=revenuecat_repair">Repair dead letters</a>
    </div>
    <section className="space-y-4">
      <h2 className="text-xl font-semibold">Webhook Receipts</h2>
      <p className="text-sm text-muted-foreground">Replay uses the existing receipt. Completed receipts refresh the customer; pending receipts retry processing. The delivery record and duplicate protection remain intact.</p>
      <Button variant="outline" onClick={() => void receipts.refetch()} disabled={receipts.isFetching}>Refresh receipts</Button>
      {receipts.isError ? <p role="alert">Could not load webhook receipts. Try again.</p> : <>
        <p className="text-sm text-muted-foreground">Sorting applies to this page.</p>
        <DataTable data={rows} columns={columns} rowKey={r => r.event_id} sensitiveRowKey tableId="admin-revenuecat-webhooks"
          sort={sort} onSortChange={setSort} filters={filters} onFiltersChange={setFilters} loading={receipts.isLoading}
          maskedColumnIds={['event', 'user', 'received', 'lastReceived', 'processed', 'error']}
          emptyState={<p>No receipts on this page.</p>}
          rowActions={r => <Button variant="outline" disabled={replay.isPending} onClick={() => replay.mutate(r.event_id)}>Replay receipt</Button>} />
        <div className="flex items-center gap-4">
          <Button variant="outline" disabled={page === 0 || receipts.isFetching} onClick={() => setPage(p => p - 1)}>Previous page</Button>
          <span>Page {page + 1}</span>
          <Button variant="outline" disabled={receipts.isFetching || (receipts.data?.length ?? 0) < PAGE_SIZE} onClick={() => setPage(p => p + 1)}>Next page</Button>
        </div>
      </>}
    </section>
  </div>
}
