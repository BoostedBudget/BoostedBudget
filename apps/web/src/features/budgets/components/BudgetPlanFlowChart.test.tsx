import { beforeEach, describe, expect, it, mock } from 'bun:test'
import { fireEvent, render, screen, within } from '@testing-library/react'
import { MockEChartsChart } from '@/test/mocks/shared-charts'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { BudgetPlanFlowChart, type BudgetPlanFlowData } from './BudgetPlanFlowChart'

const data: BudgetPlanFlowData = {
  currency_code: 'USD', eligibility: 'eligible', completeness: 'complete', reasons: [],
  period_start: '2026-09-01', period_end: '2026-09-30', actual_through: '2026-09-12',
  income: { expected: 1000, observed: 500, manual_declared: 200 },
  lines: [{ id: 'food', budget_category_id: 'food', savings_goal_id: null, label: 'Daily life', kind: 'expense', planned: 300, actual: 400, variance: 100, reasons: [] },
    { id: 'save', budget_category_id: 'save', savings_goal_id: 'goal', label: 'Rainy day', kind: 'savings', planned: 200, actual: 100, variance: -100, reasons: [] }],
}

describe('BudgetPlanFlowChart', () => {
  beforeEach(() => {
    MockEChartsChart.mockClear()
    mockTrackEvent.mockClear()
  })

  it('shows separate planned, observed and declared income, with a complete accessible table', () => {
    render(<BudgetPlanFlowChart data={data} onSelectLine={() => {}} />)
    expect(screen.getByText('Observed income')).toBeInTheDocument()
    expect(screen.getByText('Manual declaration')).toBeInTheDocument()
    const table = screen.getByRole('table', { name: 'Budget allocation and activity' })
    expect(within(table).getByText('Daily life')).toHaveAttribute('data-ph-mask')
    expect(within(table).getByText('$400.00')).toHaveAttribute('data-ph-mask')
    expect(screen.getByText(/through 2026-09-12/)).toBeInTheDocument()
  })

  it('uses structural button labels and the stable line for drilldown', () => {
    const select = mock(() => {})
    render(<BudgetPlanFlowChart data={data} onSelectLine={select} />)
    fireEvent.click(screen.getByRole('button', { name: 'View allocation 1 activity' }))
    expect(select).toHaveBeenCalledWith(data.lines[0])
    expect(screen.queryByRole('button', { name: /Daily life/ })).toBeNull()
  })

  it('renders malformed persisted currency codes without breaking the table or tooltip', () => {
    render(<BudgetPlanFlowChart data={{ ...data, currency_code: 'U$D' }} onSelectLine={() => {}} />)
    const table = screen.getByRole('table', { name: 'Budget allocation and activity' })
    expect(within(table).getByText('U$D 400.00')).toHaveAttribute('data-ph-mask')
    const props = MockEChartsChart.mock.calls.at(-1)?.[0] as unknown as {
      option: { tooltip: { formatter: (params: unknown) => string } }
    }
    expect(props.option.tooltip.formatter({ data: { id: 'allocation:food' }, value: 400 })).toContain('U$D 400.00')
  })

  it.each([['EUR', '€400.00'], ['JPY', '¥400']])('preserves valid %s currency formatting', (currency, expected) => {
    render(<BudgetPlanFlowChart data={{ ...data, currency_code: currency }} onSelectLine={() => {}} />)
    expect(within(screen.getByRole('table')).getByText(expected)).toHaveAttribute('data-ph-mask')
  })

  it('keeps unknown activity visibly unavailable instead of formatting zero', () => {
    render(<BudgetPlanFlowChart data={{ ...data, completeness: 'partial', reasons: ['legacy_savings_origin'], lines: [{ ...data.lines[1]!, actual: null, variance: null, reasons: ['legacy_savings_origin'] }] }} onSelectLine={() => {}} />)
    expect(screen.getByText(/Plan only/)).toBeInTheDocument()
    expect(screen.getAllByText('Unavailable').length).toBeGreaterThan(0)
    expect(screen.getAllByText(/Historical savings entries/).length).toBeGreaterThan(0)
  })

  it.each(['expense', 'unbudgeted'] as const)('does not offer or open unavailable %s activity', (kind) => {
    const select = mock(() => {})
    const line = { ...data.lines[0]!, kind, actual: null, variance: null, reasons: ['mixed_currency'] }
    render(<BudgetPlanFlowChart data={{ ...data, completeness: 'partial', lines: [line] }} onSelectLine={select} />)

    expect(screen.queryByRole('button', { name: 'View allocation 1 activity' })).toBeNull()
    expect(within(screen.getByRole('table')).getByText('Activity unavailable')).toBeInTheDocument()
    const props = MockEChartsChart.mock.calls.at(-1)?.[0] as unknown as { onEvents: { click: (event: unknown) => void } }
    props.onEvents.click({ dataType: 'node', data: { id: 'allocation:food' } })
    expect(select).not.toHaveBeenCalled()
    expect(mockTrackEvent).not.toHaveBeenCalled()
  })

  it('keeps known zero expense activity selectable in a partial report', () => {
    const select = mock(() => {})
    const line = { ...data.lines[0]!, actual: 0 }
    render(<BudgetPlanFlowChart data={{ ...data, completeness: 'partial', lines: [line] }} onSelectLine={select} />)
    fireEvent.click(screen.getByRole('button', { name: 'View allocation 1 activity' }))
    expect(select).toHaveBeenCalledWith(line)
  })

  it('hides monetary values in privacy mode, including chart options', () => {
    render(<BudgetPlanFlowChart data={data} privacyMode onSelectLine={() => {}} />)
    expect(screen.queryByText('$400.00')).toBeNull()
    expect(MockEChartsChart).not.toHaveBeenCalled()
    expect(screen.getByText('Chart hidden in privacy mode.')).toBeInTheDocument()
  })

  it('renders all allocations in the table even when the graph collapses them', () => {
    const lines = Array.from({ length: 15 }, (_, i) => ({ ...data.lines[0]!, id: `line${i}`, label: `Line ${i}` }))
    render(<BudgetPlanFlowChart data={{ ...data, lines }} onSelectLine={() => {}} />)
    expect(within(screen.getByRole('table')).getAllByRole('row')).toHaveLength(16)
    const button = screen.getByRole('button', { name: 'View full table' })

    fireEvent.click(button)

    expect(mockTrackEvent).toHaveBeenCalledWith('budget_plan_full_table_viewed', {
      hidden_allocation_count: 5,
    })
    expect(mockTrackEvent).toHaveBeenCalledTimes(1)
  })

  it('maps chart nodes to stable allocations and sends More to the complete table', () => {
    const select = mock(() => {})
    const lines = Array.from({ length: 15 }, (_, i) => ({ ...data.lines[0]!, id: `line${i}`, label: `Line ${i}` }))
    render(<BudgetPlanFlowChart data={{ ...data, lines }} onSelectLine={select} />)
    const props = MockEChartsChart.mock.calls.at(-1)?.[0] as unknown as { onEvents: { click: (event: unknown) => void } }
    props.onEvents.click({ dataType: 'node', data: { id: 'allocation:line0' } })
    expect(select).toHaveBeenCalledWith(lines[0])
    props.onEvents.click({ dataType: 'edge', data: { id: 'allocation:line0' } })
    expect(select).toHaveBeenCalledTimes(1)
    props.onEvents.click({ dataType: 'node', data: { id: 'allocation:more' } })
    expect(screen.getByRole('table')).toHaveFocus()
    expect(select).toHaveBeenCalledTimes(1)
  })

  it('does not offer current-period activity for an ineligible budget', () => {
    render(<BudgetPlanFlowChart data={{ ...data, eligibility: 'future', completeness: 'unavailable', reasons: ['period_not_started'] }} onSelectLine={() => {}} />)
    expect(screen.queryByRole('button', { name: 'View allocation 1 activity' })).toBeNull()
  })

  it('provides accessible plan balances and labels savings as full-history navigation', () => {
    render(<BudgetPlanFlowChart data={data} onSelectLine={() => {}} />)
    expect(screen.getByText('Unallocated plan')).toBeInTheDocument()
    expect(screen.getByText('Over-allocation shortfall')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'View allocation 2 full savings history' })).toBeInTheDocument()
  })
  it('does not open savings history under an unverified display currency', () => {
    render(<BudgetPlanFlowChart data={{ ...data, completeness: 'partial', lines: [{ ...data.lines[1]!, actual: null, variance: null, reasons: ['mixed_currency'] }] }} onSelectLine={() => {}} />)
    expect(screen.queryByRole('button', { name: 'View allocation 1 full savings history' })).toBeNull()
  })
})
