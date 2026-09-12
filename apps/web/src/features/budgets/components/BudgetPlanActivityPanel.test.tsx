import { describe, expect, it } from 'bun:test'
import { fireEvent, render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router'
import type { BudgetPlanVsActualResponse } from '@boostedbudget/api-client/models'
import BudgetPlanActivityPanel from './BudgetPlanActivityPanel'

const report: BudgetPlanVsActualResponse = {
  budget_id: 'budget-1', as_of: '2026-09-12T12:00:00Z', period: 'monthly', currency_code: 'USD',
  eligibility: 'eligible', completeness: 'complete', reasons: [], period_start: '2026-09-01', period_end: '2026-09-30', actual_through: '2026-09-12',
  income: { expected: 1000, observed: 500, manual_declared: 200, reasons: [] },
  totals: { allocated: 300, unallocated: 700, overallocated: 0, actual: 400, unbudgeted: 0 },
  lines: [{ id: 'group-1', budget_category_id: 'group-1', savings_goal_id: null, kind: 'expense', label: 'Daily life', planned: 300, actual: 400, variance: 100, completeness: 'complete', reasons: [] }],
}

describe('BudgetPlanActivityPanel', () => {
  function renderPanel(data = report) {
    return render(<QueryClientProvider client={new QueryClient()}><MemoryRouter><BudgetPlanActivityPanel data={data} onRefresh={() => {}} /></MemoryRouter></QueryClientProvider>)
  }
  it('provides a privacy toggle and opens the matching allocation activity dialog', () => {
    renderPanel()
    fireEvent.click(screen.getByRole('button', { name: 'Hide amounts' }))
    expect(screen.getByText('Chart hidden in privacy mode.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'View allocation 1 activity' })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Show amounts' }))
    fireEvent.click(screen.getByRole('button', { name: 'View allocation 1 activity' }))
    expect(screen.getByRole('dialog', { name: 'Allocation activity' })).toBeInTheDocument()
    expect(screen.getByText(/Posted spending from 2026-09-01 through 2026-09-12/)).toBeInTheDocument()
  })
})
