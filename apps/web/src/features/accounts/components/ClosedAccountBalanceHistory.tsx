/**
 * ClosedAccountBalanceHistory — Issue #1569.
 *
 * WHAT IT CLOSES. Privacy policy §8.2a promises that a removed linked account's "transaction,
 * balance, and holdings history is retained ... so you can still view it". The BALANCE third of
 * that sentence was false. balance_snapshots was written by every sync and the only per-account
 * read was the single last-known figure AccountSummaryHeader renders (#1591); getBalanceHistory
 * takes account_types and has never taken an account id, so it could not answer for one account
 * either. The day-by-day rows were reachable only through the GDPR export. This component is the
 * seat the user finally sits in front of, over
 * GET /api/v1/accounts/{id}/balance-history.
 *
 * ⚠ TWO UNIVERSES, AND THIS ONE IS THE HISTORICAL ONE. Every figure here is labelled with the UTC
 * day it was true on, and none of them is what the account holds now — a closed account has no
 * current balance at all (accountBalanceProjection sends NULL for it, deliberately). Nothing here
 * is routed through `balanceMatchesRange` / `compareBalanceValues`; that divergence was a defect
 * once already (see features/accounts/utils.ts).
 *
 * REUSE, NOT INVENTION. The chart is the dashboard's own BalanceHistoryChart — the one that
 * already renders a balance series — adapted by mapping the points onto its BalanceHistory shape.
 * It is loaded lazily rather than imported eagerly: it lives in the `dashboard` chunk and pulls
 * the 230 kB `charts` chunk behind it, and the accounts route must not pay either until a user
 * actually opens a closed account. The rows are the shared DataTable, matching
 * ClosedAccountHoldingsHistory next door on the same surface.
 *
 * ⚠ THE TABLE IS NOT DECORATION. ECharts paints to a canvas, so the chart is invisible to a screen
 * reader and to anyone who cannot distinguish the line from its background. The table is the
 * accessible rendering of the SAME numbers, which is also why no information here is carried by
 * colour: the word "Closed" states the state, the day states the date, and the badge only tints.
 *
 * ⚠ PII IS NOT AUTOMATIC. DataTable masks a <td> only when that column's id is listed in
 * `maskedColumnIds`, so both money columns are listed by id below. The chart's aria-label is
 * deliberately structural and carries NO amount — an attribute is recorded verbatim by session
 * replay and cannot be covered by any mask.
 */

import { Suspense, lazy, useEffect, useMemo, useState } from 'react'
import { useGetAccountBalanceHistory } from '@boostedbudget/api-client/endpoints/accounts/accounts'
import type { AccountBalanceHistoryPoint } from '@boostedbudget/api-client/models/accountBalanceHistoryPoint'
import { DataTable, useTablePreferences } from '@/shared/components/DataTable'
import type { ColumnDef, SortState } from '@/shared/components/DataTable'
import { formatCurrencyFull } from '@/shared/charts/formatters'
import { formatHistoryLabel } from '@/shared/dates/utils'
import { Badge } from '@/components/ui/badge'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'

// Lazily: see the chunking note in the file header. The named export is adapted to the default
// export React.lazy requires.
const BalanceHistoryChart = lazy(() =>
  import('@/features/dashboard/components/BalanceHistoryChart').then((m) => ({
    default: m.BalanceHistoryChart,
  }))
)

const COLUMN_IDS = ['date', 'current_balance', 'available_balance'] as const

// Every money column, by id. A column left off this list gets no `data-ph-mask` at all.
const MASKED_COLUMN_IDS = ['current_balance', 'available_balance']

// DataTable renders these through its own <td>, so the mask lands on the cell rather than on
// whatever the renderer happens to produce.
const COLUMNS: ColumnDef<AccountBalanceHistoryPoint>[] = [
  {
    id: 'date',
    header: 'Date',
    sortable: true,
    render: (row) => <span>{formatHistoryLabel(row.date, 'MMM d, yyyy')}</span>,
  },
  {
    id: 'current_balance',
    header: 'Balance',
    sortable: true,
    align: 'right',
    // An absent balance stays absent. Rendering 0 would claim the account held nothing on a day
    // the provider simply did not report.
    render: (row) => (
      <span className="tabular-nums">
        {row.current_balance == null ? '—' : formatCurrencyFull(row.current_balance)}
      </span>
    ),
  },
  {
    id: 'available_balance',
    header: 'Available',
    sortable: true,
    align: 'right',
    render: (row) => (
      <span className="tabular-nums">
        {row.available_balance == null ? '—' : formatCurrencyFull(row.available_balance)}
      </span>
    ),
  },
]

const NO_FILTERS = {}
const noopFiltersChange = () => {}

interface ClosedAccountBalanceHistoryProps {
  accountId: string
  /** A fixed discriminator on the analytics event — never free text, never an amount. */
  accountType?: string
  closedOn?: string
}

export function ClosedAccountBalanceHistory({
  accountId,
  accountType,
  closedOn,
}: ClosedAccountBalanceHistoryProps) {
  const trackEvent = useTrackEvent()
  const [sortState, setSortState] = useState<SortState>(null)

  const { data, isLoading, isError } = useGetAccountBalanceHistory(accountId)

  const {
    sizing,
    onSizingChange,
    columnOrder,
    onColumnOrderChange,
    columnVisibility,
    onColumnVisibilityChange,
  } = useTablePreferences('account-balance-history', [...COLUMN_IDS])

  const points = useMemo<AccountBalanceHistoryPoint[]>(() => data?.points ?? [], [data])

  // The VIEW measurement (#1569): did anyone actually go and look at the money we kept? Fired once
  // the read resolves, whether or not anything was retained — "the user opened it and we had
  // nothing" is the more interesting half of that question, not a case to drop.
  const resolved = !isLoading && !isError
  useEffect(() => {
    if (!resolved) return
    trackEvent(ANALYTICS_EVENTS.ACCOUNT_BALANCE_HISTORY_VIEWED, {
      account_type: accountType,
      // A count, not a record: no dates, no amounts, no account identity.
      retained_days: points.length,
    })
    // trackEvent is stable (useCallback with empty deps in useTrackEvent)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [resolved, accountId])

  // Mapped onto the dashboard chart's own shape rather than a second chart being written. The
  // chart draws current_balance only; a day the provider reported no balance for is skipped
  // rather than plotted as zero, which would draw a cliff that never happened.
  const chartHistory = useMemo(() => {
    const plotted = points.filter((p) => p.current_balance != null)
    return {
      start_date: plotted[0]?.date ?? '',
      end_date: plotted[plotted.length - 1]?.date ?? '',
      granularity: 'daily' as const,
      snapshots: plotted.map((p) => ({ date: p.date, balance: p.current_balance as number })),
    }
  }, [points])

  if (isLoading) {
    // Not PageSkeleton: that is a min-h-[60vh] whole-page spinner and this is one section of a
    // page that has already rendered its header. Matches ClosedAccountHoldingsHistory's inline
    // treatment on the same surface instead, motion-reduce included.
    return (
      <div
        role="status"
        aria-label="Loading retained balance history"
        className="flex justify-center py-8"
      >
        <div className="animate-spin rounded-full size-6 border-b-2 border-primary motion-reduce:animate-none" />
      </div>
    )
  }

  if (isError) {
    // A failed read must never fall through to the empty state: "we kept nothing" is a claim about
    // the user's money, and making it wrongly says the retention failed when it did not.
    return (
      <div role="alert" className="text-sm text-destructive p-4">
        We could not load this account&apos;s retained balance history. It is still kept — try
        again in a moment.
      </div>
    )
  }

  if (points.length === 0) {
    return (
      <div
        role="note"
        className="rounded-lg border border-border bg-muted/40 p-4 text-sm text-muted-foreground"
      >
        <p className="font-medium text-foreground m-0">
          No day-by-day balances were recorded for this account.
        </p>
        <p className="mt-1">
          We don&apos;t have retained balance history for this account. Nothing was deleted — its
          transactions are retained and still appear in your activity.
        </p>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <Badge variant="secondary" size="sm">
          Closed account
        </Badge>
        <p className="text-sm text-muted-foreground m-0">
          {/* Never colour alone: the word "Closed" carries the state, the badge only tints it. */}
          These are the balances we last recorded, not what the account holds today
          {closedOn ? ` — it closed on ${formatHistoryLabel(closedOn, 'MMM d, yyyy')}` : ''}.
        </p>
      </div>

      {chartHistory.snapshots.length > 1 && (
        <div
          role="img"
          /* Structural only. An amount in an attribute is recorded verbatim by session replay and
             cannot be masked, so the label points at the table instead of naming a figure. */
          aria-label="Chart of this account's retained balances over time. The same figures are listed in the table below."
          className="h-56"
        >
          <Suspense
            fallback={
              <div
                role="status"
                aria-label="Loading balance chart"
                className="h-full w-full rounded-lg bg-muted/40"
              />
            }
          >
            <BalanceHistoryChart history={chartHistory} />
          </Suspense>
        </div>
      )}

      <DataTable
        data={points}
        columns={COLUMNS}
        rowKey={(r) => r.date}
        sort={sortState}
        onSortChange={setSortState}
        // Nothing here is filterable: the table is already scoped to one account, and every row is
        // one retained day.
        filters={NO_FILTERS}
        onFiltersChange={noopFiltersChange}
        maskedColumnIds={MASKED_COLUMN_IDS}
        emptyState={
          <p className="text-sm text-muted-foreground p-4">
            No balances were recorded for this account.
          </p>
        }
        columnSizing={sizing}
        onColumnSizingChange={onSizingChange}
        columnOrder={columnOrder}
        onColumnOrderChange={onColumnOrderChange}
        columnVisibility={columnVisibility}
        onColumnVisibilityChange={onColumnVisibilityChange}
      />
    </div>
  )
}
