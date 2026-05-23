import { useState, useEffect } from 'react'
import { useTrackEvent } from '../../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../../shared/analytics'
import { RefreshCw, AlertCircle, AlertTriangle } from 'lucide-react'
import {
  useListAdminDeadLetterJobs,
  useGetAdminDeadLetterJobStats,
  useRetryAdminDeadLetterJob,
  useDeleteAdminDeadLetterJob,
  useBulkDeleteAdminDeadLetterJobs,
} from '@boostedbudget/api-client/endpoints/admin/admin'
import { SummaryCard } from '@/features/admin'
import { cn } from '@/lib/utils'
import { ConfirmationDialog } from '@/components/ui/confirmation-dialog'
import { useDeadLetterFilters } from '../hooks/useDeadLetterFilters'
import { DeadLetterDetailModal } from './DeadLetterDetailModal'
import { DataTable, TablePagination, useTablePreferences } from '@/shared/components/DataTable'
import type { ColumnDef } from '@/shared/components/DataTable'

const PAGE_SIZE = 20

// eslint-disable-next-line react-refresh/only-export-components
export const ADMIN_DLQ_COLUMN_IDS = ['kind', 'queue', 'user_id', 'failed_at', 'status', 'notes'] as const

const inputClass = 'px-3 py-2 border border-border rounded-md bg-background text-foreground text-sm focus:outline-none focus:border-primary'
const selectInputClass = `${inputClass} cursor-pointer transition-colors`
const buttonClass = 'inline-flex items-center px-3.5 py-2 border border-border rounded-md bg-background text-foreground text-sm font-medium cursor-pointer transition-colors hover:bg-muted disabled:opacity-45 disabled:cursor-not-allowed'

export function DeadLetterPage() {
  const [autoRefresh, setAutoRefresh] = useState(true)
  const [selectedJobId, setSelectedJobId] = useState<number | null>(null)
  const [confirmBulkDelete, setConfirmBulkDelete] = useState(false)
  const [confirmDeleteJobId, setConfirmDeleteJobId] = useState<number | null>(null)
  const trackEvent = useTrackEvent()

  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.ADMIN_PAGE_VIEWED, { page: 'dead-letter' })
  }, [trackEvent])

  const {
    filters,
    setKind,
    setUserId,
    setRetried,
    setStartDate,
    setEndDate,
    setPage,
    hasActiveFilters,
    resetFilters,
  } = useDeadLetterFilters()

  const listParams = {
    page: filters.page,
    page_size: PAGE_SIZE,
    ...(filters.kind && { kind: filters.kind }),
    ...(filters.userId && { user_id: filters.userId }),
    ...(filters.retried !== '' && { retried: filters.retried }),
    ...(filters.startDate && { start_date: filters.startDate }),
    ...(filters.endDate && { end_date: filters.endDate }),
  }

  const { data, isLoading, error } = useListAdminDeadLetterJobs(listParams, {
    query: { refetchInterval: autoRefresh ? 30000 : false },
  })

  const { data: statsData } = useGetAdminDeadLetterJobStats({
    query: { refetchInterval: autoRefresh ? 30000 : false },
  })

  const retryMutation = useRetryAdminDeadLetterJob()
  const deleteMutation = useDeleteAdminDeadLetterJob()
  const bulkDeleteMutation = useBulkDeleteAdminDeadLetterJobs()

  const jobs = data?.jobs ?? []
  const total = data?.total ?? 0
  const currentPage = data?.page ?? 1
  const totalPages = data?.total_pages ?? 0
  const hasMore = data?.has_more ?? false
  const isEmpty = !isLoading && !error && jobs.length === 0

  // Populate kind dropdown from stats
  const kindOptions = statsData?.by_kind ? Object.keys(statsData.by_kind) : []

  const { sizing, onSizingChange, columnVisibility, onColumnVisibilityChange } = useTablePreferences(
    'admin-dead-letter',
    [...ADMIN_DLQ_COLUMN_IDS]
  )

  type DeadLetterJob = typeof jobs[0]

  const columns: ColumnDef<DeadLetterJob>[] = [
    {
      id: 'kind',
      header: 'Kind',
      resizable: true,
      defaultSize: 180,
      minSize: 120,
      render: (job) => (
        <span className="font-mono text-[0.8125rem] font-medium">{job.kind}</span>
      ),
    },
    {
      id: 'queue',
      header: 'Queue',
      hideable: true,
      defaultSize: 140,
      minSize: 100,
      maxSize: 200,
      render: (job) => (
        <span className="font-mono text-[0.8125rem] text-muted-foreground">{job.queue}</span>
      ),
    },
    {
      id: 'user_id',
      header: 'User ID',
      resizable: true,
      defaultSize: 220,
      minSize: 160,
      render: (job) => (
        <span className="font-mono text-[0.8125rem] text-muted-foreground max-w-[140px] overflow-hidden text-ellipsis whitespace-nowrap block">
          {job.user_id ?? '—'}
        </span>
      ),
    },
    {
      id: 'failed_at',
      header: 'Failed At',
      defaultSize: 180,
      minSize: 140,
      maxSize: 220,
      render: (job) => (
        <span className="text-[0.8125rem] whitespace-nowrap">
          {new Date(job.failed_at).toLocaleString()}
        </span>
      ),
    },
    {
      id: 'status',
      header: 'Status',
      defaultSize: 120,
      minSize: 90,
      maxSize: 160,
      render: (job) => (
        <span
          className={cn(
            'inline-flex items-center px-2 py-0.5 rounded text-xs font-medium',
            job.retried_at
              ? 'bg-success/10 text-success'
              : 'bg-destructive/10 text-destructive'
          )}
        >
          {job.retried_at ? 'Retried' : 'Pending'}
        </span>
      ),
    },
    {
      id: 'notes',
      header: 'Notes',
      resizable: true,
      hideable: true,
      defaultSize: 280,
      minSize: 180,
      render: (job) => (
        <span className="text-[0.8125rem] text-muted-foreground max-w-[160px] overflow-hidden text-ellipsis whitespace-nowrap block">
          {job.notes
            ? job.notes.substring(0, 40) + (job.notes.length > 40 ? '...' : '')
            : '—'}
        </span>
      ),
    },
  ]

  const handleRetry = (jobId: number) => {
    retryMutation.mutate({ id: jobId }, {
      onSuccess: () => trackEvent(ANALYTICS_EVENTS.ADMIN_DLQ_RETRY, { job_id: jobId }),
    })
  }

  const handleDeleteConfirm = (jobId: number) => {
    deleteMutation.mutate({ id: jobId }, {
      onSuccess: () => {
        trackEvent(ANALYTICS_EVENTS.ADMIN_DLQ_DELETE, { job_id: jobId })
        setConfirmDeleteJobId(null)
      },
    })
  }

  const handleBulkDelete = () => {
    const filters_payload = {
      ...(filters.kind && { kind: filters.kind }),
      ...(filters.userId && { user_id: filters.userId }),
      ...(filters.retried !== '' && { retried: filters.retried === 'true' }),
      ...(filters.startDate && { start_date: filters.startDate }),
      ...(filters.endDate && { end_date: filters.endDate }),
    }
    bulkDeleteMutation.mutate({ data: filters_payload }, {
      onSuccess: (result) => {
        trackEvent(ANALYTICS_EVENTS.ADMIN_DLQ_BULK_DELETE, { deleted: result?.deleted })
        setConfirmBulkDelete(false)
        resetFilters()
      },
    })
  }

  return (
    <div className="flex flex-col gap-6 p-6 min-h-full">
      <header className="flex items-start justify-between flex-wrap gap-4 border-b border-border pb-4">
        <div>
          <h1 className="text-2xl font-bold text-foreground flex items-center gap-2.5 m-0">
            <AlertTriangle size={22} className="text-destructive" aria-hidden="true" />
            Dead Letter Queue
            {total > 0 && (
              <span className="text-sm font-medium text-muted-foreground bg-muted border border-border rounded-full px-2.5 py-0.5">
                {total} jobs
              </span>
            )}
          </h1>
          <p className="text-sm text-muted-foreground mt-1.5 mb-0">
            Failed jobs that exhausted all retry attempts. Retry or delete entries as needed.
          </p>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <button
            type="button"
            className={cn(buttonClass, 'gap-1.5')}
            onClick={() => setAutoRefresh((v) => !v)}
          >
            <RefreshCw size={16} className={cn(autoRefresh && 'animate-spin')} />
            Auto-refresh: {autoRefresh ? 'ON' : 'OFF'}
          </button>
        </div>
      </header>

      {statsData && (
        <div className="grid gap-4 [grid-template-columns:repeat(auto-fit,minmax(180px,1fr))]">
          <SummaryCard
            title="Total"
            value={statsData.total.toString()}
            icon={<AlertTriangle size={24} />}
            status={statsData.total > 0 ? 'warning' : 'success'}
          />
          <SummaryCard
            title="Unretried"
            value={(statsData.total - statsData.retried).toString()}
            icon={<AlertCircle size={24} />}
            status={(statsData.total - statsData.retried) > 0 ? 'warning' : 'success'}
          />
          <SummaryCard
            title="Retried"
            value={statsData.retried.toString()}
            icon={<RefreshCw size={24} />}
            status="success"
          />
          <SummaryCard
            title="Last 24h"
            value={statsData.last_24_hours.toString()}
            icon={<AlertTriangle size={24} />}
            status={statsData.last_24_hours > 0 ? 'warning' : 'success'}
          />
        </div>
      )}

      {statsData?.by_kind && Object.keys(statsData.by_kind).length > 0 && (
        <div className="flex flex-wrap gap-2">
          {Object.entries(statsData.by_kind).map(([kind, count]) => (
            <span
              key={kind}
              className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full bg-muted border border-border text-xs font-medium text-foreground cursor-pointer hover:bg-muted/80 transition-colors"
              onClick={() => setKind(kind === filters.kind ? '' : kind)}
              role="button"
              tabIndex={0}
              onKeyDown={(e) => e.key === 'Enter' && setKind(kind === filters.kind ? '' : kind)}
              aria-label={`Filter by ${kind}: ${count} jobs`}
            >
              <span className="font-mono">{kind}</span>
              <span className="text-muted-foreground">{count}</span>
            </span>
          ))}
        </div>
      )}

      <div className="flex items-center gap-3 flex-wrap">
        <select
          className={selectInputClass}
          value={filters.kind}
          onChange={(e) => setKind(e.target.value)}
          aria-label="Filter by job kind"
        >
          <option value="">All Kinds</option>
          {kindOptions.map((k) => (
            <option key={k} value={k}>{k}</option>
          ))}
        </select>

        <input
          type="text"
          className={inputClass}
          value={filters.userId}
          onChange={(e) => setUserId(e.target.value)}
          placeholder="User ID"
          aria-label="Filter by user ID"
        />

        <select
          className={selectInputClass}
          value={filters.retried}
          onChange={(e) => setRetried(e.target.value)}
          aria-label="Filter by retried status"
        >
          <option value="">All</option>
          <option value="false">Not Retried</option>
          <option value="true">Retried</option>
        </select>

        <label className="inline-flex flex-col gap-1 text-xs text-muted-foreground font-medium">
          Filter by start date
          <input
            type="date"
            className={inputClass}
            value={filters.startDate}
            onChange={(e) => setStartDate(e.target.value)}
          />
        </label>
        <label className="inline-flex flex-col gap-1 text-xs text-muted-foreground font-medium">
          Filter by end date
          <input
            type="date"
            className={inputClass}
            value={filters.endDate}
            onChange={(e) => setEndDate(e.target.value)}
          />
        </label>
        {hasActiveFilters && (
          <button
            type="button"
            className={cn(buttonClass, 'gap-1.5')}
            onClick={resetFilters}
            aria-label="Reset filters"
          >
            Reset Filters
          </button>
        )}
      </div>

      {hasActiveFilters && !isEmpty && (
        <div className="flex items-center gap-3">
          <button
            type="button"
            className={cn(buttonClass, 'gap-1.5 border-destructive/50 text-destructive hover:bg-destructive/10')}
            onClick={() => setConfirmBulkDelete(true)}
            aria-label="Delete all matching jobs"
          >
            Delete All Matching
          </button>
        </div>
      )}

      {isLoading ? (
        <div className="flex flex-col items-center justify-center gap-3 py-16 text-muted-foreground" role="status">
          Loading dead letter jobs...
        </div>
      ) : error ? (
        <div className="flex items-center gap-3 py-16 text-destructive justify-center">
          <AlertCircle size={20} />
          <span>Failed to load dead letter jobs. Please try again.</span>
        </div>
      ) : isEmpty ? (
        <div className="flex flex-col items-center justify-center gap-3 py-16 text-muted-foreground">
          <AlertTriangle aria-hidden="true" />
          <span>
            No dead letter jobs found
            {hasActiveFilters ? '. Try adjusting your filters.' : '.'}
          </span>
        </div>
      ) : (
        <>
          <DataTable
            data={jobs}
            columns={columns}
            rowKey={(job) => String(job.id)}
            sort={null}
            onSortChange={() => {}}
            filters={{}}
            onFiltersChange={() => {}}
            loading={isLoading}
            onRowClick={(job) => setSelectedJobId(job.id)}
            maskedColumnIds={['user_id', 'notes']}
            tableId="admin-dead-letter"
            columnSizing={sizing}
            onColumnSizingChange={onSizingChange}
            columnVisibility={columnVisibility}
            onColumnVisibilityChange={onColumnVisibilityChange}
            rowActions={(job) => (
              <div className="flex items-center gap-1.5">
                <button
                  type="button"
                  className="inline-flex items-center px-2.5 py-1.5 border border-border rounded-md bg-background text-foreground text-[0.8125rem] font-medium cursor-pointer transition-colors hover:bg-primary hover:border-primary hover:text-primary-foreground disabled:opacity-45 disabled:cursor-not-allowed"
                  onClick={() => handleRetry(job.id)}
                  disabled={!!job.retried_at || retryMutation.isPending}
                  aria-label={`Retry job ${job.id}`}
                >
                  Retry
                </button>
                <button
                  type="button"
                  className="inline-flex items-center px-2.5 py-1.5 border border-destructive/30 rounded-md bg-background text-destructive text-[0.8125rem] font-medium cursor-pointer transition-colors hover:bg-destructive/10 disabled:opacity-45 disabled:cursor-not-allowed"
                  onClick={() => setConfirmDeleteJobId(job.id)}
                  disabled={deleteMutation.isPending}
                  aria-label={`Delete job ${job.id}`}
                >
                  Delete
                </button>
              </div>
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

      <DeadLetterDetailModal
        jobId={selectedJobId}
        onClose={() => setSelectedJobId(null)}
      />

      <ConfirmationDialog
        open={confirmBulkDelete}
        onOpenChange={setConfirmBulkDelete}
        onConfirm={handleBulkDelete}
        title="Delete All Matching Jobs"
        description="This will permanently delete all jobs matching the current filters. This action cannot be undone."
        variant="destructive"
        loading={bulkDeleteMutation.isPending}
      />

      <ConfirmationDialog
        open={confirmDeleteJobId !== null}
        onOpenChange={(open) => !open && setConfirmDeleteJobId(null)}
        onConfirm={() => { if (confirmDeleteJobId !== null) handleDeleteConfirm(confirmDeleteJobId) }}
        title="Delete Job"
        description={confirmDeleteJobId !== null ? `Delete job #${confirmDeleteJobId}? This action cannot be undone.` : ''}
        variant="destructive"
        loading={deleteMutation.isPending}
      />
    </div>
  )
}
