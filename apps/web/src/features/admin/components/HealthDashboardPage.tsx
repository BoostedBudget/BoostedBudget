import { useState, useEffect } from 'react';
import { useGetSystemHealth } from '@boostedbudget/api-client/endpoints/admin/admin';
import { useTrackEvent } from '../../../shared/hooks/useTrackEvent';
import { ANALYTICS_EVENTS } from '../../../shared/analytics';
import { SummaryCard } from '@/features/admin';
import { AlertTriangle, Database, Shield, Landmark, RefreshCw, Clock, CreditCard, CheckCircle } from 'lucide-react';
import { cn } from '@/lib/utils';

const statusToVariant = (status: string): 'success' | 'warning' | 'error' => {
  if (status === 'up' || status === 'healthy') return 'success';
  if (status === 'degraded') return 'warning';
  return 'error';
};

const statusTextClass = (status: string) => {
  if (status === 'healthy') return 'text-success';
  if (status === 'degraded') return 'text-warning';
  return 'text-destructive';
};

export function HealthDashboardPage() {
  const [autoRefresh, setAutoRefresh] = useState(true);
  const trackEvent = useTrackEvent();

  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.ADMIN_PAGE_VIEWED, { page: 'health' });
  }, [trackEvent]);

  const { data, isLoading, error, refetch } = useGetSystemHealth({
    query: {
      refetchInterval: autoRefresh ? 30000 : false,
      refetchIntervalInBackground: false,
    },
  });

  if (isLoading) {
    return (
      <div className="p-8 max-w-[1400px] mx-auto">
        <h1 className="text-2xl font-semibold">System Health</h1>
        <p className="text-muted-foreground">Loading system health metrics...</p>
      </div>
    );
  }

  if (error) {
    return (
      <div className="p-8 max-w-[1400px] mx-auto">
        <h1 className="text-2xl font-semibold">System Health</h1>
        <p className="text-destructive bg-destructive/10 border border-destructive/20 rounded-lg p-4">
          Error loading system health: {error.message}
        </p>
      </div>
    );
  }

  if (!data) {
    return null;
  }

  return (
    <div className="p-8 max-w-[1400px] mx-auto max-md:p-4">
      <header className="flex justify-between items-center mb-8 flex-wrap gap-4 max-md:flex-col max-md:items-start">
        <div className="flex flex-col gap-2">
          <h1 className="m-0 text-3xl font-semibold text-foreground">System Health</h1>
          <span className="text-sm text-muted-foreground">
            Status:{' '}
            <strong className={statusTextClass(data.status)}>{data.status.toUpperCase()}</strong>
          </span>
        </div>
        <div className="flex gap-2 max-md:w-full">
          <button
            onClick={() => setAutoRefresh(!autoRefresh)}
            className="flex items-center gap-2 px-4 py-2 border border-border rounded-md bg-card text-foreground text-sm cursor-pointer hover:bg-accent transition-colors max-md:flex-1 max-md:justify-center"
          >
            <RefreshCw className={cn('w-4 h-4', autoRefresh && 'animate-spin')} size={16} />
            Auto-refresh: {autoRefresh ? 'ON' : 'OFF'}
          </button>
          <button
            onClick={() => refetch()}
            className="flex items-center gap-2 px-4 py-2 border border-border rounded-md bg-card text-foreground text-sm cursor-pointer hover:bg-accent transition-colors max-md:flex-1 max-md:justify-center"
          >
            <RefreshCw size={16} />
            Refresh
          </button>
        </div>
      </header>

      {/* Dead Letter Alert */}
      {data.dead_letter_queue.count > 0 && (
        <div className="flex items-center gap-4 p-4 mb-8 bg-warning/10 border border-warning/20 rounded-lg text-warning">
          <AlertTriangle size={20} />
          <div className="flex-1">
            <strong>Dead Letter Queue Alert:</strong> {data.dead_letter_queue.count} failed jobs
            (oldest: {data.dead_letter_queue.oldest_job_age_hours}h ago)
          </div>
        </div>
      )}

      {/* Service Status Section */}
      <section className="mb-8">
        <h2 className="m-0 mb-4 text-xl font-semibold text-foreground">Services</h2>
        <div className="grid gap-4 [grid-template-columns:repeat(auto-fit,minmax(250px,1fr))] max-md:[grid-template-columns:1fr]">
          <SummaryCard
            title="Database"
            value={`${data.services.database.connections.active}/${data.services.database.connections.max} connections`}
            icon={<Database size={24} />}
            status={statusToVariant(data.services.database.status)}
          />
          <SummaryCard
            title="Clerk"
            value={`${data.services.clerk.latency_ms}ms`}
            icon={<Shield size={24} />}
            status={statusToVariant(data.services.clerk.status)}
          />
          <SummaryCard
            title="Plaid"
            value={data.services.plaid.environment}
            icon={<Landmark size={24} />}
            status={statusToVariant(data.services.plaid.status)}
          />
        </div>
      </section>

      {/* Sync Status Section */}
      <section className="mb-8">
        <h2 className="m-0 mb-4 text-xl font-semibold text-foreground">Plaid Sync Status</h2>
        <div className="grid gap-4 [grid-template-columns:repeat(auto-fit,minmax(250px,1fr))] max-md:[grid-template-columns:1fr]">
          <SummaryCard
            title="Active Syncs"
            value={data.sync_status.active_syncs.toString()}
            icon={<RefreshCw size={24} />}
            status="neutral"
          />
          <SummaryCard
            title="Failed (Last Hour)"
            value={data.sync_status.failed_last_hour.toString()}
            icon={<AlertTriangle size={24} />}
            status={data.sync_status.failed_last_hour > 5 ? 'warning' : 'success'}
          />
          <SummaryCard
            title="Avg Duration"
            value={`${Math.round(data.sync_status.average_duration_ms / 1000)}s`}
            icon={<Clock size={24} />}
            status="neutral"
          />
        </div>
      </section>

      {/* Paddle Status Section */}
      <section className="mb-8">
        <h2 className="m-0 mb-4 text-xl font-semibold text-foreground">Paddle Status</h2>
        <div className="grid gap-4 [grid-template-columns:repeat(auto-fit,minmax(250px,1fr))] max-md:[grid-template-columns:1fr]">
          <SummaryCard
            title="Active Subscriptions"
            value={data.paddle_status.active_subscriptions.toString()}
            icon={<CreditCard size={24} />}
            status="neutral"
          />
          <SummaryCard
            title="Webhook Success (24h)"
            value={`${data.paddle_status.webhook_success_rate_24h.toFixed(1)}%`}
            icon={<CheckCircle size={24} />}
            status={data.paddle_status.webhook_success_rate_24h < 90 ? 'warning' : 'success'}
          />
          <SummaryCard
            title="Failed Webhooks (24h)"
            value={data.paddle_status.failed_webhooks_24h.toString()}
            icon={<AlertTriangle size={24} />}
            status={data.paddle_status.failed_webhooks_24h > 5 ? 'warning' : 'success'}
          />
          <SummaryCard
            title="Last Reconciliation"
            value={
              data.paddle_status.last_reconciliation_at
                ? new Date(data.paddle_status.last_reconciliation_at).toLocaleString()
                : 'Never'
            }
            icon={<RefreshCw size={24} />}
            status={data.paddle_status.last_reconciliation_at ? 'success' : 'warning'}
          />
        </div>
      </section>

      {/* Job Queues Section */}
      <section className="mb-8">
        <h2 className="m-0 mb-4 text-xl font-semibold text-foreground">Job Queues</h2>
        <div className="grid gap-4 [grid-template-columns:repeat(auto-fit,minmax(280px,1fr))] max-md:[grid-template-columns:1fr]">
          {Object.entries(data.job_queues).map(([queueName, metrics]) => (
            <div key={queueName} className="p-4 bg-card border border-border rounded-lg">
              <h3 className="m-0 mb-3 text-base font-semibold text-foreground">{queueName}</h3>
              <div className="flex flex-col gap-2">
                <div className="flex justify-between items-center">
                  <span className="text-sm text-muted-foreground">Pending:</span>
                  <span className="text-sm font-semibold text-foreground">{metrics.pending}</span>
                </div>
                <div className="flex justify-between items-center">
                  <span className="text-sm text-muted-foreground">Running:</span>
                  <span className="text-sm font-semibold text-info">{metrics.running}</span>
                </div>
                <div className="flex justify-between items-center">
                  <span className="text-sm text-muted-foreground">Failed:</span>
                  <span className={cn('text-sm font-semibold', metrics.failed > 0 ? 'text-destructive' : 'text-foreground')}>
                    {metrics.failed}
                  </span>
                </div>
                <div className="flex justify-between items-center">
                  <span className="text-sm text-muted-foreground">Completed (1h):</span>
                  <span className="text-sm font-semibold text-foreground">{metrics.completed_last_hour}</span>
                </div>
              </div>
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}
