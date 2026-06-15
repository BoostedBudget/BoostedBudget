// Issue #1300: InsightsPage sub-tabs + URL state tests
// API hooks are mocked via bunfig.toml preloads (analytics-api.ts, net-worth-hooks.ts,
// track-event.ts). Do NOT add mock.module() here — it leaks across test files in bun:test.
import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import React from 'react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'

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

  it('defaults to Spending tab', () => {
    renderInsightsPage()
    const tab = screen.getByRole('tab', { name: 'Spending' })
    expect(tab).toBeDefined()
    expect(tab.getAttribute('data-state')).toBe('active')
  })

  it('activates Net Worth tab when ?tab=net-worth', () => {
    renderInsightsPage('?tab=net-worth')
    const tab = screen.getByRole('tab', { name: 'Net Worth' })
    expect(tab).toBeDefined()
    expect(tab.getAttribute('data-state')).toBe('active')
  })

  it('renders both Spending and Net Worth tab triggers', () => {
    renderInsightsPage()
    expect(screen.getByRole('tab', { name: 'Spending' })).toBeDefined()
    expect(screen.getByRole('tab', { name: 'Net Worth' })).toBeDefined()
  })
})
