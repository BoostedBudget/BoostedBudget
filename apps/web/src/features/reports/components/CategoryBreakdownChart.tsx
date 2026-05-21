import { useMemo } from 'react'
import type { EChartsOption } from 'echarts'
import {
  EChartsChart,
  useDarkMode,
  CATEGORY_COLORS,
  formatCurrencyFull,
} from '@/shared/charts'
import type { SpendingByCategoryResponse } from '@boostedbudget/api-client/models'

interface CategoryBreakdownChartProps {
  data: SpendingByCategoryResponse | null
  loading: boolean
  onCategoryClick?: (category: string, displayLabel: string) => void
  className?: string
}

export function CategoryBreakdownChart({
  data,
  loading,
  onCategoryClick,
  className,
}: CategoryBreakdownChartProps) {
  const isDark = useDarkMode()
  const categories = data?.categories ?? []

  const option = useMemo((): EChartsOption => {
    if (categories.length === 0) return {};
    return {
      tooltip: {
        trigger: 'item',
        formatter: (params: unknown) => {
          const { name, value, percent } = params as { name: string; value: number; percent: number };
          return `${name}<br>${formatCurrencyFull(value)} (${percent.toFixed(1)}%)`;
        },
      },
      legend: {
        orient: 'vertical',
        right: 0,
        top: 'middle',
      },
      series: [{
        type: 'pie',
        radius: ['40%', '70%'],
        avoidLabelOverlap: false,
        label: { show: true, position: 'inside', formatter: '{d}%' },
        data: categories.map((c, i) => ({
          name: c.pfc_primary_display,
          value: c.total,
          itemStyle: { color: CATEGORY_COLORS[i % CATEGORY_COLORS.length] },
        })),
      }],
    };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data]);

  if (loading) {
    return (
      <div className={className} aria-busy="true" aria-label="Loading chart" data-testid="category-chart">
        <div className="h-64 animate-pulse rounded-lg bg-muted" />
      </div>
    )
  }

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
      <EChartsChart
        option={option}
        isDark={isDark}
        style={{ width: '100%', height: '320px' }}
        onEvents={onCategoryClick ? {
          click: (params: unknown) => {
            const { dataIndex } = params as { dataIndex: number };
            const cat = categories[dataIndex];
            if (cat) onCategoryClick(cat.category, cat.pfc_primary_display ?? cat.category);
          },
        } : undefined}
      />
    </div>
  );
}
