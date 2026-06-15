import { useMemo } from 'react'
import type { EChartsOption } from 'echarts'
import {
  EChartsChart,
  useDarkMode,
  CATEGORY_COLORS,
  formatCurrencyFull,
  formatPercent,
  escapeHtml,
} from '@/shared/charts'
import { useGetPortfolioAllocationBreakdown } from '@boostedbudget/api-client/endpoints/analytics/analytics'
import type { AllocationConfig, GetPortfolioAllocationBreakdownGroupBy } from '../hooks/usePortfolioAllocationConfig'

interface AllocationDonutChartProps {
  config: AllocationConfig
}

function normalizeBucketLabel(bucket: { security_type: string; asset_class: string }, groupBy: GetPortfolioAllocationBreakdownGroupBy): string {
  const raw = groupBy === 'asset_class' ? bucket.asset_class : bucket.security_type
  if (!raw || raw.toLowerCase() === 'unknown') return 'Other'
  return raw
}

// Aggregate buckets with the same normalized label, summing values and counts.
// Prevents last-write-wins collisions in the legend formatter (e.g., multiple "unknown" → "Other").
function aggregateBuckets(
  buckets: Array<{ security_type: string; asset_class: string; total_value: number; percentage: number; holding_count: number }>,
  groupBy: GetPortfolioAllocationBreakdownGroupBy,
): Array<{ name: string; total_value: number; pct: number; holding_count: number }> {
  const order: string[] = []
  const byName = new Map<string, { total_value: number; pct: number; holding_count: number }>()

  for (const bucket of buckets) {
    const name = normalizeBucketLabel(bucket, groupBy)
    const existing = byName.get(name)
    if (existing) {
      byName.set(name, {
        total_value: existing.total_value + bucket.total_value,
        pct: existing.pct + bucket.percentage,
        holding_count: existing.holding_count + bucket.holding_count,
      })
    } else {
      order.push(name)
      byName.set(name, {
        total_value: bucket.total_value,
        pct: bucket.percentage,
        holding_count: bucket.holding_count,
      })
    }
  }

  return order.map((name) => ({ name, ...byName.get(name)! }))
}

// Largest-remainder (Hamilton) rounding: ensures displayed percentages (via toFixed(1)) sum to 100.0.
// Handles both positive deficit (floors sum < 1000 → add tenths to largest-remainder slices) and
// negative deficit (floors sum > 1000 → subtract tenths from smallest-remainder slices).
function reconcilePercentages<T extends { pct: number }>(slices: T[]): T[] {
  if (slices.length === 0) return slices

  // Work in tenths of a percent to avoid floating-point accumulation errors.
  const SCALE = 10
  const TARGET = 1000 // 100.0 × 10

  const floors = slices.map((s) => Math.floor(s.pct * SCALE))
  const remainders = slices.map((s, i) => s.pct * SCALE - floors[i])
  const deficit = TARGET - floors.reduce((a, b) => a + b, 0)

  const count = Math.abs(deficit)
  // Positive deficit: adjust slices with the largest remainders upward (+1 tenth each).
  // Negative deficit: adjust slices with the smallest remainders downward (-1 tenth each).
  const ranked = remainders
    .map((r, i) => ({ i, r }))
    .sort((a, b) => (deficit >= 0 ? b.r - a.r : a.r - b.r))
  const adjustIndices = new Set(ranked.slice(0, count).map((x) => x.i))
  const adjustment = deficit >= 0 ? 1 : -1

  return slices.map((s, i) => ({
    ...s,
    pct: (floors[i] + (adjustIndices.has(i) ? adjustment : 0)) / SCALE,
  }))
}

export function AllocationDonutChart({ config }: AllocationDonutChartProps) {
  const isDark = useDarkMode()

  const { data: allocationData, isLoading } = useGetPortfolioAllocationBreakdown({
    include: config.include.join(',') || undefined,
    group_by: config.group_by,
    basis: config.basis,
  })

  const buckets = allocationData?.buckets

  const option = useMemo((): EChartsOption => {
    if (!buckets || buckets.length === 0) return {}

    // Step 1: Aggregate buckets with the same normalized label (e.g., two "unknown" → "Other")
    // so sliceByName cannot have last-write-wins collisions. Filter out zero-value buckets
    // (e.g. overdrawn cash floored to $0 by GREATEST) to avoid invisible slices and fabricated
    // 0.1% labels from Hamilton rounding on a single zero-value entry.
    const aggregated = aggregateBuckets(buckets, config.group_by).filter((b) => b.total_value > 0)
    if (aggregated.length === 0) return {}

    // Step 2: Build slice objects with colors, then apply largest-remainder rounding so that
    // the displayed percentages (via .toFixed(1)) sum to exactly 100.0.
    const rawSlices = aggregated.map((b, i) => ({
      name: b.name,
      value: b.total_value,
      pct: b.pct,
      itemStyle: { color: CATEGORY_COLORS[i % CATEGORY_COLORS.length] },
    }))
    const sliceData = reconcilePercentages(rawSlices)

    // Build name→slice lookup for the legend formatter (safe: all names are now unique).
    const sliceByName = new Map(sliceData.map((s) => [s.name, s]))

    return {
      tooltip: {
        trigger: 'item',
        // Use API-computed pct (p.data.pct) so basis=net percentages are correct.
        // ECharts' own p.percent always normalises to 100% of visible slices,
        // which gives wrong results when the net denominator differs from slice sum.
        formatter: (params: unknown) => {
          if (!params || typeof params !== 'object') return ''
          const p = params as { name: string; value?: number; data?: { pct?: number } }
          if (typeof p.value !== 'number') return escapeHtml(String(p.name ?? ''))
          const pct = (p.data?.pct ?? 0).toFixed(1)
          return `${escapeHtml(String(p.name))}: ${formatCurrencyFull(p.value)} (${pct}%)`
        },
      },
      legend: {
        orient: 'vertical',
        right: 0,
        top: 'middle',
        // Surface value + % in the legend so the donut is readable without hovering.
        formatter: (name: string) => {
          const slice = sliceByName.get(name)
          if (!slice) return escapeHtml(name)
          return `${escapeHtml(name)}  ${formatCurrencyFull(slice.value)}  (${formatPercent(slice.pct)})`
        },
      },
      series: [
        {
          type: 'pie',
          radius: ['40%', '70%'],
          avoidLabelOverlap: false,
          // Use API-computed percentage (not ECharts internal {d}%) so basis=net renders correctly
          label: {
            show: true,
            position: 'inside',
            formatter: (params: unknown) => {
              const p = params as { data?: { pct?: number } }
              return `${(p.data?.pct ?? 0).toFixed(1)}%`
            },
          },
          data: sliceData,
        },
      ],
    }
  }, [buckets, config.group_by])

  if (isLoading) {
    return (
      <div role="status" aria-label="Loading allocation chart" className="flex items-center justify-center h-48">
        <div className="animate-spin rounded-full size-6 border-b-2 border-primary" />
      </div>
    )
  }

  if (!buckets || buckets.length === 0 || Object.keys(option).length === 0) {
    return (
      <div className="flex items-center justify-center h-48 text-muted-foreground text-sm">
        No allocation data
      </div>
    )
  }

  return (
    <div className="h-[300px] sm:h-[400px] w-full">
      <EChartsChart
        style={{ width: '100%', height: '100%' }}
        option={option}
        isDark={isDark}
        ariaLabel="Portfolio allocation breakdown"
      />
    </div>
  )
}
