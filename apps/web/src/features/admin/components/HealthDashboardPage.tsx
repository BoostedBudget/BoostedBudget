import { useState } from 'react';
import { useGetSystemHealth } from '@/packages/api-client';
import { SummaryCard } from '@/features/dashboard/components/SummaryCard';
import { AlertTriangle, Database, Shield, Landmark, RefreshCw } from 'lucide-react';
import './HealthDashboardPage.css';

function statusToVariant(status: string): 'success' | 'warning' | 'error' {
  if (status === 'up' || status === 'healthy') return 'success';
  if (status === 'degraded') return 'warning';
  return 'error';
}

export function HealthDashboardPage() {
  const [autoRefresh, setAutoRefresh] = useState(true);

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
            value={data.services.database.status}
            icon={<Database size={24} />}
            variant={statusToVariant(data.services.database.status)}
            subtitle={`${data.services.database.connections.active}/${data.services.database.connections.max} connections`}
          />
          <SummaryCard
            title="Clerk"
            value={data.services.clerk.status}
            icon={<Shield size={24} />}
            variant={statusToVariant(data.services.clerk.status)}
            subtitle={`${data.services.clerk.latency_ms}ms latency`}
          />
          <SummaryCard
            title="Plaid"
            value={data.services.plaid.status}
            icon={<Landmark size={24} />}
            variant={statusToVariant(data.services.plaid.status)}
            subtitle={data.services.plaid.environment}
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
            variant="info"
          />
          <SummaryCard
            title="Failed (Last Hour)"
            value={data.sync_status.failed_last_hour.toString()}
            variant={data.sync_status.failed_last_hour > 5 ? 'warning' : 'success'}
          />
          <SummaryCard
            title="Avg Duration"
            value={`${Math.round(data.sync_status.average_duration_ms / 1000)}s`}
            variant="info"
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
                  <span
                    className={`queue-card__value ${
                      metrics.failed > 0 ? 'queue-card__value--error' : ''
                    }`}
                  >
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
