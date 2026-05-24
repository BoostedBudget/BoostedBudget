/**
 * WebhooksPage Component
 * Issue #747: Paddle Webhook Admin UI
 *
 * Admin page for monitoring and replaying Paddle webhook events.
 */

import { useState, useEffect } from 'react'
import { useTrackEvent } from '../../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../../shared/analytics'
import { RefreshCw, AlertCircle, Webhook } from 'lucide-react'
import {
  useListAdminPaddleWebhooks,
  useGetAdminPaddleWebhookStats,
  useReplayAdminPaddleWebhook,
} from '@boostedbudget/api-client/endpoints/admin/admin'
import { SummaryCard } from '@/features/admin'
import { cn } from '@/lib/utils'
import { useWebhookFilters } from '../hooks/useWebhookFilters'
import { WebhookDetailModal } from './WebhookDetailModal'
import { DataTable, TablePagination, useTablePreferences } from '@/shared/components/DataTable'
import type { ColumnDef } from '@/shared/components/DataTable'

const EVENT_TYPE_OPTIONS = [
  { value: '', label: 'All Event Types' },
  { value: 'subscription.created', label: 'subscription.created' },
  { value: 'subscription.activated', label: 'subscription.activated' },
  { value: 'subscription.updated', label: 'subscription.updated' },
  { value: 'subscription.canceled', label: 'subscription.canceled' },
  { value: 'subscription.past_due', label: 'subscription.past_due' },
  { value: 'subscription.resumed', label: 'subscription.resumed' },
  { value: 'subscription.trialing', label: 'subscription.trialing' },
  { value: 'transaction.completed', label: 'transaction.completed' },
  { value: 'transaction.payment_failed', label: 'transaction.payment_failed' },
]

const statusBadgeClass = (processed: boolean, hasError: boolean): string => {
  if (processed) return 'bg-success/10 text-success'
  if (hasError) return 'bg-destructive/10 text-destructive'
  return 'bg-warning/10 text-warning'
}

const selectInputClass = 'px-3 py-2 border border-border rounded-md bg-background text-foreground text-sm font-[inherit] cursor-pointer transition-colors focus:outline-none focus:border-primary'

// eslint-disable-next-line react-refresh/only-export-components
export const ADMIN_WEBHOOKS_COLUMN_IDS = ['event_type', 'event_id', 'status', 'attempts', 'error', 'created_at'] as const

export function WebhooksPage() {
  const [autoRefresh, setAutoRefresh] = useState(true)
  const [selectedWebhookId, setSelectedWebhookId] = useState<string | null>(null)
  const trackEvent = useTrackEvent()

  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.ADMIN_PAGE_VIEWED, { page: 'webhooks' })
  }, [trackEvent])

  const { filters, setEventType, setProcessed, setStartDate, setEndDate, setPage, hasActiveFilters, resetFilters, sort, setSort, apiParams } = useWebhookFilters()

  const { data, isLoading, error } = useListAdminPaddleWebhooks(apiParams, {
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

  const { sizing, onSizingChange, columnVisibility, onColumnVisibilityChange } = useTablePreferences(
    'admin-webhooks',
    [...ADMIN_WEBHOOKS_COLUMN_IDS]
  )

  type WebhookEvent = typeof events[0]

  const columns: ColumnDef<WebhookEvent>[] = [
    {
      id: 'event_type',
      header: 'Event Type',
      resizable: true,
      defaultSize: 180,
      minSize: 120,
      render: (event) => (
        <span className="font-mono text-[0.8125rem] font-medium">{event.event_type}</span>
      ),
    },
    {
      id: 'event_id',
      header: 'Event ID',
      resizable: true,
      hideable: true,
      defaultSize: 220,
      minSize: 160,
      render: (event) => (
        <span className="font-mono text-[0.8125rem] text-muted-foreground max-w-[180px] overflow-hidden text-ellipsis whitespace-nowrap block">
          {event.event_id}
        </span>
      ),
    },
    {
      id: 'status',
      header: 'Status',
      sortable: true,
      defaultSize: 110,
      minSize: 90,
      maxSize: 140,
      render: (event) => (
        <span
          className={cn(
            'inline-flex items-center px-2 py-0.5 rounded text-xs font-medium',
            statusBadgeClass(!!event.processed, !!event.processing_error)
          )}
        >
          {event.processed
            ? 'Processed'
            : event.processing_error
              ? 'Failed'
              : 'Pending'}
        </span>
      ),
    },
    {
      id: 'attempts',
      header: 'Attempts',
      sortable: true,
      hideable: true,
      numeric: true,
      defaultSize: 100,
      minSize: 80,
      maxSize: 130,
      key: 'processing_attempts',
    },
    {
      id: 'error',
      header: 'Error',
      resizable: true,
      hideable: true,
      defaultSize: 280,
      minSize: 180,
      render: (event) => (
        <span className="text-[0.8125rem] text-destructive max-w-[200px] overflow-hidden text-ellipsis whitespace-nowrap block">
          {event.processing_error
            ? event.processing_error.substring(0, 50) +
              (event.processing_error.length > 50 ? '...' : '')
            : '—'}
        </span>
      ),
    },
    {
      id: 'created_at',
      header: 'Created',
      sortable: true,
      resizable: true,
      defaultSize: 180,
      minSize: 140,
      maxSize: 220,
      render: (event) => event.created_at ? new Date(event.created_at).toLocaleString() : '—',
    },
  ]

  return (
    <div className="flex flex-col gap-6 p-6 min-h-full">
      {/* Header */}
      <header className="flex items-start justify-between flex-wrap gap-4 border-b border-border pb-4">
        <div>
          <h1 className="text-2xl font-bold text-foreground flex items-center gap-2.5 m-0">
            Paddle Webhooks
            {total > 0 && (
              <span className="text-sm font-medium text-muted-foreground bg-muted border border-border rounded-full px-2.5 py-0.5">
                {total} events
              </span>
            )}
          </h1>
          <p className="text-sm text-muted-foreground mt-1.5 mb-0">Monitor and replay Paddle webhook events.</p>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <button
            type="button"
            className="inline-flex items-center gap-1.5 px-3.5 py-2 border border-border rounded-md bg-background text-foreground text-sm font-medium cursor-pointer font-[inherit] transition-colors hover:bg-muted"
            onClick={() => setAutoRefresh((v) => !v)}
          >
            <RefreshCw size={16} className={cn(autoRefresh && 'animate-spin')} />
            Auto-refresh: {autoRefresh ? 'ON' : 'OFF'}
          </button>
        </div>
      </header>

      {/* Stats Cards */}
      {statsData && (
        <div className="grid gap-4 [grid-template-columns:repeat(auto-fit,minmax(180px,1fr))]">
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
      <div className="flex items-center gap-3 flex-wrap">
        <select
          className={selectInputClass}
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
          className={selectInputClass}
          value={filters.processed}
          onChange={(e) => setProcessed(e.target.value)}
          aria-label="Filter by processed status"
        >
          <option value="">All</option>
          <option value="true">Processed</option>
          <option value="false">Pending / Failed</option>
        </select>

        <label className="inline-flex flex-col gap-1 text-xs text-muted-foreground font-medium">
          Filter by start date
          <input
            type="date"
            className={selectInputClass}
            value={filters.startDate}
            onChange={(e) => setStartDate(e.target.value)}
          />
        </label>
        <label className="inline-flex flex-col gap-1 text-xs text-muted-foreground font-medium">
          Filter by end date
          <input
            type="date"
            className={selectInputClass}
            value={filters.endDate}
            onChange={(e) => setEndDate(e.target.value)}
          />
        </label>
        {hasActiveFilters && (
          <button
            type="button"
            className="inline-flex items-center gap-1.5 px-3.5 py-2 border border-border rounded-md bg-background text-foreground text-sm font-medium cursor-pointer font-[inherit] transition-colors hover:bg-muted"
            onClick={resetFilters}
            aria-label="Reset filters"
          >
            Reset Filters
          </button>
        )}
      </div>

      {/* Content */}
      {isLoading ? (
        <div className="flex flex-col items-center justify-center gap-3 py-16 text-muted-foreground" role="status">
          Loading webhooks...
        </div>
      ) : error ? (
        <div className="flex items-center gap-3 py-16 text-destructive justify-center">
          <AlertCircle size={20} />
          <span>Failed to load webhooks. Please try again.</span>
        </div>
      ) : isEmpty ? (
        <div className="flex flex-col items-center justify-center gap-3 py-16 text-muted-foreground">
          <Webhook aria-hidden="true" />
          <span>
            No webhook events found
            {hasActiveFilters ? '. Try adjusting your filters.' : '.'}
          </span>
        </div>
      ) : (
        <>
          <DataTable
            data={events}
            columns={columns}
            rowKey={(e) => e.id ?? ''}
            sort={sort}
            onSortChange={setSort}
            filters={{}}
            onFiltersChange={() => {}}
            loading={isLoading}
            onRowClick={(event) => setSelectedWebhookId(event.id ?? null)}
            maskedColumnIds={['event_id', 'error']}
            tableId="admin-webhooks"
            columnSizing={sizing}
            onColumnSizingChange={onSizingChange}
            columnVisibility={columnVisibility}
            onColumnVisibilityChange={onColumnVisibilityChange}
            rowActions={(event) => (
              <button
                type="button"
                className="inline-flex items-center px-2.5 py-1.5 border border-border rounded-md bg-background text-foreground text-[0.8125rem] font-medium cursor-pointer font-[inherit] transition-colors hover:bg-primary hover:border-primary hover:text-primary-foreground disabled:opacity-45 disabled:cursor-not-allowed"
                onClick={() => replayMutation.mutate({ id: event.id ?? '' })}
                disabled={replayMutation.isPending}
                aria-label={`Replay webhook ${event.event_id}`}
              >
                Replay
              </button>
            )}
          />
          <TablePagination
            currentPage={currentPage}
            totalPages={totalPages}
            hasMore={hasMore}
            onPageChange={setPage}
          />
        </>
      )}

      <WebhookDetailModal
        webhookId={selectedWebhookId}
        onClose={() => setSelectedWebhookId(null)}
      />
    </div>
  )
}
