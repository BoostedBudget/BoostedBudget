/**
 * AccountDetailPage Tests
 *
 * Per-account detail page at /accounts/:accountId.
 * Shows account header for all types; Holdings section + activity link for investment only.
 *
 * Issue #1298: Investment account holdings detail
 *
 * TDD: Tests written before implementation.
 *
 * NOTE: This file does NOT use local mock.module() calls (process-global in bun:test).
 * All API-level mocks come from the centralized preloads in bunfig.toml.
 * Component dependencies render with real implementations + preloaded API stubs.
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, fireEvent, act } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router'

import { AccountDetailPage } from './AccountDetailPage'
import {
  mockUseGetAccounts,
  mockRefetchAccounts,
  mockUseGetAccountBalanceHistory,
} from '@/test/mocks/accounts-api'
import { mockTrackEvent } from '@/test/mocks/track-event'

// ============================================================================
// Test fixtures & factory
// ============================================================================

const baseAccount = {
  linked_item_id: 'item-1',
  currency_code: 'USD',
  status: 'active' as const,
  is_hidden: false,
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-01T00:00:00Z',
}

const createAccount = (overrides: Record<string, unknown> = {}) => ({
  ...baseAccount,
  ...overrides,
})

const mockInvestmentAccount = createAccount({
  id: 'acc-invest-1',
  name: 'Vanguard Brokerage',
  official_name: 'Vanguard Brokerage Account',
  type: 'investment',
  subtype: 'brokerage',
  mask: '4321',
  current_balance: 50000,
  institution_name: 'Vanguard',
})

const mockDepositoryAccount = createAccount({
  id: 'acc-depository-1',
  linked_item_id: 'item-2',
  name: 'Chase Checking',
  official_name: 'Chase Total Checking',
  type: 'depository',
  subtype: 'checking',
  mask: '0000',
  current_balance: 2500,
  available_balance: 2300,
  institution_name: 'Chase',
})

const mockHiddenAccount = createAccount({
  id: 'acc-hidden-1',
  linked_item_id: 'item-3',
  name: 'Hidden Savings',
  nickname: 'My Hidden Account',
  type: 'depository',
  subtype: 'savings',
  mask: '9999',
  current_balance: 10000,
  institution_name: 'Bank of America',
  is_hidden: true,
})

// ============================================================================
// Render helper — wraps in MemoryRouter with a :accountId route
// ============================================================================

function renderDetailPage(accountId: string) {
  return render(
    <MemoryRouter initialEntries={[`/accounts/${accountId}`]}>
      <Routes>
        <Route path="/accounts/:accountId" element={<AccountDetailPage />} />
      </Routes>
    </MemoryRouter>
  )
}

// ============================================================================
// Tests
// ============================================================================

describe('AccountDetailPage', () => {
  beforeEach(() => {
    mockUseGetAccounts.mockClear()
    mockTrackEvent.mockClear()

    // Default: loaded state with investment + depository + hidden accounts
    mockUseGetAccounts.mockReturnValue({
      data: {
        accounts: [mockInvestmentAccount, mockDepositoryAccount, mockHiddenAccount],
      },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mockRefetchAccounts,
    })
  })

  // 1. Renders account header (AccountSummaryHeader present) when account is found
  it('renders account institution name when account is found', () => {
    renderDetailPage('acc-invest-1')
    expect(screen.getByText('Vanguard')).toBeInTheDocument()
  })

  it('renders account display name when account is found', () => {
    renderDetailPage('acc-invest-1')
    // official_name used when no nickname
    expect(screen.getByText('Vanguard Brokerage Account')).toBeInTheDocument()
  })

  // 2. Shows Holdings section (HoldingsTable with accountId) for investment accounts
  it('shows Holdings section heading for investment accounts', () => {
    renderDetailPage('acc-invest-1')
    expect(screen.getByText('Holdings')).toBeInTheDocument()
  })

  // 3. Does NOT show Holdings section for non-investment accounts (depository)
  it('does NOT show Holdings section for depository accounts', () => {
    renderDetailPage('acc-depository-1')
    expect(screen.queryByText('Holdings')).not.toBeInTheDocument()
  })

  // 3 (extended). Does NOT show Holdings section for credit accounts
  it('does NOT show Holdings section for credit accounts', () => {
    const creditAccount = createAccount({
      ...mockDepositoryAccount,
      id: 'acc-credit-1',
      type: 'credit',
      subtype: 'credit card',
      institution_name: 'Amex',
    })
    mockUseGetAccounts.mockReturnValue({
      data: { accounts: [creditAccount] },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mockRefetchAccounts,
    })
    renderDetailPage('acc-credit-1')
    expect(screen.queryByText('Holdings')).not.toBeInTheDocument()
  })

  // 4. Shows activity link with correct href for investment accounts
  it('shows activity link for investment accounts', () => {
    renderDetailPage('acc-invest-1')
    const activityLink = screen.getByRole('link', { name: /view investment activity/i })
    expect(activityLink).toBeInTheDocument()
  })

  // 5. Activity link href = `/transactions?tab=investment-activity&account_id={id}` (exact)
  it('activity link href is exact expected URL', () => {
    renderDetailPage('acc-invest-1')
    const activityLink = screen.getByRole('link', { name: /view investment activity/i })
    expect(activityLink).toHaveAttribute(
      'href',
      '/transactions?tab=investment-activity&account_id=acc-invest-1'
    )
  })

  // 6. Does NOT show activity link for non-investment accounts
  it('does NOT show activity link for depository accounts', () => {
    renderDetailPage('acc-depository-1')
    expect(
      screen.queryByRole('link', { name: /view investment activity/i })
    ).not.toBeInTheDocument()
  })

  // 7. Shows EmptyState (not-found) when query settled and account id not in list
  it('shows not-found state when account id is not in the list after query settles', () => {
    mockUseGetAccounts.mockReturnValue({
      data: { accounts: [mockDepositoryAccount] },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mockRefetchAccounts,
    })
    renderDetailPage('acc-does-not-exist')
    expect(screen.getByRole('status')).toBeInTheDocument()
    expect(screen.getByText(/not found/i)).toBeInTheDocument()
    expect(screen.queryByText('Chase Total Checking')).not.toBeInTheDocument()
  })

  // 8. Shows loading state while isLoading
  it('shows loading state while isLoading', () => {
    mockUseGetAccounts.mockReturnValue({
      data: undefined,
      isLoading: true,
      isError: false,
      error: null,
      refetch: mockRefetchAccounts,
    })
    const { container } = renderDetailPage('acc-invest-1')
    expect(container.querySelector('.animate-pulse')).toBeInTheDocument()
    expect(screen.queryByText('Vanguard')).not.toBeInTheDocument()
  })

  // 8 (extended). Does NOT show EmptyState during loading
  it('does NOT show not-found state during loading', () => {
    mockUseGetAccounts.mockReturnValue({
      data: undefined,
      isLoading: true,
      isError: false,
      error: null,
      refetch: mockRefetchAccounts,
    })
    renderDetailPage('acc-does-not-exist')
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  // 9. Shows ErrorState when isError
  it('shows ErrorState when isError', () => {
    mockUseGetAccounts.mockReturnValue({
      data: undefined,
      isLoading: false,
      isError: true,
      error: new Error('Network error'),
      refetch: mockRefetchAccounts,
    })
    renderDetailPage('acc-invest-1')
    expect(screen.getByRole('alert')).toBeInTheDocument()
    expect(screen.queryByText('Vanguard')).not.toBeInTheDocument()
  })

  // 10. Shows hidden account when it is in the list (include_hidden handled)
  it('shows hidden account when include_hidden is true', () => {
    renderDetailPage('acc-hidden-1')
    // nickname takes priority for display name
    expect(screen.getByText('My Hidden Account')).toBeInTheDocument()
  })

  // 10 (extended). Verify useGetAccounts is called with include_hidden: 'true'
  it('calls useGetAccounts with include_hidden: "true"', () => {
    renderDetailPage('acc-invest-1')
    expect(mockUseGetAccounts).toHaveBeenCalledWith(
      expect.objectContaining({ include_hidden: 'true' })
    )
  })

  // 11. ACCOUNT_DETAIL_VIEWED event fired on mount when account is loaded
  it('fires ACCOUNT_DETAIL_VIEWED event on mount when account is loaded', async () => {
    await act(async () => {
      renderDetailPage('acc-invest-1')
    })
    expect(mockTrackEvent).toHaveBeenCalledWith(
      'account_detail_viewed',
      expect.objectContaining({
        account_id: 'acc-invest-1',
        account_type: 'investment',
      })
    )
  })

  // Helper: verify analytics event payload contains only the specified keys (order-independent)
  function expectAnalyticsPayloadKeys(eventName: string, expectedKeys: string[]) {
    const call = mockTrackEvent.mock.calls.find(([event]) => event === eventName)
    expect(call).toBeDefined()
    const payload = call![1] as Record<string, unknown>
    expect(Object.keys(payload).sort()).toEqual([...expectedKeys].sort())
  }

  // 11 (extended). Analytics payload contains ONLY account_id + account_type
  it('ACCOUNT_DETAIL_VIEWED payload contains only account_id and account_type', async () => {
    await act(async () => {
      renderDetailPage('acc-invest-1')
    })
    expectAnalyticsPayloadKeys('account_detail_viewed', ['account_id', 'account_type'])
  })

  // 12. ACCOUNT_INVESTMENT_ACTIVITY_CLICKED event fired with correct values when activity link clicked
  it('fires ACCOUNT_INVESTMENT_ACTIVITY_CLICKED with correct payload when activity link is clicked', () => {
    renderDetailPage('acc-invest-1')
    const activityLink = screen.getByRole('link', { name: /view investment activity/i })
    fireEvent.click(activityLink)
    expect(mockTrackEvent).toHaveBeenCalledWith(
      'account_investment_activity_clicked',
      { account_id: 'acc-invest-1', account_type: 'investment' }
    )
  })

  // 12 (extended). Activity-click payload contains ONLY account_id + account_type (no PII leakage)
  it('ACCOUNT_INVESTMENT_ACTIVITY_CLICKED payload contains only account_id and account_type', () => {
    renderDetailPage('acc-invest-1')
    const activityLink = screen.getByRole('link', { name: /view investment activity/i })
    fireEvent.click(activityLink)
    expectAnalyticsPayloadKeys('account_investment_activity_clicked', ['account_id', 'account_type'])
  })

  // PII masking — current balance span carries data-ph-mask
  it('current balance span carries data-ph-mask attribute', () => {
    renderDetailPage('acc-invest-1')
    // formatAccountBalance(50000, 'USD') → '$50,000.00'
    const balanceEl = screen.getByText('$50,000.00')
    expect(balanceEl).toHaveAttribute('data-ph-mask')
  })

  // PII masking — available balance row renders and is masked when different from current
  it('available balance row renders with data-ph-mask when different from current balance', () => {
    renderDetailPage('acc-depository-1')
    // current_balance=2500, available_balance=2300 — different, so Available Balance row should show
    expect(screen.getByText('Available Balance')).toBeInTheDocument()
    const availableEl = screen.getByText('$2,300.00')
    expect(availableEl).toHaveAttribute('data-ph-mask')
  })

  // PII masking — available balance row is NOT rendered when equal to current balance
  it('does NOT show available balance row when available_balance equals current_balance', () => {
    const sameBalanceAccount = createAccount({
      ...mockDepositoryAccount,
      id: 'acc-same-balance',
      current_balance: 5000,
      available_balance: 5000,
    })
    mockUseGetAccounts.mockReturnValue({
      data: { accounts: [sameBalanceAccount] },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mockRefetchAccounts,
    })
    renderDetailPage('acc-same-balance')
    expect(screen.queryByText('Available Balance')).not.toBeInTheDocument()
  })

  // Back link
  it('renders a Back to Accounts link pointing to /accounts', () => {
    renderDetailPage('acc-invest-1')
    const backLink = screen.getByRole('link', { name: /back to accounts/i })
    expect(backLink).toHaveAttribute('href', '/accounts')
  })

  // Display name priority: nickname > official_name > name
  it('uses nickname as display name when available', () => {
    renderDetailPage('acc-hidden-1')
    expect(screen.getByText('My Hidden Account')).toBeInTheDocument()
  })

  it('uses official_name as display name when no nickname', () => {
    renderDetailPage('acc-invest-1')
    expect(screen.getByText('Vanguard Brokerage Account')).toBeInTheDocument()
  })

  it('uses name as fallback when no nickname and no official_name', () => {
    const accountNoOfficialName = createAccount({
      ...mockDepositoryAccount,
      id: 'acc-noname-1',
      official_name: undefined,
      institution_name: 'Wells Fargo',
      name: 'WF Checking',
    })
    mockUseGetAccounts.mockReturnValue({
      data: { accounts: [accountNoOfficialName] },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mockRefetchAccounts,
    })
    renderDetailPage('acc-noname-1')
    expect(screen.getByText('WF Checking')).toBeInTheDocument()
  })
})

// Issue #1569 task 21, F4 — the Closed view's cards were dead links.
//
// AccountCard renders an unconditional <Link to={`/accounts/${id}`}> on every card, and the Closed
// accounts view (status=all_statuses) renders those cards for terminal-status accounts. This page
// queried WITHOUT a status parameter, so the server applied its `active` default and the account
// was simply absent — every closed account's card navigated to "Account not found."
//
// That was already true before this branch for a `replaced` account, but item D made it reachable
// and obvious: the Closed view is where a user goes to look at exactly these accounts, and #1569's
// whole premise is that their history is retained. A retained account you cannot open is retention
// nobody can act on.
describe('AccountDetailPage — closed accounts (Issue #1569)', () => {
  const closedAccount = {
    ...mockDepositoryAccount,
    id: 'acc-closed',
    name: 'Old Savings',
    // Cleared explicitly: displayName is nickname || official_name || name, so leaving the base
    // fixture's values in place would assert on a name this fixture never sets.
    nickname: undefined,
    official_name: undefined,
    status: 'removed',
    removed_at: '2026-03-01T00:00:00Z',
    current_balance: undefined,
    available_balance: undefined,
  }

  it('requests the full account universe, not just active accounts', () => {
    renderDetailPage('acc-investment')

    // Every call must carry it — a single un-scoped call is enough to lose the account.
    expect(mockUseGetAccounts).toHaveBeenCalled()
    for (const call of mockUseGetAccounts.mock.calls) {
      expect(call[0]).toMatchObject({ status: 'all_statuses' })
    }
  })

  it('renders a closed account instead of "Account not found"', () => {
    mockUseGetAccounts.mockReturnValue({
      data: { accounts: [closedAccount] },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mockRefetchAccounts,
    })

    renderDetailPage('acc-closed')

    expect(screen.queryByText(/account not found/i)).toBeNull()
    expect(screen.getByText('Old Savings')).toBeInTheDocument()
  })

  it('states why a closed account has no balance, with no stranded as-of date', () => {
    mockUseGetAccounts.mockReturnValue({
      data: { accounts: [closedAccount] },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mockRefetchAccounts,
    })

    renderDetailPage('acc-closed')

    // Residual #3 from the first pass: the page passed no asOfDate, so a closed account's
    // (now absent) balance was labelled "Current Balance" — a live label on a figure that does
    // not exist. AccountCard has said "Last known balance" since task 2; the two surfaces
    // disagreed about the same account.
    expect(screen.queryByText(/last known/i)).toBeNull()
    expect(screen.queryByText('Current Balance')).toBeNull()
    expect(screen.queryByText(/as of/i)).toBeNull()
    expect(screen.queryByText('$0.00')).toBeNull()
    expect(screen.getByText(/closed accounts do not carry a current balance/i)).toBeInTheDocument()
    // fix round 1, item 1: the closure date must still be reachable somewhere in the app.
    expect(screen.getByText(/closed on/i)).toBeInTheDocument()
  })

  // ── #1569: the retained BALANCE history, on EVERY account type ─────────────────────────────
  //
  // Privacy policy §8.2a promises a removed account's balance history is retained "so you can
  // still view it". Until this round the only per-account balance on this page was the single
  // last-known figure in the header (#1591); the day-by-day rows existed in balance_snapshots and
  // were reachable from nothing but the GDPR export. These tests pin WHERE the reader is mounted,
  // which is the half a component test cannot see.
  describe('retained balance history section (#1569)', () => {
    // mockUseGetAccounts is an untyped shared stub whose inferred `data` is `undefined`, so every
    // mockReturnValue in this file reports a type error. Narrowed once, here, rather than adding
    // three more to the test-typecheck ratchet.
    type AccountsQueryStub = ReturnType<typeof mockUseGetAccounts>
    const showAccounts = (accounts: Array<Record<string, unknown>>) => {
      mockUseGetAccounts.mockReturnValue({
        data: { accounts },
        isLoading: false,
        isError: false,
        error: null,
        refetch: mockRefetchAccounts,
      } as unknown as AccountsQueryStub)
    }
    const showClosed = (account: Record<string, unknown>) => showAccounts([account])

    it('mounts the retained balance history for a closed DEPOSITORY account', () => {
      // The case with no holdings history at all: before this round a closed checking account's
      // detail page had no retained financial history on it whatsoever.
      showClosed({ ...closedAccount, type: 'depository', subtype: 'checking' })
      mockUseGetAccountBalanceHistory.mockReturnValue({
        data: {
          account_id: 'acc-closed',
          points: [{ date: '2026-02-01', current_balance: 1234, available_balance: 1200, currency_code: 'USD' }],
        },
        isLoading: false,
        isError: false,
        error: null,
      })

      renderDetailPage('acc-closed')

      expect(screen.getByText('Balance history')).toBeInTheDocument()
      expect(screen.getByText('Feb 1, 2026')).toBeInTheDocument()
      expect(screen.getByText('$1,234.00')).toBeInTheDocument()
    })

    it('mounts it for a closed INVESTMENT account too, beside the holdings section', () => {
      showClosed({ ...closedAccount, type: 'investment', subtype: 'brokerage' })
      renderDetailPage('acc-closed')
      expect(screen.getByText('Balance history')).toBeInTheDocument()
      expect(screen.getByText('Holdings')).toBeInTheDocument()
    })

    // A LIVE account must not get it. Its balances are answered by the dashboard's aggregated
    // series and by its own current balance, and putting a historical table beside a live figure
    // is exactly the two-universes confusion the rest of #1569 removes. It also means a live
    // account issues no request for it at all.
    it('does not mount it for a live account', () => {
      mockUseGetAccountBalanceHistory.mockClear()
      showAccounts([mockDepositoryAccount])

      renderDetailPage('acc-depository-1')

      expect(screen.queryByText('Balance history')).toBeNull()
      expect(mockUseGetAccountBalanceHistory).not.toHaveBeenCalled()
    })
  })
})
