// Issue #1300: InsightsPage sub-tabs + URL state tests
import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import React from 'react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'

// Stub child pages and analytics (must come before imports that use them)
mock.module('../reports', () => ({
  SpendingReportsPage: () => React.createElement('div', { 'data-testid': 'spending-page' }, 'Spending Content'),
}))
mock.module('../net-worth', () => ({
  NetWorthPage: () => React.createElement('div', { 'data-testid': 'net-worth-page' }, 'Net Worth Content'),
}))
mock.module('@/shared/hooks/useTrackEvent', () => ({
  useTrackEvent: mock(() => mock(() => {})),
}))

import { InsightsPage } from './InsightsPage'

function renderInsightsPage(search = '') {
  return render(
    <MemoryRouter initialEntries={[`/insights${search}`]}>
      <Routes>
        <Route path="/insights" element={<InsightsPage />} />
      </Routes>
    </MemoryRouter>
  )
}

describe('InsightsPage', () => {
  beforeEach(() => {
    mock.restore()
  })

  it('renders the Insights heading', () => {
    renderInsightsPage()
    expect(screen.getByRole('heading', { name: /insights/i })).toBeDefined()
  })

  it('defaults to Spending tab and shows spending content', () => {
    renderInsightsPage()
    expect(screen.getByTestId('spending-page')).toBeDefined()
  })

  it('shows Net Worth content when ?tab=net-worth', () => {
    renderInsightsPage('?tab=net-worth')
    expect(screen.getByTestId('net-worth-page')).toBeDefined()
  })

  it('renders both Spending and Net Worth tab triggers', () => {
    renderInsightsPage()
    expect(screen.getByRole('tab', { name: 'Spending' })).toBeDefined()
    expect(screen.getByRole('tab', { name: 'Net Worth' })).toBeDefined()
  })
})
