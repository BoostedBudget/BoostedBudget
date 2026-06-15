/**
 * PortfolioPage — Portfolio sub-tab on InsightsPage.
 * Issue #1296: Holistic portfolio view with configurable allocation,
 * value chart, holdings table, and investment transactions.
 *
 * Rendered inside InsightsPage's TabsContent — the wrapping container
 * suppresses the h1 via `[&_h1]:hidden`.
 */

import { useEffect } from 'react'
import { BarChart3 } from 'lucide-react'
import { EmptyState } from '@/components/ui/empty-state'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import {
  useGetPortfolioSummary,
  useGetPortfolioHistory,
  useGetHoldings,
} from '@boostedbudget/api-client/endpoints/investments/investments'
import { useGetPortfolioAllocationBreakdown } from '@boostedbudget/api-client/endpoints/analytics/analytics'
import { PlaidLinkButton } from '@/features/accounts/PlaidLinkButton'
import { NetWorthSection } from '@/features/net-worth/components/NetWorthSection'
import { PortfolioSummaryCard } from './components/PortfolioSummaryCard'
import { PortfolioTimeRange } from './components/PortfolioTimeRange'
import { usePortfolioTimeRange } from './hooks/usePortfolioTimeRange'
import { usePortfolioAllocationConfig } from './hooks/usePortfolioAllocationConfig'
import { useInfiniteInvestmentTransactions } from './hooks/useInfiniteInvestmentTransactions'
import type { AllocationConfig } from './hooks/usePortfolioAllocationConfig'

export function PortfolioPage() {
  const trackEvent = useTrackEvent()

  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.PORTFOLIO_PAGE_VIEWED)
  }, [trackEvent])

  const { preset, setPreset, dateRange } = usePortfolioTimeRange()
  const { config, setConfig } = usePortfolioAllocationConfig()

  // Portfolio summary (KPIs)
  const { data: summaryData, isLoading: summaryLoading, isError: summaryError } =
    useGetPortfolioSummary()

  // Portfolio history (value chart)
  const { data: historyData, isLoading: historyLoading } = useGetPortfolioHistory({
    start_date: dateRange.start_date,
    end_date: dateRange.end_date,
  })

  // Allocation breakdown (donut)
  const { data: allocationData, isLoading: allocationLoading } = useGetPortfolioAllocationBreakdown({
    include: config.include.join(',') || undefined,
    group_by: config.group_by,
    basis: config.basis,
  })

  // Holdings table
  const { data: holdingsData, isLoading: holdingsLoading } = useGetHoldings()

  // Investment transactions table (infinite scroll)
  const {
    transactions,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
    isLoading: txnLoading,
  } = useInfiniteInvestmentTransactions({
    start_date: dateRange.start_date,
    end_date: dateRange.end_date,
  })

  // ── Loading state ─────────────────────────────────────────────────────────
  if (summaryLoading) {
    return (
      <main className="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
        <div role="status" aria-label="Loading portfolio" className="flex justify-center py-16">
          <div className="animate-spin rounded-full size-8 border-b-2 border-primary" />
        </div>
      </main>
    )
  }

  // ── Error state ───────────────────────────────────────────────────────────
  if (summaryError) {
    return (
      <main className="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
        <p role="alert" className="text-sm text-destructive">
          Unable to load portfolio data. Please refresh the page.
        </p>
      </main>
    )
  }

  // ── Empty state — no investments connected ────────────────────────────────
  const holdingsCount = summaryData?.holdings_count ?? 0
  if (holdingsCount === 0) {
    return (
      <main className="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
        <EmptyState
          icon={<BarChart3 />}
          title="No portfolio data yet"
          description="Connect a brokerage account to start tracking your investments."
          action={<PlaidLinkButton />}
        />
      </main>
    )
  }

  // ── Portfolio content ─────────────────────────────────────────────────────
  const handlePresetChange = (next: typeof preset) => {
    setPreset(next)
    trackEvent(ANALYTICS_EVENTS.PORTFOLIO_RANGE_CHANGED, { preset: next })
  }

  // handleConfigChange — wired in Task C4 when AllocationCard is implemented
  const handleConfigChange = (next: AllocationConfig) => {
    setConfig(next)
    trackEvent(ANALYTICS_EVENTS.PORTFOLIO_ALLOCATION_CONFIG_CHANGED, {
      group_by: next.group_by,
      basis: next.basis,
      include_count: next.include.length,
    })
  }
  void handleConfigChange // suppress unused-variable lint until AllocationCard is wired

  return (
    <main className="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8 space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Portfolio</h1>
        <p className="text-sm text-muted-foreground">
          Track your investment performance, allocation, and transactions.
        </p>
      </div>

      {/* KPI Summary */}
      <PortfolioSummaryCard summary={summaryData} />

      {/* Time range selector */}
      <div className="flex items-center justify-between">
        <PortfolioTimeRange activePreset={preset} onPresetChange={handlePresetChange} />
      </div>

      {/* Value chart — placeholder; replaced in Task C4 */}
      <NetWorthSection title="Portfolio Value">
        {historyLoading ? (
          <div className="h-48 flex items-center justify-center">
            <div className="animate-spin rounded-full size-6 border-b-2 border-primary" />
          </div>
        ) : (
          <div
            data-testid="portfolio-value-chart"
            className="h-48 flex items-center justify-center text-muted-foreground text-sm"
          >
            {(historyData?.history?.length ?? 0) >= 2
              ? 'Chart renders here'
              : 'Not enough history for this range'}
          </div>
        )}
      </NetWorthSection>

      {/* Allocation — placeholder; replaced in Task C4 */}
      <NetWorthSection title="Allocation">
        {allocationLoading ? (
          <div className="h-48 flex items-center justify-center">
            <div className="animate-spin rounded-full size-6 border-b-2 border-primary" />
          </div>
        ) : (
          <div
            data-testid="portfolio-allocation"
            className="h-48 flex items-center justify-center text-muted-foreground text-sm"
          >
            {allocationData ? 'Allocation chart renders here' : 'No allocation data'}
          </div>
        )}
      </NetWorthSection>

      {/* Holdings table — placeholder; replaced in Task C5 */}
      <NetWorthSection title="Holdings">
        {holdingsLoading ? (
          <div className="h-32 flex items-center justify-center">
            <div className="animate-spin rounded-full size-6 border-b-2 border-primary" />
          </div>
        ) : (
          <div
            data-testid="holdings-table"
            className="text-muted-foreground text-sm"
          >
            {holdingsData ? `${holdingsData.holdings?.length ?? 0} holdings` : 'No holdings data'}
          </div>
        )}
      </NetWorthSection>

      {/* Investment transactions table — placeholder; replaced in Task C5 */}
      <NetWorthSection title="Transactions">
        {txnLoading ? (
          <div className="h-32 flex items-center justify-center">
            <div className="animate-spin rounded-full size-6 border-b-2 border-primary" />
          </div>
        ) : (
          <div
            data-testid="investment-transactions-table"
            className="text-muted-foreground text-sm"
          >
            {transactions.length} transactions
            {hasNextPage && (
              <button
                onClick={() => fetchNextPage()}
                disabled={isFetchingNextPage}
                className="ml-4 text-xs text-primary underline"
              >
                {isFetchingNextPage ? 'Loading…' : 'Load more'}
              </button>
            )}
          </div>
        )}
      </NetWorthSection>
    </main>
  )
}

