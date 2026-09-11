import { useMemo } from 'react'
import type { EChartsOption } from 'echarts'
import {
  EChartsChart,
  useDarkMode,
  CATEGORY_COLORS,
  formatPercent,
  escapeHtml,
} from '@/shared/charts'
import { formatPortfolioAmount, isCurrencyUnnameable } from '../utils/portfolioCurrency'
import { MixedCurrencyBadge } from './MixedCurrencyBadge'
import { useGetPortfolioAllocationBreakdown } from '@boostedbudget/api-client/endpoints/analytics/analytics'
import type { AllocationConfig, GetPortfolioAllocationBreakdownGroupBy } from '../hooks/usePortfolioAllocationConfig'
import { ErrorState } from '@/components/ui/error-state'
import { securityTypeLabel } from '@/features/transactions/investmentSubcategory'

interface AllocationDonutChartProps {
  config: AllocationConfig
}

// Map raw API security-type tokens to display-ready labels.
// Rules (in priority order):
//   1. Falsy / "unknown" → "Other"
//   2. group_by=account → verbatim (account names are already human-readable)
//   3. Known acronyms → uppercase canonical form (etf → "ETF", reit → "REIT")
//   4. Everything else → title-case by splitting on underscores/spaces
//      ("mutual_fund" → "Mutual Fund", "stock" → "Stock")
// Rules 1/3/4 are the shared `securityTypeLabel` helper (#1631) — reused here so this chart and
// HoldingsTable's Type column render the SAME label for the SAME `securities.type` value.
function humanizeBucketLabel(value: string, groupBy: GetPortfolioAllocationBreakdownGroupBy): string {
  if (!value || value.toLowerCase() === 'unknown') return 'Other'
  if (groupBy === 'account') return value
  return securityTypeLabel(value, 'Other')
}

// Aggregate buckets with the same humanized label, summing values and counts.
// Prevents last-write-wins collisions (e.g., multiple "unknown"/"" → "Other").
function aggregateBuckets(
  buckets: Array<{ security_type: string; asset_class: string; total_value: number; percentage: number; holding_count: number }>,
  groupBy: GetPortfolioAllocationBreakdownGroupBy,
): Array<{ name: string; total_value: number; pct: number; holding_count: number }> {
  const order: string[] = []
  const byName = new Map<string, { total_value: number; pct: number; holding_count: number }>()

  for (const bucket of buckets) {
    const raw = groupBy === 'asset_class' ? bucket.asset_class : bucket.security_type
    const name = humanizeBucketLabel(raw, groupBy)
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
// Handles any deficit magnitude — including |deficit| > sliceCount — by distributing
// floor(|deficit|/n) tenths to every slice, then distributing the remaining tenths
// to the highest-remainder slices. This is the general form; the original single-pass
// Set approach only worked when |deficit| ≤ n.
function reconcilePercentages<T extends { pct: number }>(slices: T[]): T[] {
  if (slices.length === 0) return slices

  // Work in tenths of a percent to avoid floating-point accumulation errors.
  const SCALE = 10
  const TARGET = 1000 // 100.0 × 10
  const n = slices.length

  const floors = slices.map((s) => Math.floor(s.pct * SCALE))
  const remainders = slices.map((s, i) => s.pct * SCALE - floors[i])
  const deficit = TARGET - floors.reduce((a, b) => a + b, 0)

  if (deficit === 0) return slices.map((s, i) => ({ ...s, pct: floors[i] / SCALE }))

  const sign = deficit >= 0 ? 1 : -1
  const absDeficit = Math.abs(deficit)
  // Each slice receives at least `base` extra tenths; `extra` slices receive one more.
  const base = Math.floor(absDeficit / n)
  const extra = absDeficit % n

  // Positive deficit → give extra to largest-remainder slices.
  // Negative deficit → take extra from smallest-remainder slices.
  const ranked = remainders
    .map((r, i) => ({ i, r }))
    .sort((a, b) => (deficit >= 0 ? b.r - a.r : a.r - b.r))
  const extraIndices = new Set(ranked.slice(0, extra).map((x) => x.i))

  return slices.map((s, i) => ({
    ...s,
    pct: (floors[i] + sign * (base + (extraIndices.has(i) ? 1 : 0))) / SCALE,
  }))
}

export function AllocationDonutChart({ config }: AllocationDonutChartProps) {
  const isDark = useDarkMode()

  const { data: allocationData, isLoading, isError, refetch } = useGetPortfolioAllocationBreakdown({
    include: config.include.join(',') || undefined,
    group_by: config.group_by,
    basis: config.basis,
  })

  const buckets = allocationData?.buckets

  // #1631 CX1: the tooltip and legend formatted every slice as dollars. The breakdown carries its
  // own currency verdict and it was thrown away. The verdict is breakdown-level, not per-slice: a
  // donut's slices are compared against one another and against a single total, so a chart whose
  // slices disagree about the unit is broken as a whole.
  const currency = useMemo(
    () => ({
      currency_code: allocationData?.currency_code,
      is_mixed_currency: allocationData?.is_mixed_currency,
    }),
    [allocationData?.currency_code, allocationData?.is_mixed_currency],
  )

  const option = useMemo((): EChartsOption => {
    if (!buckets || buckets.length === 0) return {}

    // Aggregate same-label buckets, filter zero-value (overdrawn cash, etc.), apply rounding
    const aggregated = aggregateBuckets(buckets, config.group_by).filter((b) => b.total_value > 0)
    if (aggregated.length === 0) return {}

    const rawSlices = aggregated.map((b, i) => ({
      name: b.name,
      value: b.total_value,
      pct: b.pct,
      itemStyle: { color: CATEGORY_COLORS[i % CATEGORY_COLORS.length] },
    }))
    const sliceData = reconcilePercentages(rawSlices)
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
          return `${escapeHtml(String(p.name))}: ${formatPortfolioAmount(p.value, currency)} (${pct}%)`
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
          return `${escapeHtml(name)}  ${formatPortfolioAmount(slice.value, currency)}  (${formatPercent(slice.pct)})`
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
  }, [buckets, config.group_by, currency])

  if (isLoading) {
    return (
      <div role="status" aria-label="Loading allocation chart" className="flex items-center justify-center h-48">
        <div className="animate-spin rounded-full size-6 border-b-2 border-primary" />
      </div>
    )
  }

  // Issue #1569 T9.5 — see PortfolioValueChart: "No allocation data" on a failed read tells the
  // user they hold nothing.
  if (isError) {
    return <ErrorState title="We couldn't load this chart" onRetry={refetch} />
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
      {/* Without this the reader is left to guess why the slices lost their currency symbol. */}
      {isCurrencyUnnameable(currency) && <MixedCurrencyBadge className="mb-2" />}
      <EChartsChart
        style={{ width: '100%', height: '100%' }}
        option={option}
        isDark={isDark}
        ariaLabel="Portfolio allocation breakdown"
      />
    </div>
  )
}
