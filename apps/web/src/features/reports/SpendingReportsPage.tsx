/**
 * Spending Reports Page
 * Issue #953: Spending Reports Page
 *
 * Provides category breakdown, spending trends, and top merchants
 * for a user-selected date range.
 */

import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { BarChart3 } from 'lucide-react'
import { DateRangePicker } from '@/shared/dates/DateRangePicker'
import { getDateRange } from '@/shared/dates'
import type { DatePreset } from '@/shared/dates'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import { EmptyState } from '@/components/ui/empty-state'
import { useSpendingByCategory } from './hooks/useSpendingByCategory'
import { useSpendingByMerchant } from './hooks/useSpendingByMerchant'
import { useSpendingTrends } from './hooks/useSpendingTrends'
import { CategoryBreakdownChart } from './components/CategoryBreakdownChart'
import { CategoryDrilldownHeading } from './components/CategoryDrilldownHeading'
import { SpendingTrendsChart } from './components/SpendingTrendsChart'
import { TopMerchantsTable } from './components/TopMerchantsTable'
import type { SortState } from '@/shared/components/DataTable'

export function SpendingReportsPage() {
  const navigate = useNavigate()
  const trackEvent = useTrackEvent()

  const [preset, setPreset] = useState<DatePreset>('last_3_months')
  const [customStartDate, setCustomStartDate] = useState('')
  const [customEndDate, setCustomEndDate] = useState('')
  const [drilldown, setDrilldown] = useState<{ category: string; displayLabel: string } | undefined>()
  const [merchantSort, setMerchantSort] = useState<SortState>({ columnId: 'total', direction: 'desc' })

  const derivedRange = preset !== 'custom' ? getDateRange(preset) : null
  const startDate = preset === 'custom' ? customStartDate : (derivedRange?.startDate ?? '')
  const endDate = preset === 'custom' ? customEndDate : (derivedRange?.endDate ?? '')

  // Track page view
  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.REPORTS_VIEWED)
  }, [trackEvent])

  const filters = { startDate, endDate }
  const enabled = Boolean(startDate && endDate)

  const { data: categoryData, loading: categoryLoading } = useSpendingByCategory({
    filters: { ...filters, category: drilldown?.category },
    enabled,
  })

  const { data: merchantData, loading: merchantLoading } = useSpendingByMerchant({
    filters,
    enabled,
  })

  const { data: trendsData, loading: trendsLoading } = useSpendingTrends({
    filters,
    enabled,
  })

  const handlePresetChange = (newPreset: DatePreset) => {
    setPreset(newPreset)
    setDrilldown(undefined)
    trackEvent(ANALYTICS_EVENTS.REPORT_DATE_CHANGED, { preset: newPreset })
  }

  const handleCustomRange = (start: string, end: string) => {
    setCustomStartDate(start)
    setCustomEndDate(end)
    setDrilldown(undefined)
    trackEvent(ANALYTICS_EVENTS.REPORT_DATE_CHANGED, { preset: 'custom' })
  }

  const handleCategoryClick = (category: string, displayLabel: string) => {
    if (category.startsWith('custom:')) return
    if (drilldown?.category === category) {
      setDrilldown(undefined)
    } else {
      setDrilldown({ category, displayLabel })
      trackEvent(ANALYTICS_EVENTS.REPORT_CATEGORY_CLICKED, { category })
    }
  }

  const handleMerchantClick = ({ id, name }: { id?: string | null; name: string }) => {
    trackEvent(ANALYTICS_EVENTS.REPORT_MERCHANT_CLICKED)
    if (!startDate || !endDate || !id) return
    navigate(
      `/transactions?merchant_id=${id}&start_date=${startDate}&end_date=${endDate}`,
      { state: { merchantName: name } }
    )
  }

  return (
    <main className="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
      <div className="mb-6">
        <h1 className="text-2xl font-bold tracking-tight">Spending Reports</h1>
        <p className="text-sm text-muted-foreground">
          Understand where your money goes with category breakdowns and trends.
        </p>
      </div>

      {/* Date range picker */}
      <div className="mb-8">
        <DateRangePicker
          preset={preset}
          startDate={startDate}
          endDate={endDate}
          onPresetChange={handlePresetChange}
          onCustomRangeChange={handleCustomRange}
          excludePresets={['all']}
        />
      </div>

      {!enabled ? (
        <EmptyState
          icon={<BarChart3 />}
          title="Select a date range"
          description="Choose a time period above to view your spending reports."
        />
      ) : (
        <div className="space-y-10">
          {/* Category Breakdown */}
          <section aria-labelledby="category-heading">
            <div className="mb-4">
              <CategoryDrilldownHeading drilldown={drilldown} onBack={() => setDrilldown(undefined)} />
              <p className="text-sm text-muted-foreground">
                {drilldown
                  ? 'Click a subcategory to view its transactions.'
                  : 'Click a category to drill into subcategories.'}
              </p>
            </div>
            <CategoryBreakdownChart
              data={categoryData}
              loading={categoryLoading}
              onCategoryClick={handleCategoryClick}
              className="rounded-xl border border-border bg-card p-4"
            />
          </section>

          {/* Spending Trends */}
          <section aria-labelledby="trends-heading">
            <div className="mb-4">
              <h2 id="trends-heading" className="text-lg font-semibold">
                Spending Trends
              </h2>
              <p className="text-sm text-muted-foreground">
                Monthly spending breakdown by top categories.
              </p>
            </div>
            <SpendingTrendsChart
              data={trendsData}
              loading={trendsLoading}
              className="rounded-xl border border-border bg-card p-4"
            />
          </section>

          {/* Top Merchants */}
          <section aria-labelledby="merchants-heading">
            <div className="mb-4">
              <h2 id="merchants-heading" className="text-lg font-semibold">
                Top Merchants
              </h2>
              <p className="text-sm text-muted-foreground">
                Click a merchant to view its transactions.
              </p>
            </div>
            <TopMerchantsTable
              data={merchantData}
              loading={merchantLoading}
              onMerchantClick={handleMerchantClick}
              className="rounded-xl border border-border bg-card p-4"
              sort={merchantSort}
              onSortChange={setMerchantSort}
            />
          </section>
        </div>
      )}
    </main>
  )
}
