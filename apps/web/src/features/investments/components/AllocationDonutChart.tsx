import { useMemo } from 'react'
import type { EChartsOption } from 'echarts'
import {
  EChartsChart,
  useDarkMode,
  CATEGORY_COLORS,
  formatCurrencyFull,
  escapeHtml,
} from '@/shared/charts'
import { useGetPortfolioAllocationBreakdown } from '@boostedbudget/api-client/endpoints/analytics/analytics'
import type { AllocationConfig } from '../hooks/usePortfolioAllocationConfig'

interface AllocationDonutChartProps {
  config: AllocationConfig
}

function normalizeBucketLabel(bucket: { security_type: string; asset_class: string }, groupBy: string): string {
  const raw = groupBy === 'asset_class' ? bucket.asset_class : bucket.security_type
  if (!raw || raw.toLowerCase() === 'unknown') return 'Other'
  return raw
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
    return {
      tooltip: {
        trigger: 'item',
        formatter: (params: unknown) => {
          if (!params || typeof params !== 'object') return ''
          const p = params as { name: string; value?: number; percent?: number }
          if (typeof p.value === 'number' && typeof p.percent === 'number') {
            return `${escapeHtml(String(p.name))}: ${formatCurrencyFull(p.value)} (${p.percent.toFixed(1)}%)`
          }
          return escapeHtml(String(p.name ?? ''))
        },
      },
      legend: {
        orient: 'vertical',
        right: 0,
        top: 'middle',
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
          data: buckets.map((bucket, i) => ({
            name: normalizeBucketLabel(bucket, config.group_by),
            value: bucket.total_value,
            pct: bucket.percentage,
            itemStyle: { color: CATEGORY_COLORS[i % CATEGORY_COLORS.length] },
          })),
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

  if (!buckets || buckets.length === 0) {
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
