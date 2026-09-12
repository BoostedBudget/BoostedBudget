import { useMemo, useRef } from 'react'
import type { EChartsOption } from 'echarts'
import { EChartsChart, useDarkMode, CATEGORY_COLORS, escapeHtml } from '@/shared/charts'
import { Button } from '@/components/ui/button'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import { buildBudgetFlow, type BudgetFlowLine } from './buildBudgetFlow'

export interface BudgetPlanFlowData {
  currency_code: string
  eligibility: string
  completeness: string
  reasons: string[]
  period_start: string | null
  period_end: string | null
  actual_through: string | null
  income: { expected: number; observed: number | null; manual_declared: number }
  lines: (BudgetFlowLine & { budget_category_id: string | null; savings_goal_id: string | null; variance: number | null; reasons: string[] })[]
}

interface Props {
  data: BudgetPlanFlowData
  privacyMode?: boolean
  onSelectLine: (line: BudgetPlanFlowData['lines'][number]) => void
}

const REASON_TEXT: Record<string, string> = {
  budget_inactive: 'This budget is inactive. Only the plan is shown.',
  period_not_started: 'This budget period has not started.',
  period_ended: 'This budget period has ended. Historical configuration snapshots are not available.',
  legacy_savings_origin: 'Historical savings entries may include opening balances, so their activity is unavailable.',
  ambiguous_savings_generation: 'Archived and recreated savings entries cannot be distinguished reliably.',
  savings_allocation_exceeds_transaction: 'Savings allocations exceed the linked transaction amount.',
  overlapping_individual_lines: 'Some individual lines match the same spending. Actual flows cannot be combined reliably.',
  mixed_currency: 'Activity includes different currencies. No currency conversion is applied.',
  unknown_currency: 'Some activity has no verifiable currency.',
  invalid_manual_income: 'A manual income declaration could not be read.',
}

export function BudgetPlanFlowChart({ data, privacyMode = false, onSelectLine }: Props) {
  const isDark = useDarkMode()
  const trackEvent = useTrackEvent()
  const tableRef = useRef<HTMLTableElement>(null)
  const showActuals = data.eligibility === 'eligible' && data.completeness === 'complete' && data.lines.every((line) => line.actual != null)
  const graph = useMemo(() => buildBudgetFlow(data.income.expected, data.lines, showActuals), [data.income.expected, data.lines, showActuals])
  const allocated = data.lines.reduce((sum, line) => sum + line.planned, 0)
  const actualTotal = showActuals ? data.lines.reduce((sum, line) => sum + (line.actual ?? 0), 0) : null
  const money = (amount: number | null) => privacyMode ? 'Hidden' : amount == null ? 'Unavailable' : new Intl.NumberFormat('en-US', { style: 'currency', currency: data.currency_code }).format(amount)
  const canSelect = (line: BudgetPlanFlowData['lines'][number]) => data.eligibility === 'eligible' && !privacyMode &&
    (line.kind !== 'savings' || !line.reasons.some((reason) => reason === 'mixed_currency' || reason === 'unknown_currency'))
  const select = (line: BudgetPlanFlowData['lines'][number]) => {
    if (!canSelect(line)) return
    trackEvent(ANALYTICS_EVENTS.BUDGET_PLAN_LINE_OPENED, { kind: line.kind })
    onSelectLine(line)
  }
  const showAll = () => {
    trackEvent(ANALYTICS_EVENTS.BUDGET_PLAN_FULL_TABLE_VIEWED, {
      hidden_allocation_count: graph.hiddenLineIds.length,
    })
    tableRef.current?.scrollIntoView?.({ block: 'nearest' })
    tableRef.current?.focus()
  }
  const option: EChartsOption = {
    animation: false,
    tooltip: {
      trigger: 'item',
      formatter: (params: unknown) => {
        const p = params as { data?: { id?: string }; value?: number }
        const label = graph.nodes.find((n) => n.id === p.data?.id)?.label ?? 'Allocation flow'
        return `${escapeHtml(label)}${typeof p.value === 'number' ? `<br>${escapeHtml(money(p.value))}` : ''}`
      },
    },
    series: [{
      type: 'sankey', nodeAlign: 'left', draggable: false,
      left: 16, right: 150, top: 20, bottom: 20, nodeGap: 14,
      emphasis: { focus: 'adjacency' },
      data: graph.nodes.map((node, index) => ({ id: node.id, name: node.id, itemStyle: { color: CATEGORY_COLORS[index % CATEGORY_COLORS.length] } })),
      links: graph.links,
      label: { color: isDark ? '#f9fafb' : '#1f2937', formatter: (params: { name: string }) => graph.nodes.find((n) => n.id === params.name)?.label ?? '' },
      lineStyle: { color: 'source', opacity: 0.35, curveness: 0.5 },
    }],
  }
  const handleClick = (params: unknown) => {
    const p = params as { dataType?: string; data?: { id?: string } }
    if (p.dataType !== 'node') return
    const node = graph.nodes.find((n) => n.id === p.data?.id)
    if (node?.id === 'allocation:more') { showAll(); return }
    const line = data.lines.find((l) => l.id === node?.lineId)
    if (line) select(line)
  }

  return (
    <section className="rounded-xl border border-border bg-card p-4 space-y-4" aria-labelledby="budget-plan-flow-heading">
      <h2 id="budget-plan-flow-heading" className="text-lg font-semibold">Budget Plan and Activity</h2>
      <p className="text-sm text-muted-foreground">{data.period_start && data.period_end ? `${data.period_start} to ${data.period_end}. ` : ''}{showActuals ? `Posted activity through ${data.actual_through}.` : 'Plan only'}</p>
      <p className="text-sm text-muted-foreground">This shows allocations and recorded activity, not money movement. Above-plan and shortfall flows are not received income.</p>
      <dl className="grid gap-3 sm:grid-cols-3">
        {([['Expected income', data.income.expected], ['Observed income', data.income.observed], ['Manual declaration', data.income.manual_declared]] as const).map(([label, amount]) => (
          <div key={label}><dt className="text-sm text-muted-foreground">{label}</dt><dd className="font-semibold" data-ph-mask>{money(amount)}</dd></div>
        ))}
      </dl>
      <dl className="grid gap-3 sm:grid-cols-3" aria-label="Budget plan balances">
        {([
          ['Allocated plan', allocated],
          ['Unallocated plan', Math.max(0, data.income.expected - allocated)],
          ['Over-allocation shortfall', Math.max(0, allocated - data.income.expected)],
          ['Total recorded activity', actualTotal],
          ['Unbudgeted spending', data.lines.find((line) => line.kind === 'unbudgeted')?.actual ?? (showActuals ? 0 : null)],
        ] as const).map(([label, amount]) => <div key={label}><dt className="text-sm text-muted-foreground">{label}</dt><dd data-ph-mask>{money(amount)}</dd></div>)}
      </dl>
      {data.reasons.length > 0 && <ul className="text-sm text-muted-foreground list-disc pl-5">{data.reasons.map((reason) => <li key={reason}>{REASON_TEXT[reason] ?? 'Some activity could not be verified.'}</li>)}</ul>}
      {privacyMode ? <p>Chart hidden in privacy mode.</p> : graph.nodes.length > 0 ? (
        <div className="overflow-x-auto" data-ph-mask>
          <div className="min-w-[640px]">
            <EChartsChart option={option} isDark={isDark} ariaLabel="Budget allocation flow; equivalent values and controls follow in the table" style={{ height: Math.max(360, Math.min(760, graph.nodes.length * 18)) }} onEvents={{ click: handleClick }} notMerge />
          </div>
        </div>
      ) : <p>No allocations yet. Add a category, shared group, or savings goal to build your plan.</p>}
      {graph.hiddenLineIds.length > 0 && <Button variant="outline" onClick={showAll}>View full table</Button>}
      <div className="overflow-x-auto">
        <table ref={tableRef} tabIndex={-1} className="w-full text-sm">
          <caption className="text-left font-medium mb-2">Budget allocation and activity</caption>
          <thead><tr>{['Allocation', 'Planned', 'Actual', 'Above / below plan', 'Activity'].map((heading) => <th key={heading} scope="col" className="p-2 text-left">{heading}</th>)}</tr></thead>
          <tbody>{data.lines.map((line, index) => <tr key={line.id} className="border-t border-border">
            <th scope="row" className="p-2 text-left"><span data-ph-mask>{line.label}</span>{line.kind === 'savings' && <span className="block text-xs font-normal">Savings contribution</span>}</th>
            <td className="p-2" data-ph-mask>{money(line.planned)}</td>
            <td className="p-2" data-ph-mask>{money(line.actual)}</td>
            <td className="p-2" data-ph-mask>{money(line.variance)}</td>
            <td className="p-2">{canSelect(line) && <Button variant="ghost" onClick={() => select(line)} aria-label={`View allocation ${index + 1} ${line.kind === 'savings' ? 'full savings history' : 'activity'}`}>{line.kind === 'savings' ? 'Full history' : 'View'}</Button>}
              {line.reasons.map((reason) => <p key={reason} className="text-xs text-muted-foreground">{REASON_TEXT[reason] ?? 'Some activity could not be verified.'}</p>)}
            </td>
          </tr>)}</tbody>
        </table>
      </div>
    </section>
  )
}
