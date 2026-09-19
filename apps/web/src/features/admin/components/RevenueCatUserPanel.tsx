import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import * as api from '@boostedbudget/api-client/endpoints/admin-subscriptions/admin-subscriptions'
import type { AdminRevenueCatGrantInputBody, RevenueCatAccess, RevenueCatOperation } from '@boostedbudget/api-client/models'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { DataTable, type ColumnDef as BBColumnDef, type FilterState, type SortState } from '@/shared/components/DataTable'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import { RevenueCatGrantForm } from './RevenueCatGrantForm'

function date(value: string | null | undefined): string {
  return value ? new Date(value).toLocaleString() : 'Not available'
}

function operationExpiry(operation: RevenueCatOperation): string {
  if (operation.revoke) return '—'
  return operation.expires_at ? date(operation.expires_at) : 'Permanent'
}
const operationColumns: BBColumnDef<RevenueCatOperation>[] = [
  {id: 'id', header: 'Operation ID', key: 'id'},
  {id: 'class', header: 'Class', key: 'class'},
  {id: 'key', header: 'Entitlement', key: 'key'},
  {id: 'action', header: 'Action', render: op => op.revoke ? 'Revoke' : 'Grant'},
  {id: 'expiry', header: 'Expiry', render: operationExpiry},
  {id: 'stage', header: 'Stage', key: 'stage', sortable: true},
  {id: 'attempts', header: 'Attempts', key: 'attempts', sortable: true},
  {id: 'created', header: 'Requested At', render: op => date(op.created_at)},
  {id: 'actor', header: 'Actor ID', render: op => op.actor_id ?? 'System'},
  {id: 'reason', header: 'Reason', key: 'reason'},
  {id: 'error', header: 'Last Error', key: 'last_error'},
]
function AccessCard({title, access, absent = 'Unavailable'}: {title: string; access: RevenueCatAccess | null | undefined; absent?: string}) {
  return <div className="rounded-lg border border-border p-3"><h4 className="font-medium">{title}</h4>
    {access ? <><p>{access.tier || 'None'}</p><p>{access.status} · {access.source}</p>
      <p data-ph-mask>{access.expires_at ? `Expires ${date(access.expires_at)}` : 'No observed expiry'}</p></> : <p>{absent}</p>}
  </div>
}

export function RevenueCatUserPanel({userId}: {userId: string}) {
  const queryClient = useQueryClient()
  const trackEvent = useTrackEvent()
  const [sort, setSort] = useState<SortState>(null)
  const [filters, setFilters] = useState<FilterState>({})
  const queryKey = api.getGetAdminRevenueCatUserQueryKey(userId)
  const query = useQuery({queryKey, queryFn: () => api.getAdminRevenueCatUser(userId), refetchInterval: 15000})
  const invalidate = () => {
    void queryClient.invalidateQueries({queryKey})
    void queryClient.invalidateQueries({queryKey: api.getGetAdminRevenueCatHealthQueryKey()})
  }
  const refresh = useMutation({mutationFn: () => api.refreshAdminRevenueCatUser(userId), onSuccess: () => {
    toast.info('Refresh queued. Access changes appear after provider readback.')
    trackEvent(ANALYTICS_EVENTS.ADMIN_REVENUECAT_REFRESH_QUEUED)
    invalidate()
  }, onError: () => toast.error('Could not queue refresh. Try again.')})
  const retry = useMutation({mutationFn: (operationId: string) => api.retryAdminRevenueCatOperation(userId, operationId), onSuccess: () => {
    toast.info('Retry queued. Access changes appear after provider readback.')
    trackEvent(ANALYTICS_EVENTS.ADMIN_REVENUECAT_RETRY_QUEUED)
    invalidate()
  }, onError: () => toast.error('Could not queue retry. Reload the operation and try again.')})
  const grant = useMutation({mutationFn: (body: AdminRevenueCatGrantInputBody) => api.requestAdminRevenueCatGrant(userId, body), onSuccess: (_result, body) => {
    toast.info('Change queued. Access changes appear after provider readback.')
    trackEvent(ANALYTICS_EVENTS.ADMIN_REVENUECAT_GRANT_QUEUED, {grant_class: body.class, tier: body.tier, revoke: body.revoke})
    invalidate()
  }, onError: () => toast.error('Could not queue change. Check eligibility and try again.')})

  if (query.isPending) return <p>Loading RevenueCat diagnostics…</p>
  if (query.isError || !query.data) return <div role="alert"><p>Could not load RevenueCat diagnostics.</p><Button variant="outline" onClick={() => void query.refetch()}>Reload diagnostics</Button></div>
  const {summary, state, operations} = query.data
  const rows = [...(operations ?? [])]
  if (sort) rows.sort((a, b) => (sort.columnId === 'attempts' ? a.attempts - b.attempts : a.stage.localeCompare(b.stage)) * (sort.direction === 'asc' ? 1 : -1))
  const fenced = Boolean(state?.erasure_started_at || state?.erasure_confirmed_at)
  const writesBlocked = fenced || !state
  const effective = summary.access_resolved ? summary.effective : null
  const absentGrant = summary.own ? 'No grant' : 'Unavailable'
  const pending = refresh.isPending || retry.isPending || grant.isPending
  return <section className="space-y-6" aria-label="RevenueCat user diagnostics">
    <h2 className="text-xl font-semibold">RevenueCat Diagnostics</h2>
    <p>User ID: <span data-ph-mask>{state?.user_id ?? userId}</span></p>
    {!state && <p role="alert">Customer state is unavailable. Grants, refresh, and retries are disabled until diagnostics can be reloaded.</p>}
    {fenced && <p role="alert" className="rounded-lg border border-destructive p-3 text-destructive">Erasure fence: {state?.erasure_confirmed_at ? 'provider erasure confirmed' : 'erasure started'}. Grants, refresh, and retries are disabled. This screen cannot remove the fence or recreate the customer.</p>}
    <div className="flex flex-wrap gap-2">
      <Button variant="outline" disabled={writesBlocked || pending} onClick={() => refresh.mutate()}>Refresh customer</Button>
      <Button variant="outline" disabled={query.isFetching} onClick={() => void query.refetch()}>Reload diagnostics</Button>
    </div>
    <section className="space-y-3">
      <h3 className="text-lg font-semibold">Observed Access</h3>
      {!effective && <p role="status">Access unresolved. Reload diagnostics before changing grants.</p>}
      <p data-ph-mask>Evaluated: {date(summary.as_of)} · Access updated: {date(state?.access_updated_at)} · Billing updated: {date(state?.billing_updated_at)}</p>
      <div className="grid gap-3 sm:grid-cols-2">
        <AccessCard title="Own Access" access={summary.own} />
        <AccessCard title="Paid Access" access={summary.paid} absent={absentGrant} />
        <AccessCard title="Complimentary Access" access={summary.comp} absent={absentGrant} />
        <AccessCard title="Trial Access" access={summary.trial} absent={absentGrant} />
        <AccessCard title="Effective Access" access={effective ? {tier: effective.Tier, status: effective.Status, source: effective.Source, expires_at: null} : null} />
      </div>
      <p>Household ID: <span data-ph-mask>{effective ? effective.HouseholdID ?? 'None' : 'Unavailable'}</span> · Payer ID: <span data-ph-mask>{effective ? effective.PayerUserID ?? 'None' : 'Unavailable'}</span></p>
      {state?.access_error && <p>Access error: <span data-ph-mask>{state.access_error}</span></p>}
      {state?.billing_error && <p>Billing error: <span data-ph-mask>{state.billing_error}</span></p>}
      <p>Pending operations: {state?.pending_operations ?? summary.pending_operations}. Queued changes are not applied access.</p>
    </section>
    <section className="space-y-3"><h3 className="text-lg font-semibold">Own Billing</h3>
      <p className="text-sm text-muted-foreground">These are this user's billing subscriptions. Household access can come from another payer.</p>
      {!summary.billing_available ? <p>Billing unavailable.</p> : !summary.billing?.length ? <p>No billing subscriptions.</p> : summary.billing.map(b => <dl className="rounded-lg border border-border p-3" key={b.id}>
        <dt>Subscription ID</dt><dd data-ph-mask>{b.id}</dd>
        <dt>Store Subscription ID</dt><dd data-ph-mask>{b.store_subscription_identifier}</dd>
        <dt>Product</dt><dd>{b.product_id}</dd>
        <dt>Status</dt><dd>{b.status} · {b.store} · {b.environment}</dd>
        <dt>Auto-renewal</dt><dd>{b.auto_renewal_status}</dd>
        <dt>Current Period Ends</dt><dd data-ph-mask>{date(b.current_period_ends_at)}</dd>
        <dt>Subscription Ends</dt><dd data-ph-mask>{date(b.ends_at)}</dd>
      </dl>)}
    </section>
    <RevenueCatGrantForm disabled={writesBlocked || !effective} pending={pending} onSubmit={body => grant.mutate(body)} />
    <section className="space-y-3"><h3 className="text-lg font-semibold">Operation History</h3>
      <DataTable data={rows} columns={operationColumns} rowKey={op => op.id} sensitiveRowKey tableId="admin-revenuecat-operations"
        sort={sort} onSortChange={setSort} filters={filters} onFiltersChange={setFilters}
        maskedColumnIds={['id', 'expiry', 'created', 'actor', 'reason', 'error']}
        emptyState={<p>No operations.</p>}
        rowActions={op => op.stage === 'pending' ? <Button variant="outline" disabled={writesBlocked || pending} onClick={() => retry.mutate(op.id)}>Retry operation</Button> : null} />
    </section>
  </section>
}
