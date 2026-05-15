/**
 * Spending Trends Chart
 * Issue #953: Spending Reports Page
 *
 * Stacked bar chart showing top 5 spending categories over time.
 * Other categories are bucketed into "Other".
 */

import { useMemo } from 'react'
import { format, parseISO } from 'date-fns'
import type { Data } from 'plotly.js'
import {
  Plot,
  useDarkMode,
  getBarChartLayout,
  DEFAULT_CONFIG,
  CATEGORY_COLORS,
} from '@/shared/charts'
import { formatPFCPrimary } from '@/shared/categories/formatPfc'
import type { SpendingTrendsResponse } from '@boostedbudget/api-client/models'

interface SpendingTrendsChartProps {
  data: SpendingTrendsResponse | null
  loading: boolean
  className?: string
}

export function SpendingTrendsChart({ data, loading, className }: SpendingTrendsChartProps) {
  const isDark = useDarkMode()

  const chartData: Data[] = useMemo(() => {
    const periods = data?.periods ?? []
    const topCategories = data?.top_categories ?? []

    if (periods.length === 0 || topCategories.length === 0) return []

    const allCategories = [...topCategories]
    const hasOther = periods.some((p) =>
      p.categories?.some((c) => c.category === 'OTHER')
    )
    if (hasOther) allCategories.push('OTHER')

    const xLabels = periods.map((p) => {
      try {
        return format(parseISO(p.period_start), 'MMM yyyy')
      } catch {
        return p.period_start
      }
    })

    const findDisplayName = (cat: string) => {
      if (cat === 'OTHER') return 'Other'
      for (const p of periods) {
        const found = p.categories?.find((c) => c.category === cat)
        if (found?.pfc_primary_display) return found.pfc_primary_display
      }
      // Guard against raw custom:<uuid> tokens reaching the chart
      if (cat.startsWith('custom:')) return 'Custom Category'
      return formatPFCPrimary(cat)
    }

    return allCategories.map((cat, i) => {
      const displayName = findDisplayName(cat)

      const values = periods.map((p) => {
        const found = p.categories?.find((c) => c.category === cat)
        return found?.total ?? 0
      })

      return {
        type: 'bar',
        name: displayName,
        x: xLabels,
        y: values,
        marker: {
          color: CATEGORY_COLORS[i % CATEGORY_COLORS.length],
        },
        hovertemplate: `${displayName}<br>%{y:$,.2f}<extra></extra>`,
      } as Data
    })
  }, [data])

  const layout = useMemo(
    () => ({
      ...getBarChartLayout(isDark),
      barmode: 'stack' as const,
    }),
    [isDark]
  )

  if (loading) {
    return (
      <div className={className} aria-busy="true" aria-label="Loading trends chart" data-testid="trends-chart">
        <div className="h-64 animate-pulse rounded-lg bg-muted" />
      </div>
    )
  }

  const periods = data?.periods ?? []
  if (periods.length === 0) {
    return (
      <div className={className} role="status" data-testid="trends-chart">
        <p className="text-sm text-muted-foreground">No trend data available for this period.</p>
      </div>
    )
  }

  return (
    <div className={className} data-testid="trends-chart">
      <Plot
        data={chartData}
        layout={layout}
        config={DEFAULT_CONFIG}
        style={{ width: '100%', height: '320px' }}
        useResizeHandler
      />
    </div>
  )
}
