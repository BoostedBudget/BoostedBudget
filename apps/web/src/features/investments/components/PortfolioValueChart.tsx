import { useMemo } from 'react'
import type { EChartsOption } from 'echarts'
import {
  EChartsChart,
  useDarkMode,
  getCurrencyOption,
  formatCurrency,
  escapeHtml,
} from '@/shared/charts'
import { useGetPortfolioHistory } from '@boostedbudget/api-client/endpoints/investments/investments'
import { ErrorState } from '@/components/ui/error-state'

interface PortfolioValueChartProps {
  dateRange: { start_date: string; end_date: string }
}

export function PortfolioValueChart({ dateRange }: PortfolioValueChartProps) {
  const isDark = useDarkMode()

  const { data, isLoading, isError, refetch } = useGetPortfolioHistory({
    start_date: dateRange.start_date,
    end_date: dateRange.end_date,
  })

  const history = useMemo(() => data?.history ?? [], [data?.history])

  const option = useMemo((): EChartsOption => {
    const base = getCurrencyOption(isDark)
    return {
      ...base,
      xAxis: { type: 'category', data: history.map((p) => p.date) },
      tooltip: {
        trigger: 'axis',
        formatter: (params: unknown) => {
          const arr = Array.isArray(params) ? params : []
          if (arr.length === 0) return ''
          const [p] = arr as Array<{ name: string; value: number }>
          return `${escapeHtml(p.name)}<br>${formatCurrency(p.value)}`
        },
      },
      series: [
        {
          type: 'line',
          smooth: true,
          showSymbol: false,
          data: history.map((p) => p.total_value),
        },
      ],
    }
  }, [isDark, history])

  if (isLoading) {
    return (
      <div role="status" aria-label="Loading portfolio chart" className="flex items-center justify-center h-48">
        <div className="animate-spin rounded-full size-6 border-b-2 border-primary" />
      </div>
    )
  }

  // Issue #1569 T9.5 — a failed read is not an empty range. "Not enough data for this range"
  // tells the user their portfolio has no history here; a request that never returned cannot
  // support that.
  if (isError) {
    return <ErrorState title="We couldn't load this chart" onRetry={refetch} />
  }

  if (history.length < 2) {
    return (
      <div className="flex items-center justify-center h-48 text-muted-foreground text-sm">
        Not enough data for this range
      </div>
    )
  }

  return (
    <div className="h-[300px] sm:h-[400px] w-full">
      <EChartsChart
        style={{ width: '100%', height: '100%' }}
        option={option}
        isDark={isDark}
        ariaLabel="Portfolio value over time"
      />
    </div>
  )
}
