/**
 * Asset Allocation Widget
 * Issue #1297: Investment widgets — allocation donut chart with server-persisted groupBy.
 *
 * Reuses AllocationDonutChart from the Portfolio tab. Drives group_by from the
 * per-widget settings so the user's preference follows them cross-device.
 */

import { useEffect, useRef } from 'react'
import { PieChart } from 'lucide-react'
import { useGetPortfolioAllocationBreakdown } from '@boostedbudget/api-client/endpoints/analytics/analytics'
import { AllocationDonutChart } from '@/features/investments/components/AllocationDonutChart'
import { registerWidget } from './registry'
import { WidgetEmptyState } from './WidgetEmptyState'
import { normalizeWidgetSettings } from './settings'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import type { WidgetProps, AssetAllocationSettings } from '../types'
import type { AllocationConfig } from '@/features/investments/hooks/usePortfolioAllocationConfig'

function AssetAllocationWidget(props: WidgetProps) {
  const { settings: rawSettings, onSettingsChange, isEditing } = props
  const trackEvent = useTrackEvent()
  const viewedFired = useRef(false)

  const { groupBy } = normalizeWidgetSettings(
    'asset-allocation',
    rawSettings as Record<string, unknown> | undefined
  ) as AssetAllocationSettings

  // Call the hook at the widget level to detect loading / empty / error states
  // AllocationDonutChart also calls this hook internally; React Query deduplicates the request
  const { data: allocationData, isLoading, isError, error } = useGetPortfolioAllocationBreakdown({
    group_by: groupBy,
    basis: 'assets',
  })

  const config: AllocationConfig = {
    include: [],
    group_by: groupBy,
    basis: 'assets',
  }

  useEffect(() => {
    if (!viewedFired.current && allocationData?.buckets && allocationData.buckets.length > 0) {
      viewedFired.current = true
      trackEvent(ANALYTICS_EVENTS.ASSET_ALLOCATION_WIDGET_VIEWED)
    }
  }, [allocationData, trackEvent])

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-full">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary" />
      </div>
    )
  }

  if (isError) {
    return (
      <div className="flex items-center justify-center h-full text-destructive text-sm">
        {(error as Error)?.message ?? 'Failed to load allocation'}
      </div>
    )
  }

  if (!allocationData?.buckets || allocationData.buckets.length === 0) {
    return <WidgetEmptyState widgetType="asset-allocation" />
  }

  return (
    <div className="h-full flex flex-col gap-2 p-1 overflow-auto">
      {/* groupBy controls — edit mode only */}
      {isEditing && (
        <fieldset className="flex gap-4 flex-wrap mb-1">
          <legend className="text-xs text-muted-foreground mb-1">Group by</legend>
          {([
            { value: 'security_type', label: 'Type' },
            { value: 'asset_class', label: 'Class' },
            { value: 'account', label: 'Account' },
          ] as const).map(({ value, label }) => (
            <label key={value} className="flex items-center gap-1 text-xs cursor-pointer">
              <input
                type="radio"
                name={`groupby-${props.widgetId}`}
                value={value}
                checked={groupBy === value}
                onChange={() => {
                  onSettingsChange?.({ groupBy: value })
                }}
                aria-label={label}
              />
              {label}
            </label>
          ))}
        </fieldset>
      )}

      {/* Allocation donut chart */}
      <div className="flex-1 min-h-0">
        <AllocationDonutChart config={config} />
      </div>
    </div>
  )
}

registerWidget({
  type: 'asset-allocation',
  title: 'Asset Allocation',
  description: 'Donut chart showing your portfolio allocation by security type, asset class, or account.',
  icon: 'chart-pie',
  category: 'investments',
  defaultSize: { w: 4, h: 4 },
  minSize: { w: 3, h: 3 },
  Component: AssetAllocationWidget,
  emptyState: {
    icon: <PieChart />,
    title: 'No allocation data',
    noAccountsDescription: 'Connect a brokerage account to see your allocation',
    noDataDescription: 'No holdings in your connected accounts',
  },
})

export { AssetAllocationWidget }
