/**
 * InvestmentTransactionsTable Component Tests
 * Issue #1296: Portfolio tab — investment transactions DataTable with infinite scroll.
 * Tests: loading state, scroll container structure, hasNextPage sentinel, param forwarding.
 *
 * Uses the REAL useInfiniteInvestmentTransactions hook with a mocked API fetcher
 * (mockGetInvestmentTransactions). No mock.module() on the hook itself — that would
 * be process-global and contaminate useInfiniteInvestmentTransactions.test.tsx which
 * needs the real hook. A QueryClientProvider wrapper is required for useInfiniteQuery.
 *
 * NOTE: TanStack Virtualizer measures offsetHeight=0 in jsdom — row text is not visible.
 * Tests assert on structural elements (virtual-scroll-container, sentinel, loading spinner).
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { InvestmentTransactionsTable } from './InvestmentTransactionsTable'
import { mockGetInvestmentTransactions } from '@/test/mocks/investments-api'
import type { InvestmentTransaction } from '@boostedbudget/api-client/models/investmentTransaction'

function createWrapper() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

const mockTransaction: InvestmentTransaction = {
  id: 't1',
  account_id: 'a1',
  user_id: 'u1',
  date: '2024-01-15',
  type: 'buy',
  name: 'AAPL',
  amount: 2500,
  quantity: 10,
  price: 250,
  fees: 0,
  plaid_investment_transaction_id: 'plaid-1',
  created_at: '2024-01-15T00:00:00Z',
}

const defaultDateRange = { start_date: '2024-01-01', end_date: '2024-12-31' }

describe('InvestmentTransactionsTable', () => {
  beforeEach(() => {
    mockGetInvestmentTransactions.mockClear()
    mockGetInvestmentTransactions.mockResolvedValue({
      transactions: [],
      limit: 50,
      next_cursor: undefined,
    })
  })

  it('shows loading state before data loads', () => {
    // Real useInfiniteQuery starts in loading state immediately (before promise resolves)
    const { container } = render(<InvestmentTransactionsTable dateRange={defaultDateRange} />, {
      wrapper: createWrapper(),
    })
    const statusEl =
      container.querySelector('[role="status"]') ?? screen.queryByRole('status', { hidden: true })
    expect(statusEl).not.toBeNull()
  })

  it('renders virtual scroll container and table after loading', async () => {
    const { container } = render(<InvestmentTransactionsTable dateRange={defaultDateRange} />, {
      wrapper: createWrapper(),
    })
    await waitFor(() => {
      expect(container.querySelector('[role="status"]')).toBeNull()
    })
    expect(container.querySelector('[data-testid="virtual-scroll-container"]')).not.toBeNull()
    expect(container.querySelector('table')).not.toBeNull()
  })

  it('renders Load More sentinel when API returns next_cursor', async () => {
    mockGetInvestmentTransactions.mockResolvedValue({
      transactions: [mockTransaction],
      limit: 50,
      next_cursor: 'cursor1',
    })
    const { container } = render(<InvestmentTransactionsTable dateRange={defaultDateRange} />, {
      wrapper: createWrapper(),
    })
    await waitFor(() => {
      const sentinelEl = container.querySelector('[data-testid="virtual-sentinel"]')
      expect(sentinelEl).not.toBeNull()
    })
  })

  it('calls getInvestmentTransactions with correct dateRange params', async () => {
    render(<InvestmentTransactionsTable dateRange={defaultDateRange} />, {
      wrapper: createWrapper(),
    })
    await waitFor(() => expect(mockGetInvestmentTransactions.mock.calls.length).toBeGreaterThan(0))
    const callParams = mockGetInvestmentTransactions.mock.calls[0][0] as Record<string, unknown>
    expect(callParams.start_date).toBe('2024-01-01')
    expect(callParams.end_date).toBe('2024-12-31')
  })
})
