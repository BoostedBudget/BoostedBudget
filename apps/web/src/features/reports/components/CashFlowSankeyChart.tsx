import { useMemo } from 'react'
import { useNavigate } from 'react-router'
import type { EChartsOption } from 'echarts'
import type { CategoryHierarchyResponse } from '@boostedbudget/api-client/models'
import {
  EChartsChart,
  useDarkMode,
  formatCurrencyFull,
  formatPercent,
  escapeHtml,
} from '@/shared/charts'
import { buildSankeyData } from './buildSankeyData'
import type { NodeAction, SankeyNode, SankeyViewMode } from './buildSankeyData'

/**
 * A mixed subtree emits one filter value per descendant custom category. Capped so a user with
 * hundreds of them cannot produce a multi-KB query string — which session replay records verbatim.
 * Past the cap the node focuses instead, which is lossless: the same categories are still reachable
 * one level down.
 */
const MAX_NAV_FILTER_VALUES = 25

interface CashFlowSankeyChartProps {
  incomeData: CategoryHierarchyResponse | null
  expenseData: CategoryHierarchyResponse | null
  loading: boolean
  privacyMode: boolean
  startDate: string
  endDate: string
  viewMode: SankeyViewMode
  onFocus: (key: string) => void
  className?: string
}

/** Builds the /transactions query string for a navigate action. */
function navParams(action: Extract<NodeAction, { kind: 'navigate' }>, direction: 'income' | 'expense', startDate: string, endDate: string) {
  const params = new URLSearchParams()
  if (action.pfcPrimaries?.length) params.set('pfc_primaries', action.pfcPrimaries.join(','))
  if (action.pfcDetaileds?.length) params.set('pfc_detaileds', action.pfcDetaileds.join(','))
  if (action.customCategoryIds?.length) params.set('custom_category_ids', action.customCategoryIds.join(','))
  params.set('amount_type', direction)
  if (startDate) params.set('start_date', startDate)
  if (endDate) params.set('end_date', endDate)
  return params
}

function totalFilterValues(action: Extract<NodeAction, { kind: 'navigate' }>) {
  return (action.pfcPrimaries?.length ?? 0) + (action.pfcDetaileds?.length ?? 0) + (action.customCategoryIds?.length ?? 0)
}

export function CashFlowSankeyChart({
  incomeData,
  expenseData,
  loading,
  privacyMode,
  startDate,
  endDate,
  viewMode,
  onFocus,
  className,
}: CashFlowSankeyChartProps) {
  const navigate = useNavigate()
  const isDark = useDarkMode()

  const sankeyData = useMemo(
    () => buildSankeyData(incomeData?.levels ?? [], expenseData?.levels ?? [], viewMode),
    [incomeData, expenseData, viewMode],
  )

  const activate = (node: SankeyNode | undefined) => {
    if (!node) return
    const { action } = node
    if (action.kind === 'none') return
    if (action.kind === 'focus') {
      onFocus(action.key)
      return
    }
    // A subtree wide enough to blow up the URL focuses instead — the categories stay reachable.
    if (totalFilterValues(action) > MAX_NAV_FILTER_VALUES) {
      onFocus(node.id)
      return
    }
    const direction = node.nodeKind === 'income' ? 'income' : 'expense'
    navigate(`/transactions?${navParams(action, direction, startDate, endDate).toString()}`)
  }

  const handleClick = (params: unknown) => {
    const p = params as { dataType?: string; data?: unknown; dataIndex?: number }
    const node = p.dataType === 'node' && p.data && typeof (p.data as SankeyNode).id === 'string'
      ? (p.data as SankeyNode)
      : !p.dataType && typeof p.dataIndex === 'number'
      ? sankeyData.nodes[p.dataIndex] // dataIndex-only: test mock path; real ECharts always sets dataType
      : undefined
    activate(node)
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
        style={{ width: '100%', height: '460px' }}
        ariaLabel="Cash flow diagram showing income sources and expense categories"
        onEvents={{ click: handleClick }}
      />
      {/* sr-only table: screen reader access + keyboard fallback. data-ph-mask covers amounts.
          Node names are rendered as TEXT, never interpolated into an aria-label or title — session
          replay records attribute values verbatim and data-ph-mask cannot cover them. */}
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
            // Prefer the TARGET, which is the category the row is about. The source is used only
            // for links flowing INTO the hub — the income rows, where the source is the category
            // and the hub is not actionable. Falling back to the source generally would make an
            // inert row (a "(direct)" bucket, an overflow node) act on its PARENT instead, which
            // duplicates a row that already exists and acts on something the label does not name.
            const actionable = targetNode && targetNode.action.kind !== 'none'
              ? targetNode
              : link.target === 'hub' && sourceNode && sourceNode.action.kind !== 'none'
              ? sourceNode
              : undefined
            const label = `${sourceNode?.name ?? link.source} → ${targetNode?.name ?? link.target}`
            return (
              <tr key={i}>
                <td>
                  {actionable ? (
                    <button
                      type="button"
                      className="focus-visible:not-sr-only focus-visible:absolute focus-visible:z-50 focus-visible:rounded focus-visible:bg-background focus-visible:px-2 focus-visible:py-1 focus-visible:text-sm focus-visible:shadow-md focus-visible:ring-2 focus-visible:ring-ring"
                      onClick={() => activate(actionable)}
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
