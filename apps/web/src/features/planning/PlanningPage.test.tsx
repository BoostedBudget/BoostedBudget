// Issue #1300: PlanningPage sub-tabs + URL state tests
import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import React from 'react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'

// Stub child pages and analytics (must come before imports that use them)
mock.module('../budgets', () => ({
  BudgetsPage: () => React.createElement('div', { 'data-testid': 'budgets-page' }, 'Budgets Content'),
}))
mock.module('../savings', () => ({
  SavingsPage: () => React.createElement('div', { 'data-testid': 'savings-page' }, 'Savings Content'),
}))
mock.module('@/shared/hooks/useTrackEvent', () => ({
  useTrackEvent: mock(() => mock(() => {})),
}))

import { PlanningPage } from './PlanningPage'

function renderPlanningPage(search = '') {
  return render(
    <MemoryRouter initialEntries={[`/planning${search}`]}>
      <Routes>
        <Route path="/planning" element={<PlanningPage />} />
      </Routes>
    </MemoryRouter>
  )
}

describe('PlanningPage', () => {
  beforeEach(() => {
    mock.restore()
  })

  it('renders the Planning heading', () => {
    renderPlanningPage()
    expect(screen.getByRole('heading', { name: /planning/i })).toBeDefined()
  })

  it('defaults to Budgets tab and shows budgets content', () => {
    renderPlanningPage()
    expect(screen.getByTestId('budgets-page')).toBeDefined()
  })

  it('shows Savings content when ?tab=savings', () => {
    renderPlanningPage('?tab=savings')
    expect(screen.getByTestId('savings-page')).toBeDefined()
  })

  it('renders both Budgets and Savings tab triggers', () => {
    renderPlanningPage()
    expect(screen.getByRole('tab', { name: 'Budgets' })).toBeDefined()
    expect(screen.getByRole('tab', { name: 'Savings' })).toBeDefined()
  })
})
