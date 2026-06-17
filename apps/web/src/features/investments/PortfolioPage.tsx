import { useEffect } from 'react'
import { BarChart3 } from 'lucide-react'
import { EmptyState } from '@/components/ui/empty-state'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import { useGetPortfolioSummary } from '@boostedbudget/api-client/endpoints/investments/investments'
import { PlaidLinkButton } from '@/features/accounts/PlaidLinkButton'
import { useActiveAccounts } from '@/features/accounts/hooks/useActiveAccounts'
import { NetWorthSection } from '@/features/net-worth/components/NetWorthSection'
import { PortfolioSummaryCard } from './components/PortfolioSummaryCard'
import { PortfolioTimeRange } from './components/PortfolioTimeRange'
import { PortfolioValueChart } from './components/PortfolioValueChart'
import { AllocationCard } from './components/AllocationCard'
import { HoldingsTable } from './components/HoldingsTable'
import { InvestmentTransactionsTable } from './components/InvestmentTransactionsTable'
import { usePortfolioTimeRange } from './hooks/usePortfolioTimeRange'
import { usePortfolioAllocationConfig } from './hooks/usePortfolioAllocationConfig'

export function PortfolioPage() {
  const trackEvent = useTrackEvent()

  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.PORTFOLIO_PAGE_VIEWED)
  }, [trackEvent])

  const { preset, setPreset, dateRange } = usePortfolioTimeRange()
  const { config, setConfig } = usePortfolioAllocationConfig()

  const { data: summaryData, isLoading: summaryLoading, isError: summaryError } =
    useGetPortfolioSummary()

  const { accounts } = useActiveAccounts()

  if (summaryLoading) {
    return (
      <main className="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
        <div role="status" aria-label="Loading portfolio" className="flex justify-center py-16">
          <div className="animate-spin rounded-full size-8 border-b-2 border-primary" />
        </div>
      </main>
    )
  }

  if (summaryError) {
    return (
      <main className="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
        <p role="alert" className="text-sm text-destructive">
          Unable to load portfolio data. Please refresh the page.
        </p>
      </main>
    )
  }

  const holdingsCount = summaryData?.holdings_count ?? 0
  // Show empty state only when accounts have loaded (not undefined) AND none are investment type.
  // While accounts === undefined (still loading), fall through to sections to avoid a flash of the CTA.
  const hasInvestmentAccount = accounts?.some((a) => a.type === 'investment') ?? false
  if (holdingsCount === 0 && accounts !== undefined && !hasInvestmentAccount) {
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

  return (
    <main className="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8 space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Portfolio</h1>
        <p className="text-sm text-muted-foreground">
          Track your investment performance, allocation, and transactions.
        </p>
      </div>

      <PortfolioSummaryCard summary={summaryData} />

      <PortfolioTimeRange
        activePreset={preset}
        onPresetChange={(next) => {
          setPreset(next)
          trackEvent(ANALYTICS_EVENTS.PORTFOLIO_RANGE_CHANGED, { preset: next })
        }}
      />

      <NetWorthSection title="Portfolio Value">
        <PortfolioValueChart dateRange={dateRange} />
      </NetWorthSection>

      <AllocationCard config={config} onConfigChange={setConfig} />

      <NetWorthSection title="Holdings">
        <HoldingsTable />
      </NetWorthSection>

      <NetWorthSection title="Transactions">
        <InvestmentTransactionsTable dateRange={dateRange} />
      </NetWorthSection>
    </main>
  )
}
