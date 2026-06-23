// Issue #1300: InsightsPage sub-tabs + URL state tests
// API hooks are mocked via bunfig.toml preloads (analytics-api.ts, net-worth-hooks.ts,
// track-event.ts, investments-api.ts).
// IMPORTANT: Do NOT add ad-hoc mock.module() calls here for hooks other than useTrackEvent
// — they leak globally across test files in bun:test. The track-event re-registration
// below is intentional: it pins useTrackEvent to the canonical mockTrackEvent reference
// so analytics assertions work regardless of file execution order (pattern from
// PortfolioPage.test.tsx / BudgetDetailPage.analytics.test.tsx).
// QueryClientProvider is required because PortfolioPage uses useInfiniteInvestmentTransactions
// (real useInfiniteQuery under the mocked getInvestmentTransactions fetcher).
import { describe, it, expect, mock } from 'bun:test'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import React from 'react'
import { MemoryRouter, Routes, Route, useLocation as useRouterLocation } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { mockTrackEvent } from '@/test/mocks/track-event'

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

  it('clicking Portfolio tab writes ?tab=portfolio to the URL', async () => {
    const locationRef = { current: '' }
    function LocationProbe() {
      const { search } = useRouterLocation()
      locationRef.current = search
      return null
    }

    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/insights']}>
          <LocationProbe />
          <Routes>
            <Route path="/insights" element={<InsightsPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>
    )

    await userEvent.click(screen.getByRole('tab', { name: 'Portfolio' }))

    expect(locationRef.current).toBe('?tab=portfolio')
  })

  // R8-3: Insights analytics events must be fired and testable.
  describe('analytics', () => {
    it('fires insights_page_viewed exactly once on mount (ref-guarded)', () => {
      mockTrackEvent.mockClear()
      renderInsightsPage()
      const pageViewedCalls = mockTrackEvent.mock.calls.filter(
        (c: unknown[]) => c[0] === 'insights_page_viewed',
      )
      expect(pageViewedCalls.length).toBe(1)
      // Payload includes the active tab (defaults to 'spending')
      expect(pageViewedCalls[0][1]).toMatchObject({ tab: 'spending' })
    })

    it('fires insights_tab_changed with the correct tab payload when switching tabs', async () => {
      mockTrackEvent.mockClear()
      renderInsightsPage()

      await userEvent.click(screen.getByRole('tab', { name: 'Portfolio' }))

      const tabChangedCalls = mockTrackEvent.mock.calls.filter(
        (c: unknown[]) => c[0] === 'insights_tab_changed',
      )
      expect(tabChangedCalls.length).toBeGreaterThanOrEqual(1)
      expect(tabChangedCalls[0][1]).toMatchObject({ tab: 'portfolio' })
    })
  })
})
