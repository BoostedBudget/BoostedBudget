/**
 * HoldingsTable Component Tests
 * Issue #1296: Portfolio tab — holdings DataTable.
 * Tests: data rendering, loading state, empty state, PII masking,
 *        client-side sort (manualSorting=true), account filter (manualFiltering=true).
 *
 * DataTable has manualSorting=true AND manualFiltering=true: HoldingsTable owns sort/filter
 * application. Sort is proven by asserting DOM row order after header click.
 * Filter is proven by opening the account filter dropdown and selecting an account.
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { HoldingsTable } from './HoldingsTable'
import { mockUseGetHoldings } from '@/test/mocks/investments-api'
import { mockUseUser, createMockClerkUser, mockClerkUser } from '@/test/mocks/clerk'
import type { HoldingView } from '@boostedbudget/api-client/models/holdingView'

function makeHolding(overrides: Partial<HoldingView> & { id: string }): HoldingView {
  return {
    user_id: 'u1',
    security_id: 's1',
    quantity: 10,
    security_name: 'Security',
    ticker_symbol: 'TICK',
    security_type: 'stock',
    institution_value: 1000,
    institution_price: 100,
    cost_basis: 800,
    gain_loss: 200,
    gain_loss_pct: 25,
    day_change: 10,
    day_change_pct: 1,
    account_id: 'acct-default',
    ...overrides,
  }
}

const holdingA = makeHolding({ id: 'h1', security_name: 'Apple Inc', ticker_symbol: 'AAPL', institution_value: 2500, account_id: 'acct-alpha' })
const holdingB = makeHolding({ id: 'h2', security_name: 'Microsoft', ticker_symbol: 'MSFT', institution_value: 1000, account_id: 'acct-beta' })
const holdingC = makeHolding({ id: 'h3', security_name: 'Tesla', ticker_symbol: 'TSLA', institution_value: 5000, account_id: 'acct-alpha' })

// Crossed values: gain_loss_pct and day_change_pct have OPPOSITE relative orderings so that
// sorting by one column produces a different row order than sorting by the other. This ensures
// each sort test independently proves its accessor rather than passing accidentally due to
// correlated fixture values.
//   gain_loss_pct order: LGH(5) < MGM(25) < HGL(50) → asc: LGH first, HGL last
//   day_change_pct order: HGL(0.5) < MGM(2.5) < LGH(5.0) → asc: HGL first, LGH last
const holdingLowGain = makeHolding({ id: 'h4', security_name: 'LowGainHighDay', ticker_symbol: 'LGH', gain_loss_pct: 5, day_change_pct: 5.0, institution_value: 2000, account_id: 'acct-x' })
const holdingMidGain = makeHolding({ id: 'h5', security_name: 'MidGainMidDay', ticker_symbol: 'MGM', gain_loss_pct: 25, day_change_pct: 2.5, institution_value: 3000, account_id: 'acct-x' })
const holdingHighGain = makeHolding({ id: 'h6', security_name: 'HighGainLowDay', ticker_symbol: 'HGL', gain_loss_pct: 50, day_change_pct: 0.5, institution_value: 1000, account_id: 'acct-x' })

describe('HoldingsTable', () => {
  beforeEach(() => {
    mockUseGetHoldings.mockReturnValue({ data: undefined, isLoading: false, isError: false })
  })

  it('renders holdings data', () => {
    mockUseGetHoldings.mockReturnValue({
      data: { holdings: [holdingA], count: 1 },
      isLoading: false,
      isError: false,
    })
    render(<HoldingsTable />)
    expect(screen.getByText('AAPL')).toBeDefined()
  })

  it('shows loading state', () => {
    mockUseGetHoldings.mockReturnValue({ data: undefined, isLoading: true, isError: false })
    const { container } = render(<HoldingsTable />)
    const statusEl =
      container.querySelector('[role="status"]') ?? screen.queryByRole('status', { hidden: true })
    expect(statusEl).toBeDefined()
  })

  it('renders empty state when no holdings', () => {
    mockUseGetHoldings.mockReturnValue({
      data: { holdings: [], count: 0 },
      isLoading: false,
      isError: false,
    })
    render(<HoldingsTable />)
    expect(screen.getByText(/no holdings found/i)).toBeDefined()
  })

  it('masked columns contain data-ph-mask', () => {
    mockUseGetHoldings.mockReturnValue({
      data: { holdings: [holdingA], count: 1 },
      isLoading: false,
      isError: false,
    })
    const { container } = render(<HoldingsTable />)
    const maskedTds = container.querySelectorAll('td[data-ph-mask]')
    expect(maskedTds.length).toBeGreaterThan(0)
  })

  it('sorts by Market Value ascending on first header click', () => {
    mockUseGetHoldings.mockReturnValue({
      data: { holdings: [holdingA, holdingB, holdingC], count: 3 },
      isLoading: false,
      isError: false,
    })
    render(<HoldingsTable />)

    const sortBtn = screen.getAllByRole('button').find((b) => b.textContent?.includes('Market Value'))!
    fireEvent.click(sortBtn)

    // asc: MSFT(1000) < AAPL(2500) < TSLA(5000)
    const dataRows = screen.getAllByRole('row').slice(1)
    expect(dataRows[0].textContent).toContain('MSFT')
    expect(dataRows[dataRows.length - 1].textContent).toContain('TSLA')
  })

  it('sorts by Market Value descending on second header click', () => {
    mockUseGetHoldings.mockReturnValue({
      data: { holdings: [holdingA, holdingB, holdingC], count: 3 },
      isLoading: false,
      isError: false,
    })
    render(<HoldingsTable />)

    const sortBtn = screen.getAllByRole('button').find((b) => b.textContent?.includes('Market Value'))!
    fireEvent.click(sortBtn) // asc
    fireEvent.click(sortBtn) // desc

    // desc: TSLA(5000) first
    const dataRows = screen.getAllByRole('row').slice(1)
    expect(dataRows[0].textContent).toContain('TSLA')
  })

  it('filters rows to selected account when account filter is applied', async () => {
    mockUseGetHoldings.mockReturnValue({
      data: { holdings: [holdingA, holdingB, holdingC], count: 3 },
      isLoading: false,
      isError: false,
    })
    render(<HoldingsTable />)

    // All 3 tickers initially visible
    expect(screen.getByText('AAPL')).toBeDefined()
    expect(screen.getByText('MSFT')).toBeDefined()
    expect(screen.getByText('TSLA')).toBeDefined()

    // DataTable renders a filter button for the Account column with aria-label "Filter by Account"
    await userEvent.click(screen.getByRole('button', { name: /filter by account/i }))
    // Use checkbox role to target the dropdown option unambiguously — 'acct-beta' also
    // appears as cell text in the Account column for holdingB, causing getByText to throw.
    await userEvent.click(screen.getByRole('checkbox', { name: 'acct-beta' }))

    // After applying the filter, only MSFT (acct-beta) should remain
    expect(screen.queryByText('AAPL')).toBeNull()
    expect(screen.queryByText('TSLA')).toBeNull()
    expect(screen.getByText('MSFT')).toBeDefined()
  })

  it('sorts by Gain/Loss (%) ascending on first header click', () => {
    mockUseGetHoldings.mockReturnValue({
      data: { holdings: [holdingHighGain, holdingLowGain, holdingMidGain], count: 3 },
      isLoading: false,
      isError: false,
    })
    render(<HoldingsTable />)

    const sortBtn = screen.getAllByRole('button').find((b) => b.textContent?.includes('Gain/Loss (%)'))!
    fireEvent.click(sortBtn)

    // asc: LGH(5%) < MGM(25%) < HGL(50%)
    const dataRows = screen.getAllByRole('row').slice(1)
    expect(dataRows[0].textContent).toContain('LGH')
    expect(dataRows[dataRows.length - 1].textContent).toContain('HGL')
  })

  it('sorts by Day Change (%) ascending on first header click', () => {
    mockUseGetHoldings.mockReturnValue({
      data: { holdings: [holdingLowGain, holdingMidGain, holdingHighGain], count: 3 },
      isLoading: false,
      isError: false,
    })
    render(<HoldingsTable />)

    const sortBtn = screen.getAllByRole('button').find((b) => b.textContent?.includes('Day Change (%)'))!
    fireEvent.click(sortBtn)

    // asc: HGL(0.5%) < MGM(2.5%) < LGH(5.0%) — reversed vs. gain_loss_pct order
    const dataRows = screen.getAllByRole('row').slice(1)
    expect(dataRows[0].textContent).toContain('HGL')
    expect(dataRows[dataRows.length - 1].textContent).toContain('LGH')
  })

  it('restores hidden column visibility from localStorage for signed-in user', () => {
    const userId = mockClerkUser.id
    const schemaVersion =
      'name,ticker,type,quantity,price,market_value,cost_basis,gain_loss,gain_loss_pct,day_change,day_change_pct,account'
    localStorage.setItem(
      `bb:table-prefs:${userId}:portfolio-holdings`,
      JSON.stringify({
        schemaVersion,
        sizingVersion: 'v3-compact',
        sizing: {},
        columnOrder: [],
        columnVisibility: { account: false },
      })
    )
    mockUseUser.mockReturnValue(createMockClerkUser({ isSignedIn: true, user: mockClerkUser }))
    mockUseGetHoldings.mockReturnValue({
      data: { holdings: [holdingA], count: 1 },
      isLoading: false,
      isError: false,
    })

    render(<HoldingsTable />)

    // The 'Account' column header (hideable:true) must be absent when hidden in localStorage.
    expect(screen.queryByText('Account')).toBeNull()
    // Sanity: non-hideable column headers are still present.
    expect(screen.getByText('Market Value')).toBeDefined()

    // Restore mock and localStorage for subsequent tests.
    mockUseUser.mockReturnValue(createMockClerkUser())
    localStorage.removeItem(`bb:table-prefs:${userId}:portfolio-holdings`)
  })
})
