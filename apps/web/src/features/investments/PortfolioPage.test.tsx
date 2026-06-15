/**
 * PortfolioPage Tests
 * Issue #1296: Portfolio tab orchestrator.
 * Tests: loading, error, empty-state (PlaidLinkButton), and data states.
 */

import { describe, it, expect, mock } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { mockTrackEvent } from '@/test/mocks/track-event'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { PortfolioPage } from './PortfolioPage'
import {
  mockUseGetPortfolioSummary,
  mockUseGetPortfolioHistory,
  mockUseGetHoldings,
  mockUseInfiniteInvestmentTransactions,
} from '@/test/mocks/investments-api'
import { mockUseGetPortfolioAllocationBreakdown } from '@/test/mocks/analytics-api'
import type { PortfolioSummary } from '@boostedbudget/api-client/models/portfolioSummary'
import type { GetPortfolioHistoryOutputBody } from '@boostedbudget/api-client/models/getPortfolioHistoryOutputBody'

// Re-register useTrackEvent with the preloaded mockTrackEvent reference so analytics
// assertions work regardless of file execution order (other files may override this mock).
// Pattern from BoostsPage.test.tsx / BudgetDetailPage.analytics.test.tsx.
mock.module('@/shared/hooks/useTrackEvent', () => ({
  useTrackEvent: () => mockTrackEvent,
}))

function createWrapper() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

const mockSummary: PortfolioSummary = {
  total_value: 52340.50,
  total_cost_basis: 48000.00,
  total_gain_loss: 4340.50,
  total_gain_loss_pct: 9.04,
  day_change: 230.15,
  day_change_pct: 0.44,
  holdings_count: 12,
}

const mockHistory: GetPortfolioHistoryOutputBody = {
  history: [
    { date: '2025-01-01', total_value: 50000 },
    { date: '2025-06-01', total_value: 52340 },
  ],
}

describe('PortfolioPage', () => {
  it('renders loading spinner when portfolio summary is loading', () => {
    mockUseGetPortfolioSummary.mockReturnValue({ data: undefined, isLoading: true, isError: false })
    render(<PortfolioPage />, { wrapper: createWrapper() })
    expect(screen.getByRole('status', { hidden: true })).toBeDefined()
  })

  it('renders error state when portfolio summary fails', () => {
    mockUseGetPortfolioSummary.mockReturnValue({ data: undefined, isLoading: false, isError: true })
    render(<PortfolioPage />, { wrapper: createWrapper() })
    // Should show some error indicator
    const errorElements = screen.queryAllByRole('alert', { hidden: true })
    expect(errorElements.length + screen.queryAllByText(/error|failed|unable/i).length).toBeGreaterThan(0)
  })

  it('renders empty state with link-account CTA when holdings_count is 0', () => {
    const emptySummary: PortfolioSummary = { ...mockSummary, holdings_count: 0 }
    mockUseGetPortfolioSummary.mockReturnValue({ data: emptySummary, isLoading: false, isError: false })
    render(<PortfolioPage />, { wrapper: createWrapper() })
    // EmptyState renders an h3 with the title
    expect(screen.getByText(/no.*(portfolio|investment|holding)/i)).toBeDefined()
    // PlaidLinkButton CTA must render (loading state shows "Preparing bank connection...")
    expect(
      screen.queryByText(/preparing bank connection|connect bank account/i)
    ).not.toBeNull()
  })

  it('renders portfolio content when holdings_count > 0', () => {
    mockUseGetPortfolioSummary.mockReturnValue({ data: mockSummary, isLoading: false, isError: false })
    mockUseGetPortfolioHistory.mockReturnValue({ data: mockHistory, isLoading: false, isError: false })
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({ data: undefined, isLoading: false, isError: false })
    mockUseGetHoldings.mockReturnValue({ data: undefined, isLoading: false, isError: false })
    mockUseInfiniteInvestmentTransactions.mockReturnValue({
      transactions: [], fetchNextPage: () => {}, hasNextPage: false,
      isFetchingNextPage: false, isLoading: false, isError: false, isSuccess: true,
    })

    render(<PortfolioPage />, { wrapper: createWrapper() })

    // Should not show empty state title "No portfolio data yet"
    expect(screen.queryByText(/no portfolio data/i)).toBeNull()
    // Should render a heading or the summary card label
    expect(screen.getByText(/total value/i)).toBeDefined()
  })

  it('renders time range selector', () => {
    mockUseGetPortfolioSummary.mockReturnValue({ data: mockSummary, isLoading: false, isError: false })
    mockUseGetPortfolioHistory.mockReturnValue({ data: undefined, isLoading: false, isError: false })
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({ data: undefined, isLoading: false, isError: false })
    mockUseGetHoldings.mockReturnValue({ data: undefined, isLoading: false, isError: false })
    mockUseInfiniteInvestmentTransactions.mockReturnValue({
      transactions: [], fetchNextPage: () => {}, hasNextPage: false,
      isFetchingNextPage: false, isLoading: false, isError: false, isSuccess: true,
    })

    render(<PortfolioPage />, { wrapper: createWrapper() })
    // 1Y preset should be visible by default
    expect(screen.getByText('1Y')).toBeDefined()
  })

  // R5-7: proves the full preset→dateRange→API seam. Clicking a preset must change the
  // {start_date,end_date} args forwarded to useGetPortfolioHistory — not just the preset label.
  it('changes dateRange args reaching useGetPortfolioHistory when preset changes', async () => {
    mockUseGetPortfolioSummary.mockReturnValue({ data: mockSummary, isLoading: false, isError: false })
    mockUseGetPortfolioHistory.mockReturnValue({ data: mockHistory, isLoading: false, isError: false })
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({ data: undefined, isLoading: false, isError: false })
    mockUseGetHoldings.mockReturnValue({ data: undefined, isLoading: false, isError: false })
    mockUseInfiniteInvestmentTransactions.mockReturnValue({
      transactions: [], fetchNextPage: () => {}, hasNextPage: false,
      isFetchingNextPage: false, isLoading: false, isError: false, isSuccess: true,
    })

    mockUseGetPortfolioHistory.mockClear()
    render(<PortfolioPage />, { wrapper: createWrapper() })

    // Capture the initial call args (default preset = 1Y).
    type HistoryArgs = { start_date: string; end_date: string }
    const initialArgs = mockUseGetPortfolioHistory.mock.calls[0]?.[0] as HistoryArgs
    expect(initialArgs?.start_date).toBeTruthy()
    expect(initialArgs?.end_date).toBeTruthy()

    // Click the 3M preset.
    await userEvent.click(screen.getByText('3M'))

    // The hook must be called again with a different (more recent) start_date.
    const calls = mockUseGetPortfolioHistory.mock.calls
    const lastArgs = calls[calls.length - 1]?.[0] as HistoryArgs
    expect(lastArgs?.start_date).toBeTruthy()
    expect(lastArgs.start_date).not.toBe(initialArgs.start_date) // range changed
    // 3M ago is more recent than 1Y ago — start_date string lexicographic comparison works for ISO dates.
    expect(lastArgs.start_date > initialArgs.start_date).toBe(true)
  })

  // R4-6: proves PORTFOLIO_RANGE_CHANGED fires with the correct preset when user changes range.
  it('fires portfolio_range_changed event when preset is changed', async () => {
    mockUseGetPortfolioSummary.mockReturnValue({ data: mockSummary, isLoading: false, isError: false })
    mockUseGetPortfolioHistory.mockReturnValue({ data: mockHistory, isLoading: false, isError: false })
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({ data: undefined, isLoading: false, isError: false })
    mockUseGetHoldings.mockReturnValue({ data: undefined, isLoading: false, isError: false })

    render(<PortfolioPage />, { wrapper: createWrapper() })

    mockTrackEvent.mockClear()

    await userEvent.click(screen.getByText('1M'))

    expect(mockTrackEvent).toHaveBeenCalledWith('portfolio_range_changed', { preset: '1M' })
  })
})
