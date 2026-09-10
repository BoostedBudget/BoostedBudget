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
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import { buildSankeyData } from './buildSankeyData'
import type { NodeAction, SankeyNode, SankeyViewMode } from './buildSankeyData'

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
  // #1626 Phase 2. The uncategorized sentinel has no category value to send — see NodeAction.
  if (action.uncategorized) params.set('uncategorized', 'true')
  params.set('amount_type', direction)
  // The Sankey aggregates POSTED spend only; the transactions list includes pending rows by
  // default. Without pinning this, the node's amount and the list's total would differ by exactly
  // the pending rows — and the click-through claim is what Phase 2 exists to make true rather than
  // approximately true.
  //
  // ⚠ The claim is |node amount| == |total_amount|, NOT plain equality. A node's amount is an ABS
  // sum (an unsigned chart quantity); the list's total_amount is SIGNED like its rows (PFCv2:
  // expenses positive, inflows negative), so for an INCOME node the two differ by a factor of −1.
  // Both are correct for their own consumer; a consumer comparing them must compare magnitudes.
  params.set('pending', 'false')
  if (startDate) params.set('start_date', startDate)
  if (endDate) params.set('end_date', endDate)
  return params
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
  const trackEvent = useTrackEvent()

  const sankeyData = useMemo(
    () => buildSankeyData(incomeData?.levels ?? [], expenseData?.levels ?? [], viewMode),
    [incomeData, expenseData, viewMode],
  )

  const activate = (node: SankeyNode | undefined) => {
    if (!node) return
    const { action } = node
    if (action.kind === 'none') return
    if (action.kind === 'focus') {
      // Structure only — the node's DEPTH, never the category's name, id or amount: event
      // properties are stored verbatim and are not covered by data-ph-mask.
      //
      // ⚠ From node.depth, not from splitting the id on ':' — the same rule events.ts states for
      // the navigate event 13 lines below, and for the same reason: the income fan is flattened to
      // `in:l1:*` whatever a leaf's real depth, so an id-derived level is a plausible-looking
      // number that stops being true the moment anything but an expense node can focus.
      trackEvent(ANALYTICS_EVENTS.REPORT_SANKEY_CATEGORY_FOCUSED, { depth: node.categoryDepth })
      onFocus(action.key)
      return
    }
    // No filter-value cap is needed: only LEAVES navigate, and a leaf resolves to exactly ONE
    // filter value (one custom id, one detailed enum, or one primary). Parents focus instead, so
    // there is no path that accumulates a descendant's ids into a long query string. An earlier
    // draft carried a cap for that case; the action model made it unreachable, and an unreachable
    // guard reads as protection that is not there.
    const direction = node.nodeKind === 'income' ? 'income' : 'expense'
    // Structure only — depth and direction. ⚠ depth comes from the NODE, never from splitting its
    // id: the income fan is flattened to `in:l1:*` whatever a leaf's real depth, so reading the id
    // would report every income navigation as level 1. Same PII rule as the focus event above.
    trackEvent(ANALYTICS_EVENTS.REPORT_SANKEY_NODE_NAVIGATED, { depth: node.categoryDepth, direction })
    navigate(`/transactions?${navParams(action, direction, startDate, endDate).toString()}`)
  }

  const handleClick = (params: unknown) => {
    const p = params as { dataType?: string; data?: unknown; dataIndex?: number }
    // ⚠ Do NOT trust the shape of `p.data` beyond its `id` — the chart option below projects our
    // node objects down to `id`/`name`/`itemStyle` before handing them to ECharts (see the `data:`
    // assignment in the `option` memo), specifically so that no internal field (action, nodeKind,
    // categoryDepth — nor any field added later) can leak into ECharts' chart config the way
    // `categoryDepth`'s predecessor once did as a layout directive. ECharts hands that same
    // projected object back in click params, so we resolve the REAL node from `sankeyData.nodes`
    // by id rather than casting `p.data` to `SankeyNode` — the id is the only field guaranteed to
    // survive the projection.
    const clickedId = p.dataType === 'node' && p.data && typeof (p.data as { id?: unknown }).id === 'string'
      ? (p.data as { id: string }).id
      : undefined
    const node = clickedId !== undefined
      ? sankeyData.nodes.find(n => n.id === clickedId)
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
        // Project explicitly — id/name/itemStyle only. ECharts' sankey series reads several
        // node-data field names as layout/style directives (depth, value, label, emphasis,
        // levels, …), so handing it our internal SankeyNode objects (action, nodeKind,
        // categoryDepth) risks any of them colliding with an ECharts directive, silently and
        // invisibly — which is exactly how `categoryDepth`'s predecessor field, named `depth`,
        // once became ECharts' layout LAYER and inverted this chart's rendering. See
        // `.superpowers/sdd/brainstorming-writing-plans-using-super-fancy-melody/sankey-root-cause.md`.
        data: sankeyData.nodes.map(n => ({ id: n.id, name: n.name, itemStyle: n.itemStyle })),
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
