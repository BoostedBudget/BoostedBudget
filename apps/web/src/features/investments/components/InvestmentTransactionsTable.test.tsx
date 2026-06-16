/**
 * InvestmentTransactionsTable Component Tests
 * Issue #1296: Portfolio tab — investment transactions DataTable with infinite scroll.
 * Uses mocked hook (no real React Query); hook tested separately in useInfiniteInvestmentTransactions.test.tsx.
 * Assert DataTable presence (loaded state), not absence of spinner (ResizeObserver async race).
 */

import { describe, it, expect, beforeEach, afterEach, mock } from 'bun:test'
import React from 'react'
import { render } from '@testing-library/react'
import { InvestmentTransactionsTable } from './InvestmentTransactionsTable'
import {
  mockUseInfiniteInvestmentTransactions,
  mockFetchNextInvestmentPage,
} from '@/test/mocks/investments-api'
import type { InvestmentTransaction } from '@boostedbudget/api-client/models/investmentTransaction'

// Capture DataTable props for maskedColumnIds assertion (R7-6)
let lastDataTableProps: Record<string, unknown> | null = null

mock.module('@/shared/components/DataTable', () => ({
  DataTable: (props: Record<string, unknown>) => {
    lastDataTableProps = props
    const virt = props.virtualization as { hasMore?: boolean } | undefined
    // Emit the same data-testids the real DataTable uses so existing tests pass.
    return React.createElement(
      'div',
      { 'data-testid': 'virtual-scroll-container' },
      React.createElement('table'),
      virt?.hasMore ? React.createElement('div', { 'data-testid': 'virtual-sentinel' }) : null,
    )
  },
  useTablePreferences: () => ({
    sizing: {},
    onSizingChange: () => {},
    columnOrder: [],
    onColumnOrderChange: () => {},
    columnVisibility: {},
    onColumnVisibilityChange: () => {},
  }),
}))

const mockTxn: InvestmentTransaction = {
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

function loadedResult(overrides: Record<string, unknown> = {}) {
  return {
    transactions: [],
    fetchNextPage: mockFetchNextInvestmentPage,
    hasNextPage: false,
    isFetchingNextPage: false,
    isLoading: false,
    isError: false,
    isSuccess: true,
    ...overrides,
  }
}

describe('InvestmentTransactionsTable', () => {
  beforeEach(() => {
    lastDataTableProps = null
    mockUseInfiniteInvestmentTransactions.mockClear()
    mockFetchNextInvestmentPage.mockClear()
    // Default: loaded, no transactions, no more pages
    mockUseInfiniteInvestmentTransactions.mockReturnValue(loadedResult())
  })

  it('shows loading spinner when isLoading is true', () => {
    mockUseInfiniteInvestmentTransactions.mockReturnValueOnce(
      loadedResult({ isLoading: true, isSuccess: false }),
    )
    const { container } = render(<InvestmentTransactionsTable dateRange={defaultDateRange} />)
    expect(container.querySelector('[role="status"]')).not.toBeNull()
  })

  it('renders virtual scroll container and table after loading', () => {
    mockUseInfiniteInvestmentTransactions.mockReturnValue(loadedResult())
    const { container } = render(<InvestmentTransactionsTable dateRange={defaultDateRange} />)
    expect(container.querySelector('[data-testid="virtual-scroll-container"]')).not.toBeNull()
    expect(container.querySelector('table')).not.toBeNull()
  })

  it('renders Load More sentinel when hasNextPage is true', () => {
    mockUseInfiniteInvestmentTransactions.mockReturnValue(
      loadedResult({ transactions: [mockTxn], hasNextPage: true }),
    )
    const { container } = render(<InvestmentTransactionsTable dateRange={defaultDateRange} />)
    expect(container.querySelector('[data-testid="virtual-sentinel"]')).not.toBeNull()
  })

  it('passes dateRange params to the hook', () => {
    render(<InvestmentTransactionsTable dateRange={defaultDateRange} />)
    const callParams = mockUseInfiniteInvestmentTransactions.mock.calls[0][0] as Record<
      string,
      unknown
    >
    expect(callParams.start_date).toBe('2024-01-01')
    expect(callParams.end_date).toBe('2024-12-31')
  })

  it('does not render Load More sentinel when hasNextPage is false', () => {
    const { container } = render(<InvestmentTransactionsTable dateRange={defaultDateRange} />)
    expect(container.querySelector('[data-testid="virtual-sentinel"]')).toBeNull()
  })

  it('wires onScrollEnd to fetchNextPage — via DataTable virtualization prop', () => {
    mockUseInfiniteInvestmentTransactions.mockReturnValue(
      loadedResult({ transactions: [mockTxn], hasNextPage: true }),
    )
    render(<InvestmentTransactionsTable dateRange={defaultDateRange} />)
    // Assert the captured DataTable virtualization prop wires onScrollEnd to the real
    // fetchNextPage fn — removing / no-op-ing the wiring in the component fails this.
    const virt = lastDataTableProps?.virtualization as
      | { hasMore?: boolean; onScrollEnd?: () => void }
      | undefined
    expect(virt?.onScrollEnd).toBe(mockFetchNextInvestmentPage)
  })

  it('passes maskedColumnIds to DataTable (monetary columns are masked)', () => {
    mockUseInfiniteInvestmentTransactions.mockReturnValue(
      loadedResult({ transactions: [mockTxn] }),
    )
    render(<InvestmentTransactionsTable dateRange={defaultDateRange} />)
    // maskedColumnIds must deep-equal the monetary set — removing the prop from the
    // component would fail this assertion (unlike the previous table-existence check).
    expect(lastDataTableProps?.maskedColumnIds).toEqual(['quantity', 'price', 'amount', 'fees'])
  })

  describe('dark mode', () => {
    afterEach(() => {
      document.documentElement.classList.remove('dark')
    })

    it('renders virtual scroll container in dark mode context without error', () => {
      document.documentElement.classList.add('dark')
      mockUseInfiniteInvestmentTransactions.mockReturnValue(loadedResult())
      const { container } = render(<InvestmentTransactionsTable dateRange={defaultDateRange} />)
      expect(container.querySelector('[data-testid="virtual-scroll-container"]')).not.toBeNull()
    })
  })
})
