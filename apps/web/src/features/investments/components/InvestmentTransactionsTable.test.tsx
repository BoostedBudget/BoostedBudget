/**
 * InvestmentTransactionsTable Component Tests
 * Issue #1296: Portfolio tab — investment transactions DataTable with infinite scroll.
 * Uses mocked hook (no real React Query); hook tested separately in useInfiniteInvestmentTransactions.test.tsx.
 * Assert DataTable presence (loaded state), not absence of spinner (ResizeObserver async race).
 *
 * NOTE: This file does NOT use mock.module('@/shared/components/DataTable') — that call is
 * process-global in bun:test and would permanently replace useTablePreferences for subsequent
 * test files (DataTable.test.tsx, useTablePreferences.test.ts). Tests use the real DataTable
 * which renders data-testid="virtual-scroll-container" and data-testid="virtual-sentinel" via
 * the virtualization prop. See feedback_bun_test_mock_module_global.md.
 */

import { describe, it, expect, beforeEach, afterEach } from 'bun:test'
import { render, screen, waitFor } from '@testing-library/react'
import { InvestmentTransactionsTable } from './InvestmentTransactionsTable'
import {
  mockUseInfiniteInvestmentTransactions,
  mockFetchNextInvestmentPage,
} from '@/test/mocks/investments-api'
import type { InvestmentTransaction } from '@boostedbudget/api-client/models/investmentTransaction'

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
    mockUseInfiniteInvestmentTransactions.mockClear()
    mockFetchNextInvestmentPage.mockClear()
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

  // R12-1: end-to-end wiring — proves the component passes fetchNextPage from the hook
  // into buildInvestmentVirtualization so DataTable's onScrollEnd fires it.
  // TanStack Virtual uses offsetHeight (via getRect) for initial container sizing.
  // Stubbing offsetHeight + disabling ResizeObserver (which would override with 0) lets
  // the virtualizer produce virtual items in happy-dom → DataTable's onScrollEnd useEffect fires.
  describe('onScrollEnd wiring (component → DataTable)', () => {
    const origOffsetHeight = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'offsetHeight')
    const origRO = (globalThis as Record<string, unknown>).ResizeObserver

    beforeEach(() => {
      Object.defineProperty(HTMLElement.prototype, 'offsetHeight', {
        configurable: true,
        get() { return 600 },
      })
      // Prevent ResizeObserver from firing with 0 dimensions after the initial getRect call
      ;(globalThis as Record<string, unknown>).ResizeObserver = class {
        observe() {}
        unobserve() {}
        disconnect() {}
      }
    })

    afterEach(() => {
      if (origOffsetHeight) {
        Object.defineProperty(HTMLElement.prototype, 'offsetHeight', origOffsetHeight)
      }
      ;(globalThis as Record<string, unknown>).ResizeObserver = origRO
    })

    it('calls fetchNextPage when virtual scroll reaches the end', async () => {
      mockFetchNextInvestmentPage.mockClear()
      mockUseInfiniteInvestmentTransactions.mockReturnValue(
        loadedResult({ transactions: [mockTxn], hasNextPage: true }),
      )
      render(<InvestmentTransactionsTable dateRange={defaultDateRange} />)
      await waitFor(() => {
        expect(mockFetchNextInvestmentPage).toHaveBeenCalled()
      })
    })
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

  // R11-3: defensive empty-type render — uses disableVirtualization to force real rows
  describe('type column rendering (disableVirtualization seam)', () => {
    it('renders "Other" chip for a transaction with empty type without crashing', () => {
      const emptyTypeTxn = { ...mockTxn, type: '' }
      mockUseInfiniteInvestmentTransactions.mockReturnValue(
        loadedResult({ transactions: [emptyTypeTxn] }),
      )
      // disableVirtualization forces DataTable to render all rows (no jsdom layout needed).
      render(
        <InvestmentTransactionsTable dateRange={defaultDateRange} disableVirtualization />,
      )
      // ActivityTypeChip falls back to 'Other' for unknown/empty types.
      expect(screen.getByText('Other')).toBeDefined()
    })

    it('renders "Buy" chip for a buy transaction', () => {
      mockUseInfiniteInvestmentTransactions.mockReturnValue(
        loadedResult({ transactions: [mockTxn] }),
      )
      render(
        <InvestmentTransactionsTable dateRange={defaultDateRange} disableVirtualization />,
      )
      // mockTxn.type = 'buy' → ActivityTypeChip → 'Buy'
      expect(screen.getByText('Buy')).toBeDefined()
    })
  })
})

// #1631 CX1: the amount, price and fee columns hard-coded the dollar, so a GBP-denominated row
// rendered as dollars. Each row carries its own currency_code -- an investment account can hold
// positions in several currencies at once, which is exactly why the per-ROW code is the one to
// format by rather than any portfolio-level verdict.
describe('InvestmentTransactionsTable currency (#1631 CX1)', () => {
  beforeEach(() => {
    mockUseInfiniteInvestmentTransactions.mockClear()
  })

  it('formats a row in the currency that row states', async () => {
    mockUseInfiniteInvestmentTransactions.mockReturnValue(
      loadedResult({ transactions: [{ ...mockTxn, currency_code: 'GBP' }] }),
    )
    // disableVirtualization forces DataTable to render real rows; jsdom has no layout.
    render(<InvestmentTransactionsTable dateRange={defaultDateRange} disableVirtualization />)

    await waitFor(() => {
      expect(screen.getByText(/£2,500/)).toBeDefined()
    })
    expect(screen.queryByText(/\$2,500/)).toBeNull()
  })

  // The sibling: a row that states no currency keeps today's dollar rendering, because unknown is
  // not a conflict and every row written before migration 000197 is in that state.
  it('keeps the dollar rendering for a row that states no currency', async () => {
    mockUseInfiniteInvestmentTransactions.mockReturnValue(
      loadedResult({ transactions: [mockTxn] }),
    )
    render(<InvestmentTransactionsTable dateRange={defaultDateRange} disableVirtualization />)

    await waitFor(() => {
      expect(screen.getByText(/\$2,500/)).toBeDefined()
    })
  })
})
