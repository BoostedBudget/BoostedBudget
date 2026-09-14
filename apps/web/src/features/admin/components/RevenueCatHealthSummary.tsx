import { useQuery } from '@tanstack/react-query'
import * as api from '@boostedbudget/api-client/endpoints/admin-subscriptions/admin-subscriptions'
import { Activity } from 'lucide-react'
import { SummaryCard } from './SummaryCard'

export function RevenueCatHealthSummary() {
  const { data, isLoading, isError } = useQuery({
    queryKey: api.getGetAdminRevenueCatHealthQueryKey(),
    queryFn: () => api.getAdminRevenueCatHealth(),
    refetchInterval: 30000,
  })
  const metrics = !isError && data?.available ? data.metrics : undefined
  const counters = [
    ['Customers', metrics?.customers], ['Events', metrics?.events],
    ['Deliveries', metrics?.deliveries], ['Processing Attempts', metrics?.processing_attempts],
    ['Processed Events', metrics?.processed_events], ['Failed Webhooks', metrics?.failed_webhooks],
    ['Pending Operations', metrics?.pending_operations], ['Failed Operations', metrics?.failed_operations],
    ['Access Failures', metrics?.access_failures], ['Billing Failures', metrics?.billing_failures],
    ['Unresolved Customers', metrics?.unresolved_customers], ['Stale Active Customers', metrics?.stale_active_customers],
  ] as const
  return <section className="mb-8 space-y-4" aria-label="RevenueCat health">
    <div className="flex flex-wrap items-center justify-between gap-2">
      <h2 className="text-xl font-semibold">RevenueCat</h2>
      <a className="text-primary underline" href="/admin/revenuecat">Open RevenueCat operations</a>
    </div>
    <p>{isLoading ? 'Loading RevenueCat health…' : data && !isError ? (data.configured ? 'Configured' : 'Not configured') : 'Configuration unavailable'}</p>
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
      {counters.map(([title, value]) => <SummaryCard key={title} title={title} value={value == null ? 'Unavailable' : String(value)} status={value == null ? 'warning' : 'neutral'} icon={<Activity size={24} />} />)}
    </div>
    <p>Oldest pending receipt: <span data-ph-mask>{metrics ? (metrics.oldest_pending_received_at ? new Date(metrics.oldest_pending_received_at).toLocaleString() : 'None') : 'Unavailable'}</span></p>
    <p className="text-sm text-muted-foreground">Stale active customers indicate access sync lag. Counters reflect persisted receipts and operations; deliveries and processing attempts are separate.</p>
  </section>
}
