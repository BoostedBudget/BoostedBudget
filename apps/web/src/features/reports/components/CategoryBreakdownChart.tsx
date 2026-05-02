/**
 * Category Breakdown Chart
 * Issue #953: Spending Reports Page
 *
 * Donut chart showing spending distribution by category.
 * Click a category to drill into subcategories.
 */

import { useMemo } from 'react'
import type { Data } from 'plotly.js'
import {
  Plot,
  useDarkMode,
  DEFAULT_CONFIG,
  CATEGORY_COLORS,
  formatCurrencyFull,
} from '@/shared/charts'
import type { SpendingByCategoryResponse } from '@boostedbudget/api-client/models'

interface CategoryBreakdownChartProps {
  data: SpendingByCategoryResponse | null
  loading: boolean
  onCategoryClick?: (category: string) => void
  className?: string
}

export function CategoryBreakdownChart({
  data,
  loading,
  onCategoryClick,
  className,
}: CategoryBreakdownChartProps) {
  const isDark = useDarkMode()

  const chartData: Data[] = useMemo(() => {
    const categories = data?.categories ?? []
    if (categories.length === 0) return []

    return [
      {
        type: 'pie',
        hole: 0.5,
        labels: categories.map((c) => c.pfc_primary_display),
        values: categories.map((c) => c.total),
        marker: {
          colors: CATEGORY_COLORS.slice(0, categories.length) as string[],
        },
        hovertemplate: '%{label}<br>%{value:$,.2f} (%{percent})<extra></extra>',
        textinfo: 'percent',
        textposition: 'inside',
      } as Data,
    ]
  }, [data])

  const layout = useMemo(
    () => ({
      autosize: true,
      showlegend: true,
      margin: { t: 10, r: 10, b: 10, l: 10 },
      paper_bgcolor: isDark ? '#1f2937' : '#ffffff',
      plot_bgcolor: 'rgba(0,0,0,0)',
      font: {
        color: isDark ? '#f3f4f6' : '#111827',
      },
      legend: {
        orientation: 'v' as const,
        x: 1.02,
        y: 0.5,
        font: {
          size: 12,
          color: isDark ? '#f3f4f6' : '#111827',
        },
      },
    }),
    [isDark]
  )

  if (loading) {
    return (
      <div className={className} aria-busy="true" aria-label="Loading chart" data-testid="category-chart">
        <div className="h-64 animate-pulse rounded-lg bg-muted" />
      </div>
    )
  }

  const categories = data?.categories ?? []
  if (categories.length === 0) {
    return (
      <div className={className} role="status" data-testid="category-chart">
        <p className="text-sm text-muted-foreground">No spending data for this period.</p>
      </div>
    )
  }

  return (
    <div className={className} data-testid="category-chart">
      {data && (
        <p className="mb-2 text-sm text-muted-foreground">
          Total: <span className="font-medium" data-ph-mask>{formatCurrencyFull(data.total_spent)}</span>
        </p>
      )}
      <Plot
        data={chartData}
        layout={layout}
        config={DEFAULT_CONFIG}
        style={{ width: '100%', height: '320px' }}
        useResizeHandler
        onClick={
          onCategoryClick
            ? (event) => {
                const point = event.points?.[0]
                if (point) {
                  const idx = point.pointIndex
                  const cat = categories[idx]
                  if (cat) onCategoryClick(cat.category)
                }
              }
            : undefined
        }
      />
    </div>
  )
}
