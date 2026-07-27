import { useMemo } from 'react'
import { useNavigate } from 'react-router'
import type { EChartsOption } from 'echarts'
import type { IncomeSourceBreakdownResponse, SpendingByCategoryResponse } from '@boostedbudget/api-client/models'
import {
  EChartsChart,
  useDarkMode,
  formatCurrencyFull,
  formatPercent,
  escapeHtml,
} from '@/shared/charts'
import { buildSankeyData } from './buildSankeyData'
import type { SankeyNode } from './buildSankeyData'

interface CashFlowSankeyChartProps {
  incomeData: IncomeSourceBreakdownResponse | null
  categoryData: SpendingByCategoryResponse | null
  loading: boolean
  privacyMode: boolean
  startDate: string
  endDate: string
  className?: string
}

export function CashFlowSankeyChart({
  incomeData,
  categoryData,
  loading,
  privacyMode,
  startDate,
  endDate,
  className,
}: CashFlowSankeyChartProps) {
  const navigate = useNavigate()
  const isDark = useDarkMode()

  const sankeyData = useMemo(
    () => buildSankeyData(
      incomeData?.sources ?? [],
      categoryData?.categories ?? [],
      incomeData?.total_income ?? 0,
      categoryData?.total_spent ?? 0,
    ),
    [incomeData, categoryData],
  )

  const handleClick = (params: unknown) => {
    const p = params as { dataType?: string; data?: unknown; dataIndex?: number }
    const node = p.dataType === 'node' && p.data && typeof (p.data as SankeyNode).id === 'string'
      ? (p.data as SankeyNode)
      : !p.dataType && typeof p.dataIndex === 'number'
      ? sankeyData.nodes[p.dataIndex] // dataIndex-only: test mock path; real ECharts always sets dataType
      : undefined

    if (!node?.navigable) return

    const urlParams = new URLSearchParams()
    if (node.nodeKind === 'expense') {
      if (node.isCustom && node.category) {
        urlParams.set('custom_category_ids', node.category.slice('custom:'.length))
      } else if (node.category) {
        urlParams.set('pfc_primaries', node.category)
      }
      urlParams.set('amount_type', 'expense')
    } else if (node.nodeKind === 'income' && node.category) {
      urlParams.set('pfc_primaries', node.category)
      urlParams.set('amount_type', 'income')
    }
    if (startDate) urlParams.set('start_date', startDate)
    if (endDate) urlParams.set('end_date', endDate)
    navigate(`/transactions?${urlParams.toString()}`)
  }

  const option = useMemo((): EChartsOption => {
    if (sankeyData.nodes.length === 0) return {}
    return {
      tooltip: {
        trigger: 'item',
        formatter: (params: unknown) => {
          const p = params as { value?: number; name?: string }
          if (!p || typeof p.value !== 'number') return ''
          const pct = sankeyData.hubTotal > 0 ? (p.value / sankeyData.hubTotal) * 100 : 0
          const label = escapeHtml(p.name ?? '')
          if (privacyMode) return `${label} ${formatPercent(pct)}`
          return `${label}<br>${formatCurrencyFull(p.value)} (${formatPercent(pct)})`
        },
      },
      series: [{
        type: 'sankey',
        emphasis: { focus: 'adjacency' },
        label: {
          formatter: (params: unknown) => {
            const p = params as { data?: { name?: string } }
            return escapeHtml(p.data?.name ?? '')
          },
        },
        lineStyle: { color: 'gradient', opacity: 0.5 },
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        data: sankeyData.nodes as any,
        links: sankeyData.links,
      }],
    }
  }, [sankeyData, privacyMode])

  if (loading) {
    return (
      <div className={className} aria-busy="true" aria-label="Loading cash flow chart" data-testid="sankey-chart">
        <div className="h-64 animate-pulse rounded-lg bg-muted" />
      </div>
    )
  }

  if (sankeyData.nodes.length === 0) {
    return (
      <div className={className} role="status" data-testid="sankey-chart">
        <p className="text-sm text-muted-foreground">No cash flow data for this period.</p>
      </div>
    )
  }

  return (
    <div className={className} data-testid="sankey-chart">
      <EChartsChart
        option={option}
        isDark={isDark}
        style={{ width: '100%', height: '400px' }}
        ariaLabel="Cash flow diagram showing income sources and expense categories"
        onEvents={{ click: handleClick }}
      />
      {/* sr-only table: screen reader access + keyboard fallback. data-ph-mask covers amounts. */}
      <table className="sr-only" aria-label="Cash flow data table" data-ph-mask>
        <caption>Cash flow breakdown for the selected period</caption>
        <thead>
          <tr>
            <th scope="col">Flow</th>
            <th scope="col">Amount</th>
            <th scope="col">Percentage</th>
          </tr>
        </thead>
        <tbody>
          {sankeyData.links.map((link, i) => {
            const sourceNode = sankeyData.nodes.find(n => n.id === link.source)
            const targetNode = sankeyData.nodes.find(n => n.id === link.target)
            const pct = sankeyData.hubTotal > 0 ? (link.value / sankeyData.hubTotal) * 100 : 0
            const navigableNode = targetNode?.navigable ? targetNode : sourceNode?.navigable ? sourceNode : undefined
            const label = `${sourceNode?.name ?? link.source} → ${targetNode?.name ?? link.target}`
            return (
              <tr key={i}>
                <td>
                  {navigableNode ? (
                    <button
                      type="button"
                      className="focus-visible:not-sr-only focus-visible:absolute focus-visible:z-50 focus-visible:rounded focus-visible:bg-background focus-visible:px-2 focus-visible:py-1 focus-visible:text-sm focus-visible:shadow-md focus-visible:ring-2 focus-visible:ring-ring"
                      onClick={() => handleClick({ dataType: 'node', data: navigableNode })}
                    >
                      {label}
                    </button>
                  ) : (
                    <span>{label}</span>
                  )}
                </td>
                <td>{formatCurrencyFull(link.value)}</td>
                <td>{formatPercent(pct)}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
