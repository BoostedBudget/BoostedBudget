import { useState, useEffect } from 'react'
import type { ReactNode } from 'react'
import { useNavigate } from 'react-router'
import { BarChart3, Eye, EyeOff } from 'lucide-react'
import { Card } from '@/components/ui/card'
import { DateRangePicker } from '@/shared/dates/DateRangePicker'
import { getDateRange } from '@/shared/dates'
import type { DatePreset } from '@/shared/dates'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import { EmptyState } from '@/components/ui/empty-state'
import { ErrorState } from '@/components/ui/error-state'
import { useSpendingByCategory } from './hooks/useSpendingByCategory'
import { useSpendingByMerchant } from './hooks/useSpendingByMerchant'
import { useSpendingTrends } from './hooks/useSpendingTrends'
import { useCategoryHierarchy } from './hooks/useCategoryHierarchy'
import { CategoryBreakdownChart } from './components/CategoryBreakdownChart'
import { CategoryDrilldownHeading } from './components/CategoryDrilldownHeading'
import { CustomCategoryFilter } from './components/CustomCategoryFilter'
import { CashFlowSankeyChart } from './components/CashFlowSankeyChart'
import { SankeyViewModeControl } from './components/SankeyViewModeControl'
import type { SankeyViewModeSelection } from './components/SankeyViewModeControl'
import { SpendingTrendsChart } from './components/SpendingTrendsChart'
import { TopMerchantsTable } from './components/TopMerchantsTable'
import type { SortState } from '@/shared/components/DataTable'

// Issue #1569 T9.5 — every read on this page is a read of the user's money, and a failed read
// must never fall through to a chart's own no-data copy: "No spending data for this period" is a
// claim about the user's finances that a failed request cannot support. All five reads share this
// one render path — five separate error blocks would not fit the reports bundle budget.
function readOrError(error: string | null, onRetry: () => void, chart: ReactNode): ReactNode {
  if (!error) return chart
  return (
    <ErrorState
      title="We couldn't load this"
      description="The request failed. Try again in a moment."
      onRetry={onRetry}
      className="border-0 bg-transparent"
    />
  )
}

export function SpendingReportsPage() {
  const navigate = useNavigate()
  const trackEvent = useTrackEvent()

  const [preset, setPreset] = useState<DatePreset>('last_3_months')
  const [customStartDate, setCustomStartDate] = useState('')
  const [customEndDate, setCustomEndDate] = useState('')
  const [drilldown, setDrilldown] = useState<{ category: string; displayLabel: string } | undefined>()
  const [customCategoryIds, setCustomCategoryIds] = useState<string[]>([])
  const [merchantSort, setMerchantSort] = useState<SortState>({ columnId: 'total', direction: 'desc' })
  const [privacyMode, setPrivacyMode] = useState(false)
  // The Sankey owns its own view state. It stays DECOUPLED from `drilldown` below, which drives the
  // pie chart and table: the original design deliberately gave the chart its own unfiltered expense
  // universe so drilling the table could not narrow the chart, and that separation still holds.
  const [sankeyMode, setSankeyMode] = useState<SankeyViewModeSelection>('parents')
  const [sankeyFocus, setSankeyFocus] = useState<string | undefined>()

  const derivedRange = preset !== 'custom' ? getDateRange(preset) : null
  const startDate = preset === 'custom' ? customStartDate : (derivedRange?.startDate ?? '')
  const endDate = preset === 'custom' ? customEndDate : (derivedRange?.endDate ?? '')

  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.REPORTS_VIEWED)
  }, [trackEvent])

  const filters = { startDate, endDate }
  const enabled = Boolean(startDate && endDate)

  // The Sankey's own reads: one per flow direction, each returning the whole category tree in a
  // single round trip so switching view mode never refetches.
  const {
    data: sankeyExpenseData,
    loading: sankeyExpenseLoading,
    error: sankeyExpenseError,
    refetch: refetchSankeyExpense,
  } = useCategoryHierarchy({
    filters: { ...filters, customCategoryIds },
    direction: 'expense',
    enabled,
  })

  const {
    data: sankeyIncomeData,
    loading: sankeyIncomeLoading,
    error: sankeyIncomeError,
    refetch: refetchSankeyIncome,
  } = useCategoryHierarchy({
    filters: { ...filters, customCategoryIds },
    direction: 'income',
    enabled,
  })

  const {
    data: categoryData,
    loading: categoryLoading,
    error: categoryError,
    refetch: refetchCategory,
  } = useSpendingByCategory({
    filters: { ...filters, category: drilldown?.category, customCategoryIds },
    enabled,
  })

  const {
    data: merchantData,
    loading: merchantLoading,
    error: merchantError,
    refetch: refetchMerchant,
  } = useSpendingByMerchant({
    filters,
    enabled,
  })

  const {
    data: trendsData,
    loading: trendsLoading,
    error: trendsError,
    refetch: refetchTrends,
  } = useSpendingTrends({
    filters,
    enabled,
  })

  const handlePresetChange = (newPreset: DatePreset) => {
    setPreset(newPreset)
    setDrilldown(undefined)
    // #1626: the focused category may not exist in the new universe. Leaving it set makes the
    // control claim a focus the chart has silently abandoned.
    setSankeyFocus(undefined)
    trackEvent(ANALYTICS_EVENTS.REPORT_DATE_CHANGED, { preset: newPreset })
  }

  const handleCustomRange = (start: string, end: string) => {
    setCustomStartDate(start)
    setCustomEndDate(end)
    setDrilldown(undefined)
    // #1626: the focused category may not exist in the new universe. Leaving it set makes the
    // control claim a focus the chart has silently abandoned.
    setSankeyFocus(undefined)
    trackEvent(ANALYTICS_EVENTS.REPORT_DATE_CHANGED, { preset: 'custom' })
  }

  const handleCustomCategoryFilterChange = (ids: string[]) => {
    setCustomCategoryIds(ids)
    setDrilldown(undefined)
    // #1626: the focused category may not exist in the new universe. Leaving it set makes the
    // control claim a focus the chart has silently abandoned.
    setSankeyFocus(undefined)
    trackEvent(ANALYTICS_EVENTS.REPORT_CUSTOM_CATEGORY_FILTERED, { count: ids.length })
  }

  const handleCategoryClick = (category: string, displayLabel: string) => {
    if (category.startsWith('custom:')) return
    if (drilldown?.category === category) {
      // Deliberately does NOT touch sankeyFocus. A pie drilldown does not change the Sankey's
      // universe — the chart reads its own endpoint, which has no `category` param at all — so
      // resetting its focus here would re-couple the two views that are kept separate on purpose.
      setDrilldown(undefined)
    } else {
      setDrilldown({ category, displayLabel })
      trackEvent(ANALYTICS_EVENTS.REPORT_CATEGORY_CLICKED, { category })
    }
  }

  const handlePrivacyToggle = () => {
    setPrivacyMode(prev => {
      trackEvent(ANALYTICS_EVENTS.REPORT_SANKEY_PRIVACY_TOGGLED, { enabled: !prev })
      return !prev
    })
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

      <div className="mb-8">
        <DateRangePicker
          preset={preset}
          startDate={startDate}
          endDate={endDate}
          onPresetChange={handlePresetChange}
          onCustomRangeChange={handleCustomRange}
          excludePresets={['all']}
        />
        <CustomCategoryFilter
          selectedIds={customCategoryIds}
          onChange={handleCustomCategoryFilterChange}
          className="mt-4"
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
          <section aria-labelledby="cashflow-heading">
            <div className="mb-4 flex items-center justify-between">
              <div>
                <h2 id="cashflow-heading" className="text-lg font-semibold">Cash Flow</h2>
                <p className="text-sm text-muted-foreground">
                  Income sources flowing into expense categories, as a percentage of total flow.
                </p>
              </div>
              <button
                type="button"
                aria-pressed={privacyMode}
                aria-label={privacyMode ? 'Show amounts' : 'Hide amounts'}
                onClick={handlePrivacyToggle}
                className="flex items-center gap-1 rounded-md px-2 py-1 text-sm text-muted-foreground hover:bg-muted hover:text-foreground"
              >
                {privacyMode ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                {privacyMode ? 'Show' : 'Hide'}
              </button>
            </div>
            <div className="mb-3 flex justify-end">
              <SankeyViewModeControl
                value={sankeyMode}
                onChange={setSankeyMode}
                focusedKey={sankeyFocus}
                onClearFocus={() => setSankeyFocus(undefined)}
              />
            </div>
            <Card>
              {readOrError(
                sankeyIncomeError ?? sankeyExpenseError,
                () => {
                  void refetchSankeyIncome()
                  void refetchSankeyExpense()
                },
                <CashFlowSankeyChart
                  incomeData={sankeyIncomeData}
                  expenseData={sankeyExpenseData}
                  loading={sankeyIncomeLoading || sankeyExpenseLoading}
                  privacyMode={privacyMode}
                  startDate={startDate}
                  endDate={endDate}
                  viewMode={sankeyFocus ? { kind: 'focus', key: sankeyFocus } : { kind: sankeyMode }}
                  onFocus={setSankeyFocus}
                />,
              )}
            </Card>
          </section>

          <section aria-labelledby="category-heading">
            <div className="mb-4">
              <CategoryDrilldownHeading drilldown={drilldown} onBack={() => setDrilldown(undefined)} />
              <p className="text-sm text-muted-foreground">
                {drilldown
                  ? 'Click a subcategory to view its transactions.'
                  : 'Click a category to drill into subcategories.'}
              </p>
            </div>
            <Card>
              {readOrError(
                categoryError,
                () => void refetchCategory(),
                <CategoryBreakdownChart
                  data={categoryData}
                  loading={categoryLoading}
                  onCategoryClick={handleCategoryClick}
                />,
              )}
            </Card>
          </section>

          <section aria-labelledby="trends-heading">
            <div className="mb-4">
              <h2 id="trends-heading" className="text-lg font-semibold">
                Spending Trends
              </h2>
              <p className="text-sm text-muted-foreground">
                Monthly spending breakdown by top categories.
              </p>
            </div>
            <Card>
              {readOrError(
                trendsError,
                () => void refetchTrends(),
                <SpendingTrendsChart
                  data={trendsData}
                  loading={trendsLoading}
                />,
              )}
            </Card>
          </section>

          <section aria-labelledby="merchants-heading">
            <div className="mb-4">
              <h2 id="merchants-heading" className="text-lg font-semibold">
                Top Merchants
              </h2>
              <p className="text-sm text-muted-foreground">
                Click a merchant to view its transactions.
              </p>
            </div>
            <Card>
              {readOrError(
                merchantError,
                () => void refetchMerchant(),
                <TopMerchantsTable
                  data={merchantData}
                  loading={merchantLoading}
                  onMerchantClick={handleMerchantClick}
                  sort={merchantSort}
                  onSortChange={setMerchantSort}
                />,
              )}
            </Card>
          </section>
        </div>
      )}
    </main>
  )
}
