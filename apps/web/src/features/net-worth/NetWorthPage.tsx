import { useEffect } from 'react'
import { TrendingUp } from 'lucide-react'
import { DateRangePicker } from '@/shared/dates'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { EmptyState } from '@/components/ui/empty-state'
import { useNetWorthData } from '@/features/dashboard/hooks/useNetWorthData'
import { NetWorthDisplay } from '@/features/dashboard/components/NetWorthDisplay'
import { isNetWorthEmpty } from '@/features/dashboard/utils/netWorth'
import { NetWorthSection } from './components/NetWorthSection'
import { NetWorthHistoryChart } from './components/NetWorthHistoryChart'
import { AccountBreakdownTable } from './components/AccountBreakdownTable'
import { useNetWorthDateRange } from './hooks/useNetWorthDateRange'
import { useAccountBreakdown } from './hooks/useAccountBreakdown'

export function NetWorthPage() {
  const trackEvent = useTrackEvent()
  const { preset, startDate, endDate, setPreset, setCustomRange } = useNetWorthDateRange()

  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.NET_WORTH_PAGE_VIEWED)
  }, [trackEvent])

  const filters = startDate && endDate ? { startDate, endDate } : undefined
  const { summary, history, loading, error, refetch } = useNetWorthData(filters)
  const netWorth = summary?.net_worth ?? 0
  const { assets, liabilities, loading: accountsLoading, error: accountsError } = useAccountBreakdown(netWorth)

  function handlePresetChange(next: typeof preset) {
    setPreset(next)
    trackEvent(ANALYTICS_EVENTS.NET_WORTH_RANGE_CHANGED, { preset: next })
  }

  if (loading) {
    return (
      <main className="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
        <div role="status" aria-label="Loading net worth" className="flex justify-center py-16">
          <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary" />
        </div>
      </main>
    )
  }

  if (error) {
    return (
      <main className="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
        <p className="text-sm text-destructive mb-4">{error.message}</p>
        <button
          onClick={refetch}
          className="text-sm text-primary underline hover:no-underline"
        >
          Retry
        </button>
      </main>
    )
  }

  if (!summary || isNetWorthEmpty(summary)) {
    return (
      <main className="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
        <EmptyState
          icon={<TrendingUp />}
          title="No net worth data"
          description="Connect a bank account to start tracking your net worth."
        />
      </main>
    )
  }

  const hasEnoughHistory = (history?.snapshots?.length ?? 0) >= 2

  return (
    <main className="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
      <div className="mb-6">
        <h1 className="text-2xl font-bold tracking-tight">Net Worth</h1>
        <p className="text-sm text-muted-foreground">Track your total assets, liabilities, and net worth over time.</p>
      </div>

      <div className="mb-6">
        <NetWorthDisplay summary={summary} />
      </div>

      <div className="mb-4">
        <DateRangePicker
          preset={preset}
          startDate={startDate}
          endDate={endDate}
          onPresetChange={handlePresetChange}
          onCustomRangeChange={setCustomRange}
          excludePresets={['this_week', 'last_month']}
        />
      </div>

      <div className="space-y-4">
        {hasEnoughHistory && history && (
          <NetWorthSection title="History">
            <NetWorthHistoryChart history={history} filterKey={`${preset}-${startDate}-${endDate}`} />
          </NetWorthSection>
        )}

        <NetWorthSection title="Breakdown">
          <AccountBreakdownTable
            assets={assets}
            liabilities={liabilities}
            netWorth={netWorth}
            loading={accountsLoading}
            error={accountsError}
          />
        </NetWorthSection>
      </div>
    </main>
  )
}
