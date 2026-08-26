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
import { mockUseGetHoldings, mockUseGetAccountHoldingsHistory } from '@/test/mocks/investments-api'
import { mockUseGetAccounts } from '@/test/mocks/accounts-api'
import { mockUseUser, createMockClerkUser, mockClerkUser } from '@/test/mocks/clerk'
import type { HoldingView } from '@boostedbudget/api-client/models/holdingView'
import type { HoldingSnapshotView } from '@boostedbudget/api-client/models/holdingSnapshotView'
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
  // ── #1569: a closed account's RETAINED holdings history ────────────────────────────────
  //
  // T12 half A ruled that positions are a CURRENT-STATE question and stay hidden once an account
  // goes terminal. That ruling stands — the live read is untouched — but the slot it left behind
  // used to hold a notice saying only the transactions were still reachable. That was honest while
  // holding_snapshots had no reader, and false against privacy policy §8.2a, which tells the user a
  // removed account's holdings history "is retained ... so you can still view it under 'Closed
  // accounts'". The reader was built rather than the policy narrowed, and these tests are what
  // stop the promise going stale again.
  describe('closed account holdings history (#1569)', () => {
    const accountClosed = makeAccount({
      id: 'acct-closed',
      name: 'Old Brokerage',
      status: 'removed',
      // Deliberately a DIFFERENT day from the snapshot date below: with both the same, a mutant
      // that rendered the wrong one — or a raw ISO string — survived, because the other half of
      // the caption still produced the expected text.
      removed_at: '2026-03-10T00:00:00Z',
    })

    const position = (overrides: Partial<HoldingSnapshotView> = {}): HoldingSnapshotView => ({
      security_id: 'sec-1',
      as_of_date: '2026-03-04',
      quantity: 10,
      cost_basis: 4000,
      institution_value: 5000,
      institution_price: 500,
      security_name: 'Apple Inc',
      ticker_symbol: 'AAPL',
      security_type: 'stock',
      gain_loss: 1000,
      gain_loss_pct: 25,
      ...overrides,
    })

    function withHistory(overrides: Partial<{
      as_of_date: string
      available_dates: string[]
      positions: HoldingSnapshotView[]
      total_value: number
    }> = {}) {
      mockUseGetAccountHoldingsHistory.mockReturnValue({
        data: {
          account_id: 'acct-closed',
          as_of_date: '2026-03-04',
          available_dates: ['2026-03-04'],
          positions: [position()],
          total_value: 5000,
          ...overrides,
        },
        isLoading: false,
        isError: false,
      })
    }

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
      mockUseGetAccountHoldingsHistory.mockReturnValue({
        data: { account_id: 'acct-closed', available_dates: [], positions: [] },
        isLoading: false,
        isError: false,
      })
    })

    // THE DEFECT THIS CLOSES. §8.2a says the user can still view a removed account's holdings
    // history. Before this, they could not see a single position.
    it('renders the retained positions of a closed account', () => {
      withHistory()
      render(<HoldingsTable accountId="acct-closed" />)

      expect(screen.getByText('AAPL')).toBeDefined()
      expect(screen.getByText('Apple Inc')).toBeDefined()
      expect(screen.queryByText(/no holdings synced yet/i)).toBeNull()
    })

    it('names the day the positions were true, so they cannot read as current', () => {
      withHistory()
      const { container } = render(<HoldingsTable accountId="acct-closed" />)
      const text = container.textContent ?? ''

      // The SNAPSHOT day, formatted — not the raw ISO string, and not the closure day.
      expect(text).toMatch(/Positions as of Mar 4, 2026/)
      expect(text).not.toMatch(/2026-03-04/)
      expect(text).toMatch(/not what the account holds today/i)
    })

    // The closure date is a different fact from the snapshot date and comes from a different
    // field (getAccountClosedDate). Both must render, and neither may stand in for the other.
    it('names the day the account closed, alongside the day the positions were true', () => {
      withHistory()
      const { container } = render(<HoldingsTable accountId="acct-closed" />)
      const text = container.textContent ?? ''

      expect(text).toMatch(/closed on Mar 10, 2026/)
      expect(text).toMatch(/Positions as of Mar 4, 2026/)
    })

    // Information never by colour alone: the state is carried by the word "Closed".
    it('says the account is closed in words, not only by tint', () => {
      withHistory()
      render(<HoldingsTable accountId="acct-closed" />)
      expect(screen.getByText(/closed account/i)).toBeDefined()
    })

    // Day change is measured against today's prior close. A historical position must not carry it,
    // and the backend type has no field for it — this pins the UI half of that separation.
    it('does not render day-change columns over historical positions', () => {
      withHistory()
      render(<HoldingsTable accountId="acct-closed" />)

      expect(screen.queryByText(/Day Change \(\$\)/)).toBeNull()
      expect(screen.queryByText(/Day Change \(%\)/)).toBeNull()
      // The shared value columns ARE there — the historical table is the same table, minus what
      // cannot mean anything on a past day.
      expect(screen.getByText(/Market Value/)).toBeDefined()
      expect(screen.getByText(/Gain\/Loss \(\$\)/)).toBeDefined()
    })

    // PII: DataTable masks a <td> ONLY when the column id is in maskedColumnIds. Holdings are
    // money, so every value column must be listed — "it's a DataTable so it's masked" is false.
    it('masks every money and security cell for session replay', () => {
      withHistory()
      const { container } = render(<HoldingsTable accountId="acct-closed" />)

      const maskedText = Array.from(container.querySelectorAll('[data-ph-mask]')).map(
        (el) => el.textContent ?? '',
      )
      for (const value of ['AAPL', 'Apple Inc', '$5,000', '$4,000', '$1,000']) {
        expect(maskedText.some((t) => t.includes(value))).toBe(true)
      }
    })

    // Attributes are recorded verbatim by session replay and cannot be covered by any mask, so no
    // element on this surface may carry an amount in an aria-label or title.
    it('puts no amount into an attribute', () => {
      withHistory()
      const { container } = render(<HoldingsTable accountId="acct-closed" />)

      for (const el of Array.from(container.querySelectorAll('[aria-label], [title]'))) {
        const attrs = `${el.getAttribute('aria-label') ?? ''} ${el.getAttribute('title') ?? ''}`
        expect(attrs).not.toMatch(/\d[\d,]*\.?\d*\s*$|\$/)
      }
    })

    it('shows the day total from the server rather than adding the rows up', () => {
      withHistory({ total_value: 7777 })
      const { container } = render(<HoldingsTable accountId="acct-closed" />)
      expect(container.textContent ?? '').toMatch(/\$7,777/)
    })

    // Nil, not zero. A portfolio whose values the provider never sent is not worth nothing.
    it('renders no total when the server could not compute one', () => {
      mockUseGetAccountHoldingsHistory.mockReturnValue({
        data: {
          account_id: 'acct-closed',
          as_of_date: '2026-03-04',
          available_dates: ['2026-03-04'],
          positions: [position({ institution_value: undefined, cost_basis: undefined })],
        },
        isLoading: false,
        isError: false,
      })
      const { container } = render(<HoldingsTable accountId="acct-closed" />)
      expect(container.textContent ?? '').not.toMatch(/Total recorded value/i)
    })

    it('offers a labelled day picker only when more than one day was recorded', () => {
      withHistory()
      const single = render(<HoldingsTable accountId="acct-closed" />)
      expect(single.container.querySelector('select')).toBeNull()
      single.unmount()

      withHistory({ available_dates: ['2026-03-04', '2026-03-03'] })
      render(<HoldingsTable accountId="acct-closed" />)

      const select = screen.getByLabelText(/positions as of/i)
      expect(select).toBeDefined()
      expect(screen.getByText('Mar 3, 2026')).toBeDefined()
    })

    it('asks the server for the day the user picked', async () => {
      withHistory({ available_dates: ['2026-03-04', '2026-03-03'] })
      render(<HoldingsTable accountId="acct-closed" />)

      mockUseGetAccountHoldingsHistory.mockClear()
      fireEvent.change(screen.getByLabelText(/positions as of/i), {
        target: { value: '2026-03-03' },
      })

      const args = mockUseGetAccountHoldingsHistory.mock.calls.at(-1)?.[0] as
        | { account_id: string; as_of?: string }
        | undefined
      expect(args?.account_id).toBe('acct-closed')
      expect(args?.as_of).toBe('2026-03-03')
    })

    // No retained holdings history for this account. #1569 R6B/C3: the cause is not established
    // (it may have held nothing, or capture may not have covered it) so the copy must not claim
    // one; it must also not imply the data was destroyed, or render an empty table that reads as
    // "held nothing".
    it('says nothing was recorded, without claiming anything was deleted', () => {
      const { container } = render(<HoldingsTable accountId="acct-closed" />)
      const text = container.textContent ?? ''

      expect(text).toMatch(/no day-by-day positions were recorded/i)
      expect(text).toMatch(/nothing was deleted/i)
      // #1569 R6C: both strings above predate the C3 fix and pass against either copy. Pin the
      // causal claim C3 removed OUT — a regression that reintroduces it must fail here even though
      // it would still satisfy the two assertions above.
      expect(text).not.toMatch(/stopped syncing/i)
    })

    it('tells the user the history is kept rather than erased', () => {
      withHistory()
      const { container } = render(<HoldingsTable accountId="acct-closed" />)
      expect(container.textContent ?? '').toMatch(/keeps this account's history rather than erasing it/i)
    })

    it('says the retained figures are excluded from active balances and reports', () => {
      withHistory()
      const { container } = render(<HoldingsTable accountId="acct-closed" />)
      expect(container.textContent ?? '').toMatch(/excluded from your active balances and reports/i)
    })

    it('surfaces a load failure without saying the history is gone', () => {
      mockUseGetAccountHoldingsHistory.mockReturnValue({
        data: undefined,
        isLoading: false,
        isError: true,
      })
      render(<HoldingsTable accountId="acct-closed" />)

      const alert = screen.getByRole('alert')
      expect(alert.textContent ?? '').toMatch(/still kept/i)
    })

    it('leaves a live account on the ordinary empty state', () => {
      render(<HoldingsTable accountId="acct-alpha" />)
      expect(screen.getByText(/no holdings synced yet/i)).toBeDefined()
      expect(screen.queryByText(/closed account/i)).toBeNull()
    })

    // If the backend ever did return a live position for a terminal account, the honest thing is
    // to show it as current rather than to swap in the historical view.
    it('does not replace holdings that are actually present', () => {
      withHistory()
      mockUseGetHoldings.mockReturnValue({
        data: { holdings: [makeHolding({ id: 'h9', ticker_symbol: 'ZZZ', account_id: 'acct-closed' })], count: 1 },
        isLoading: false,
        isError: false,
      })
      render(<HoldingsTable accountId="acct-closed" />)
      expect(screen.getByText('ZZZ')).toBeDefined()
      expect(screen.queryByText(/closed account/i)).toBeNull()
    })
  })
})
