/**
 * WebhooksPage Component
 * Issue #747: Paddle Webhook Admin UI
 *
 * Admin page for monitoring and replaying Paddle webhook events.
 */

import { useState, useEffect } from 'react'
import { useTrackEvent } from '../../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../../shared/analytics'
import { RefreshCw, AlertCircle, Webhook, ChevronLeft, ChevronRight } from 'lucide-react'
import {
  useListAdminPaddleWebhooks,
  useGetAdminPaddleWebhookStats,
  useReplayAdminPaddleWebhook,
} from '@boostedbudget/api-client/endpoints/admin/admin'
import { SummaryCard } from '@/features/admin'
import { useWebhookFilters } from '../hooks/useWebhookFilters'
import { WebhookDetailModal } from './WebhookDetailModal'
import './WebhooksPage.css'

const EVENT_TYPE_OPTIONS = [
  { value: '', label: 'All Event Types' },
  { value: 'subscription.created', label: 'subscription.created' },
  { value: 'subscription.activated', label: 'subscription.activated' },
  { value: 'subscription.updated', label: 'subscription.updated' },
  { value: 'subscription.canceled', label: 'subscription.canceled' },
  { value: 'subscription.paused', label: 'subscription.paused' },
  { value: 'subscription.past_due', label: 'subscription.past_due' },
  { value: 'subscription.resumed', label: 'subscription.resumed' },
  { value: 'subscription.trialing', label: 'subscription.trialing' },
  { value: 'transaction.completed', label: 'transaction.completed' },
  { value: 'transaction.payment_failed', label: 'transaction.payment_failed' },
]

const PAGE_SIZE = 20

export function WebhooksPage() {
  const [autoRefresh, setAutoRefresh] = useState(true)
  const [selectedWebhookId, setSelectedWebhookId] = useState<string | null>(null)
  const trackEvent = useTrackEvent()

  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.ADMIN_PAGE_VIEWED, { page: 'webhooks' })
  }, [trackEvent])

  const { filters, setEventType, setProcessed, setStartDate, setEndDate, setPage, hasActiveFilters, resetFilters } = useWebhookFilters()

  // Build API params
  const listParams = {
    limit: PAGE_SIZE,
    offset: (filters.page - 1) * PAGE_SIZE,
    ...(filters.eventType && { event_type: filters.eventType }),
    ...(filters.processed !== '' && { processed: filters.processed }),
    ...(filters.startDate && { start_date: filters.startDate }),
    ...(filters.endDate && { end_date: filters.endDate }),
  }

  const { data, isLoading, error } = useListAdminPaddleWebhooks(listParams, {
    query: {
      refetchInterval: autoRefresh ? 30000 : false,
    },
  })

  const { data: statsData } = useGetAdminPaddleWebhookStats(
    { time_range: '24h' },
    { query: { refetchInterval: autoRefresh ? 30000 : false } }
  )

  const replayMutation = useReplayAdminPaddleWebhook()

  const events = data?.Events ?? []
  const total = data?.Total ?? 0
  const currentPage = data?.Page ?? 1
  const totalPages = data?.TotalPages ?? 0
  const hasMore = data?.HasMore ?? false
  const isEmpty = !isLoading && !error && events.length === 0

  return (
    <div className="webhooks-page">
      {/* Header */}
      <header className="webhooks-page__header">
        <div>
          <h1 className="webhooks-page__title">
            Paddle Webhooks
            {total > 0 && <span className="webhooks-page__count">{total} events</span>}
          </h1>
          <p className="webhooks-page__subtitle">Monitor and replay Paddle webhook events.</p>
        </div>
        <div className="webhooks-page__controls">
          <button
            type="button"
            className="webhooks-page__button"
            onClick={() => setAutoRefresh((v) => !v)}
          >
            <RefreshCw size={16} className={autoRefresh ? 'spinning' : ''} />
            Auto-refresh: {autoRefresh ? 'ON' : 'OFF'}
          </button>
        </div>
      </header>

      {/* Stats Cards */}
      {statsData && (
        <div className="webhooks-page__stats-grid">
          <SummaryCard
            title="Total (24h)"
            value={statsData.Total.toString()}
            icon={<Webhook size={24} />}
            status="neutral"
          />
          <SummaryCard
            title="Processed"
            value={statsData.Processed.toString()}
            icon={<Webhook size={24} />}
            status="success"
          />
          <SummaryCard
            title="Failed"
            value={statsData.Failed.toString()}
            icon={<AlertCircle size={24} />}
            status={statsData.Failed > 0 ? 'warning' : 'success'}
          />
          <SummaryCard
            title="Avg Processing"
            value={`${statsData.AvgProcessingTimeMs}ms`}
            icon={<RefreshCw size={24} />}
            status="neutral"
          />
        </div>
      )}

      {/* Filters */}
      <div className="webhooks-page__filters">
        <select
          className="webhooks-page__select"
          value={filters.eventType}
          onChange={(e) => setEventType(e.target.value)}
          aria-label="Filter by event type"
        >
          {EVENT_TYPE_OPTIONS.map((opt) => (
            <option key={opt.value} value={opt.value}>
              {opt.label}
            </option>
          ))}
        </select>

        <select
          className="webhooks-page__select"
          value={filters.processed}
          onChange={(e) => setProcessed(e.target.value)}
          aria-label="Filter by processed status"
        >
          <option value="">All</option>
          <option value="true">Processed</option>
          <option value="false">Pending / Failed</option>
        </select>

        <label className="webhooks-page__date-label">
          Filter by start date
          <input
            type="date"
            className="webhooks-page__date-input"
            value={filters.startDate}
            onChange={(e) => setStartDate(e.target.value)}
          />
        </label>
        <label className="webhooks-page__date-label">
          Filter by end date
          <input
            type="date"
            className="webhooks-page__date-input"
            value={filters.endDate}
            onChange={(e) => setEndDate(e.target.value)}
          />
        </label>
        {hasActiveFilters && (
          <button
            type="button"
            className="webhooks-page__button"
            onClick={resetFilters}
            aria-label="Reset filters"
          >
            Reset Filters
          </button>
        )}
      </div>

      {/* Content */}
      {isLoading ? (
        <div className="webhooks-page__loading" role="status">
          Loading webhooks...
        </div>
      ) : error ? (
        <div className="webhooks-page__error">
          <AlertCircle size={20} />
          <span>Failed to load webhooks. Please try again.</span>
        </div>
      ) : isEmpty ? (
        <div className="webhooks-page__empty">
          <Webhook aria-hidden="true" />
          <span>
            No webhook events found
            {hasActiveFilters ? '. Try adjusting your filters.' : '.'}
          </span>
        </div>
      ) : (
        <>
          <div className="webhooks-page__table-wrapper">
            <table className="webhooks-page__table">
              <thead>
                <tr>
                  <th>Event Type</th>
                  <th>Event ID</th>
                  <th>Status</th>
                  <th>Attempts</th>
                  <th>Error</th>
                  <th>Created</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                {events.map((event) => (
                  <tr
                    key={event.id}
                    className="webhooks-page__row--clickable"
                    onClick={() => setSelectedWebhookId(event.id ?? null)}
                  >
                    <td className="webhooks-page__event-type">{event.event_type}</td>
                    <td className="webhooks-page__event-id">{event.event_id}</td>
                    <td>
                      <span
                        className={`webhooks-page__status-badge webhooks-page__status-badge--${event.processed ? 'processed' : event.processing_error ? 'failed' : 'pending'}`}
                      >
                        {event.processed
                          ? 'Processed'
                          : event.processing_error
                            ? 'Failed'
                            : 'Pending'}
                      </span>
                    </td>
                    <td>{event.processing_attempts}</td>
                    <td className="webhooks-page__error-preview">
                      {event.processing_error
                        ? event.processing_error.substring(0, 50) +
                          (event.processing_error.length > 50 ? '...' : '')
                        : '—'}
                    </td>
                    <td>
                      {event.created_at ? new Date(event.created_at).toLocaleString() : '—'}
                    </td>
                    <td>
                      <button
                        type="button"
                        className="webhooks-page__replay-btn"
                        onClick={(e) => {
                          e.stopPropagation()
                          replayMutation.mutate({ id: event.id ?? '' })
                        }}
                        disabled={replayMutation.isPending}
                        aria-label={`Replay webhook ${event.event_id}`}
                      >
                        Replay
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {totalPages > 1 && (
            <div className="webhooks-page__pagination">
              <span>
                Page {currentPage} of {totalPages}
              </span>
              <div>
                <button
                  type="button"
                  onClick={() => setPage(currentPage - 1)}
                  disabled={currentPage <= 1}
                  aria-label="Previous page"
                >
                  <ChevronLeft size={16} /> Previous
                </button>
                <button
                  type="button"
                  onClick={() => setPage(currentPage + 1)}
                  disabled={!hasMore}
                  aria-label="Next page"
                >
                  Next <ChevronRight size={16} />
                </button>
              </div>
            </div>
          )}
        </>
      )}

      <WebhookDetailModal
        webhookId={selectedWebhookId}
        onClose={() => setSelectedWebhookId(null)}
      />
    </div>
  )
}
