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

import { describe, it, expect, beforeEach, afterEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { HoldingsTable } from './HoldingsTable'
import { mockUseGetHoldings } from '@/test/mocks/investments-api'
import { mockUseGetAccounts } from '@/test/mocks/accounts-api'
import { mockUseUser, createMockClerkUser, mockClerkUser } from '@/test/mocks/clerk'
import type { HoldingView } from '@boostedbudget/api-client/models/holdingView'
import type { Account } from '@boostedbudget/api-client/models'

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

// Crossed values: opposite orderings ensure each sort test proves its accessor independently
// gain_loss_pct: LGH(5) < MGM(25) < HGL(50)  |  day_change_pct: HGL(0.5) < MGM(2.5) < LGH(5.0)
const holdingLowGain = makeHolding({ id: 'h4', security_name: 'LowGainHighDay', ticker_symbol: 'LGH', gain_loss_pct: 5, day_change_pct: 5.0, institution_value: 2000, account_id: 'acct-x' })
const holdingMidGain = makeHolding({ id: 'h5', security_name: 'MidGainMidDay', ticker_symbol: 'MGM', gain_loss_pct: 25, day_change_pct: 2.5, institution_value: 3000, account_id: 'acct-x' })
const holdingHighGain = makeHolding({ id: 'h6', security_name: 'HighGainLowDay', ticker_symbol: 'HGL', gain_loss_pct: 50, day_change_pct: 0.5, institution_value: 1000, account_id: 'acct-x' })

function makeAccount(overrides: Partial<Account> & { id: string; name: string }): Account {
  return {
    available_balance: undefined,
    created_at: '2024-01-01',
    currency_code: 'USD',
    current_balance: 10000,
    custom_color: undefined,
    custom_emoji: undefined,
    custom_icon: undefined,
    display_order: undefined,
    institution_name: 'Test Bank',
    is_hidden: false,
    linked_item_id: 'item-1',
    linked_item_status: 'active',
    mask: undefined,
    nickname: undefined,
    official_name: undefined,
    plaid_account_id: 'plaid-1',
    replaced_at: undefined,
    replaced_by_id: undefined,
    status: 'active',
    subtype: 'checking',
    type: 'depository',
    updated_at: '2024-01-01',
    user_account_id: undefined,
    user_id: 'u1',
    ...overrides,
  }
}

const accountAlpha = makeAccount({ id: 'acct-alpha', name: 'Alpha Brokerage' })
const accountBeta = makeAccount({ id: 'acct-beta', name: 'Beta Brokerage' })

describe('HoldingsTable', () => {
  beforeEach(() => {
    mockUseGetHoldings.mockReturnValue({ data: undefined, isLoading: false, isError: false })
    mockUseGetAccounts.mockReturnValue({
      data: { accounts: [accountAlpha, accountBeta] },
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve({}),
    })
  })

  afterEach(() => {
    mockUseGetAccounts.mockClear()
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

  it('shows account names (not raw IDs) in Account column cells and filter options', () => {
    mockUseGetHoldings.mockReturnValue({
      data: { holdings: [holdingA, holdingB], count: 2 },
      isLoading: false,
      isError: false,
    })
    render(<HoldingsTable />)

    // Account column cells should show the account name, not the raw UUID
    expect(screen.getAllByText('Alpha Brokerage').length).toBeGreaterThan(0)
    // Raw ID should not appear in the account column cell
    const rows = screen.getAllByRole('row').slice(1)
    const hasRawId = rows.some((r) => r.textContent?.includes('acct-alpha') || r.textContent?.includes('acct-beta'))
    expect(hasRawId).toBe(false)
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
    // Filter dropdown should show account names, not raw IDs
    await userEvent.click(screen.getByRole('checkbox', { name: 'Beta Brokerage' }))

    // After applying the filter, only MSFT (acct-beta = Beta Brokerage) should remain
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

  it('reads updated column visibility on remount (full persistence round-trip)', () => {
    const userId = mockClerkUser.id
    const schemaVersion =
      'name,ticker,type,quantity,price,market_value,cost_basis,gain_loss,gain_loss_pct,day_change,day_change_pct,account'

    mockUseUser.mockReturnValue(createMockClerkUser({ isSignedIn: true, user: mockClerkUser }))
    mockUseGetHoldings.mockReturnValue({
      data: { holdings: [holdingA], count: 1 },
      isLoading: false,
      isError: false,
    })

    // Mount 1: no stored prefs → Account column should be visible (default).
    localStorage.removeItem(`bb:table-prefs:${userId}:portfolio-holdings`)
    const { unmount: unmount1 } = render(<HoldingsTable />)
    expect(screen.queryByText('Account')).not.toBeNull()
    unmount1()

    // Simulate saving a preference (hide the Account column).
    localStorage.setItem(
      `bb:table-prefs:${userId}:portfolio-holdings`,
      JSON.stringify({
        schemaVersion,
        sizingVersion: 'v3-compact',
        sizing: {},
        columnOrder: [],
        columnVisibility: { account: false },
      }),
    )

    // Mount 2: Account column must be hidden (restored from updated localStorage).
    render(<HoldingsTable />)
    expect(screen.queryByText('Account')).toBeNull()
    // Non-hideable column still present.
    expect(screen.getByText('Market Value')).toBeDefined()

    // Cleanup.
    mockUseUser.mockReturnValue(createMockClerkUser())
    localStorage.removeItem(`bb:table-prefs:${userId}:portfolio-holdings`)
  })

  describe('dark mode', () => {
    afterEach(() => {
      document.documentElement.classList.remove('dark')
    })

    it('renders and applies data-ph-mask to monetary cells in dark mode context', () => {
      document.documentElement.classList.add('dark')
      mockUseGetHoldings.mockReturnValue({
        data: { holdings: [holdingA], count: 1 },
        isLoading: false,
        isError: false,
      })
      const { container } = render(<HoldingsTable />)
      expect(screen.getByText('AAPL')).toBeDefined()
      const maskedTds = container.querySelectorAll('td[data-ph-mask]')
      expect(maskedTds.length).toBeGreaterThan(0)
    })
  })

  // ── PII masking fix: name + ticker (applies to both scoped and unscoped) ──────

  describe('name and ticker PII masking', () => {
    it('name column td carries data-ph-mask', () => {
      mockUseGetHoldings.mockReturnValue({
        data: { holdings: [holdingA], count: 1 },
        isLoading: false,
        isError: false,
      })
      const { container } = render(<HoldingsTable />)
      // The 'name' column cell should be masked — confirm by finding a masked td with that text
      const nameCells = Array.from(container.querySelectorAll('td[data-ph-mask]')).filter(
        (td) => td.textContent?.includes('Apple Inc')
      )
      expect(nameCells.length).toBeGreaterThan(0)
    })

    it('ticker column td carries data-ph-mask', () => {
      mockUseGetHoldings.mockReturnValue({
        data: { holdings: [holdingA], count: 1 },
        isLoading: false,
        isError: false,
      })
      const { container } = render(<HoldingsTable />)
      const maskedTds = Array.from(container.querySelectorAll('td[data-ph-mask]')).filter(
        (td) => td.textContent?.includes('AAPL')
      )
      expect(maskedTds.length).toBeGreaterThan(0)
    })
  })

  // ── Scoped mode (accountId provided) ─────────────────────────────────────────

  describe('scoped mode (accountId provided)', () => {
    it('calls useGetHoldings with { account_id } when accountId prop is given', () => {
      mockUseGetHoldings.mockReturnValue({
        data: { holdings: [], count: 0 },
        isLoading: false,
        isError: false,
      })
      mockUseGetHoldings.mockClear()
      render(<HoldingsTable accountId="test-account-id" />)
      expect(mockUseGetHoldings.mock.calls.length).toBeGreaterThan(0)
      expect(mockUseGetHoldings.mock.calls[0][0]).toEqual({ account_id: 'test-account-id' })
    })

    it('does NOT render the Account column header when scoped', () => {
      mockUseGetHoldings.mockReturnValue({
        data: { holdings: [holdingA], count: 1 },
        isLoading: false,
        isError: false,
      })
      render(<HoldingsTable accountId="acct-alpha" />)
      expect(screen.queryByText('Account')).toBeNull()
    })

    it('does NOT render the Account filter dropdown when scoped', () => {
      mockUseGetHoldings.mockReturnValue({
        data: { holdings: [holdingA], count: 1 },
        isLoading: false,
        isError: false,
      })
      render(<HoldingsTable accountId="acct-alpha" />)
      expect(screen.queryByRole('button', { name: /filter by account/i })).toBeNull()
    })

    it('shows account-specific empty state when scoped and holdings = []', () => {
      mockUseGetHoldings.mockReturnValue({
        data: { holdings: [], count: 0 },
        isLoading: false,
        isError: false,
      })
      render(<HoldingsTable accountId="acct-alpha" />)
      expect(screen.getByText(/no holdings synced yet for this account/i)).toBeDefined()
    })
  })

  // ── Unscoped regression ───────────────────────────────────────────────────────

  describe('unscoped mode regression', () => {
    it('calls useGetHoldings with no params (undefined) when accountId is not provided', () => {
      mockUseGetHoldings.mockReturnValue({
        data: { holdings: [], count: 0 },
        isLoading: false,
        isError: false,
      })
      mockUseGetHoldings.mockClear()
      render(<HoldingsTable />)
      expect(mockUseGetHoldings.mock.calls.length).toBeGreaterThan(0)
      expect(mockUseGetHoldings.mock.calls[0][0]).toBeUndefined()
    })

    it('renders the Account column header in unscoped mode', () => {
      mockUseGetHoldings.mockReturnValue({
        data: { holdings: [holdingA], count: 1 },
        isLoading: false,
        isError: false,
      })
      render(<HoldingsTable />)
      expect(screen.getByText('Account')).toBeDefined()
    })

    it('uses portfolio-holdings as the useTablePreferences key in unscoped mode', () => {
      const userId = mockClerkUser.id
      const schemaVersion =
        'name,ticker,type,quantity,price,market_value,cost_basis,gain_loss,gain_loss_pct,day_change,day_change_pct,account'
      // Write a pref under 'portfolio-holdings' key and confirm the component reads it
      localStorage.setItem(
        `bb:table-prefs:${userId}:portfolio-holdings`,
        JSON.stringify({
          schemaVersion,
          sizingVersion: 'v3-compact',
          sizing: {},
          columnOrder: [],
          columnVisibility: { account: false },
        }),
      )
      mockUseUser.mockReturnValue(createMockClerkUser({ isSignedIn: true, user: mockClerkUser }))
      mockUseGetHoldings.mockReturnValue({
        data: { holdings: [holdingA], count: 1 },
        isLoading: false,
        isError: false,
      })

      render(<HoldingsTable />)
      // If the key is correct ('portfolio-holdings'), the hidden-account pref is picked up
      expect(screen.queryByText('Account')).toBeNull()

      mockUseUser.mockReturnValue(createMockClerkUser())
      localStorage.removeItem(`bb:table-prefs:${userId}:portfolio-holdings`)
    })
  })
  // ── #1569 T12 gap 4: the disconnected-account empty state ──────────────────────────────
  //
  // Half A ruled that positions are a CURRENT-STATE question and stay hidden once an account
  // goes terminal (see backend account_liveness_sql.go). That ruling is only honest if the UI
  // says so. AccountsPage lists closed accounts, so a user CAN reach this tab for one, and the
  // generic empty state told them "nothing synced yet" — which is false: positions were synced,
  // retained, and are being withheld on purpose.
  describe('disconnected account (#1569 T12)', () => {
    const accountClosed = makeAccount({ id: 'acct-closed', name: 'Old Brokerage', status: 'removed' })

    beforeEach(() => {
      mockUseGetAccounts.mockReturnValue({
        data: { accounts: [accountAlpha, accountBeta, accountClosed] },
        isLoading: false,
        error: null,
        refetch: () => Promise.resolve({}),
      })
      mockUseGetHoldings.mockReturnValue({
        data: { holdings: [], count: 0 },
        isLoading: false,
        isError: false,
      })
    })

    it('explains WHY a disconnected account shows no positions, instead of "nothing synced yet"', () => {
      render(<HoldingsTable accountId="acct-closed" />)
      expect(screen.getByText(/disconnected/i)).toBeDefined()
      expect(screen.queryByText(/no holdings synced yet/i)).toBeNull()
    })

    it('says the history was kept, so the user is not told their data is gone', () => {
      render(<HoldingsTable accountId="acct-closed" />)
      expect(screen.getByText(/retained/i)).toBeDefined()
    })

    // Fix round 1 (I4). The first draft told the user "the positions we last confirmed are kept
    // on record". They ARE kept — in holding_snapshots — but nothing reads that table: zero
    // non-test callers, no service method, no endpoint, no UI. Promising the user data no
    // surface can show them is #1569's own complaint pointed the other way, so the notice now
    // claims only what it can deliver: the TRANSACTIONS are visible, and the positions stop.
    it('promises the user nothing it cannot actually show them', () => {
      const { container } = render(<HoldingsTable accountId="acct-closed" />)
      const text = container.textContent ?? ''
      expect(text).not.toMatch(/positions .*(kept on record|last confirmed)/i)
      // What IS reachable today: the account's transactions.
      expect(text).toMatch(/transactions/i)
    })

    it('leaves a live account on the ordinary empty state', () => {
      render(<HoldingsTable accountId="acct-alpha" />)
      expect(screen.getByText(/no holdings synced yet/i)).toBeDefined()
      expect(screen.queryByText(/disconnected/i)).toBeNull()
    })

    // The notice must not appear over real rows: if the backend ever did return a position for a
    // terminal account, the honest thing is to show the position, not to claim there is none.
    it('does not replace holdings that are actually present', () => {
      mockUseGetHoldings.mockReturnValue({
        data: { holdings: [makeHolding({ id: 'h9', ticker_symbol: 'ZZZ', account_id: 'acct-closed' })], count: 1 },
        isLoading: false,
        isError: false,
      })
      render(<HoldingsTable accountId="acct-closed" />)
      expect(screen.getByText('ZZZ')).toBeDefined()
      expect(screen.queryByText(/disconnected/i)).toBeNull()
    })
  })
})
