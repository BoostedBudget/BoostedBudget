import { useState, useEffect } from 'react';
import { useGetSystemHealth } from '@boostedbudget/api-client/endpoints/admin/admin';
import { useTrackEvent } from '../../../shared/hooks/useTrackEvent';
import { ANALYTICS_EVENTS } from '../../../shared/analytics';
import { SummaryCard } from '@/features/admin';
import { AlertTriangle, Database, Shield, Landmark, RefreshCw, Clock, CreditCard, CheckCircle } from 'lucide-react';
import './HealthDashboardPage.css';

const statusToVariant = (status: string): 'success' | 'warning' | 'error' => {
  if (status === 'up' || status === 'healthy') return 'success';
  if (status === 'degraded') return 'warning';
  return 'error';
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
      <div className="health-dashboard">
        <h1>System Health</h1>
        <p>Loading system health metrics...</p>
      </div>
    );
  }

  if (error) {
    return (
      <div className="health-dashboard">
        <h1>System Health</h1>
        <p className="error-message">Error loading system health: {error.message}</p>
      </div>
    );
  }

  if (!data) {
    return null;
  }

  return (
    <div className="health-dashboard">
      <header className="health-dashboard__header">
        <div className="health-dashboard__title-group">
          <h1>System Health</h1>
          <span className="health-dashboard__overall-status">
            Status: <strong className={`status-${data.status}`}>{data.status.toUpperCase()}</strong>
          </span>
        </div>
        <div className="health-dashboard__controls">
          <button
            onClick={() => setAutoRefresh(!autoRefresh)}
            className="health-dashboard__button"
          >
            <RefreshCw className={autoRefresh ? 'spinning' : ''} size={16} />
            Auto-refresh: {autoRefresh ? 'ON' : 'OFF'}
          </button>
          <button onClick={() => refetch()} className="health-dashboard__button">
            <RefreshCw size={16} />
            Refresh
          </button>
        </div>
      </header>

      {/* Dead Letter Alert */}
      {data.dead_letter_queue.count > 0 && (
        <div className="dead-letter-alert">
          <AlertTriangle size={20} />
          <div className="dead-letter-alert__content">
            <strong>Dead Letter Queue Alert:</strong> {data.dead_letter_queue.count} failed jobs
            (oldest: {data.dead_letter_queue.oldest_job_age_hours}h ago)
          </div>
        </div>
      )}

      {/* Service Status Section */}
      <section className="health-dashboard__section">
        <h2>Services</h2>
        <div className="health-dashboard__grid">
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
      <section className="health-dashboard__section">
        <h2>Plaid Sync Status</h2>
        <div className="health-dashboard__grid">
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
      <section className="health-dashboard__section">
        <h2>Paddle Status</h2>
        <div className="health-dashboard__grid">
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
      <section className="health-dashboard__section">
        <h2>Job Queues</h2>
        <div className="health-dashboard__queue-grid">
          {Object.entries(data.job_queues).map(([queueName, metrics]) => (
            <div key={queueName} className="queue-card">
              <h3 className="queue-card__name">{queueName}</h3>
              <div className="queue-card__metrics">
                <div className="queue-card__metric">
                  <span className="queue-card__label">Pending:</span>
                  <span className="queue-card__value">{metrics.pending}</span>
                </div>
                <div className="queue-card__metric">
                  <span className="queue-card__label">Running:</span>
                  <span className="queue-card__value queue-card__value--running">
                    {metrics.running}
                  </span>
                </div>
                <div className="queue-card__metric">
                  <span className="queue-card__label">Failed:</span>
                  <span className={`queue-card__value${metrics.failed > 0 ? ' queue-card__value--error' : ''}`}>
                    {metrics.failed}
                  </span>
                </div>
                <div className="queue-card__metric">
                  <span className="queue-card__label">Completed (1h):</span>
                  <span className="queue-card__value">{metrics.completed_last_hour}</span>
                </div>
              </div>
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}
