/**
 * PortfolioPage Tests
 * Issue #1296: Portfolio tab orchestrator.
 * Tests: loading, error, empty-state (PlaidLinkButton), and data states.
 */

import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
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
})
