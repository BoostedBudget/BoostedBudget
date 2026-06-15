// Issue #1300: InsightsPage sub-tabs + URL state tests
// API hooks are mocked via bunfig.toml preloads (analytics-api.ts, net-worth-hooks.ts,
// track-event.ts, investments-api.ts). Do NOT add mock.module() here — it leaks across
// test files in bun:test.
// QueryClientProvider is required because PortfolioPage uses useInfiniteInvestmentTransactions
// (real useInfiniteQuery under the mocked getInvestmentTransactions fetcher).
import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import React from 'react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

import { InsightsPage } from './InsightsPage'

function renderInsightsPage(search = '') {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[`/insights${search}`]}>
        <Routes>
          <Route path="/insights" element={<InsightsPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
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

  it('renders Portfolio tab trigger', () => {
    renderInsightsPage()
    expect(screen.getByRole('tab', { name: 'Portfolio' })).toBeDefined()
  })

  it('activates Portfolio tab when ?tab=portfolio', () => {
    renderInsightsPage('?tab=portfolio')
    const tab = screen.getByRole('tab', { name: 'Portfolio' })
    expect(tab).toBeDefined()
    expect(tab.getAttribute('data-state')).toBe('active')
  })

  it('clicking Portfolio tab updates URL and activates the Portfolio tab', async () => {
    // Start at default (Spending tab) — no ?tab param
    renderInsightsPage()
    const portfolioTab = screen.getByRole('tab', { name: 'Portfolio' })
    expect(portfolioTab.getAttribute('data-state')).toBe('inactive')

    // useTabState writes ?tab=portfolio to MemoryRouter history on click
    await userEvent.click(portfolioTab)

    expect(portfolioTab.getAttribute('data-state')).toBe('active')
    // Spending and Net Worth should no longer be active
    expect(screen.getByRole('tab', { name: 'Spending' }).getAttribute('data-state')).toBe('inactive')
  })
})
