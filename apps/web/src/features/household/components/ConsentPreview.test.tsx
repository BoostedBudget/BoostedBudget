/**
 * ConsentPreview Component Tests
 *
 * Issue #1370: Household feature — invite-accept consent preview (Task 6)
 * TDD RED: written before implementation.
 *
 * Renders the invitee's own accounts (via the real `useActiveAccounts` hook,
 * driven through the centralized `@/test/mocks/accounts-api` mock — same
 * approach used throughout this feature's other hook-backed tests) as a
 * per-account share/hide `Switch`, defaulting every account to shared (on).
 */
import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import type { Account } from '@boostedbudget/api-client/models'
import { mockUseGetAccounts } from '@/test/mocks/accounts-api'
import { HOUSEHOLD_SHARE_CONSENT_COPY } from '../householdShareConsent'
import { ConsentPreview } from './ConsentPreview'

function makeAccount(overrides: Partial<Account> = {}): Account {
  return {
    id: 'account-1',
    user_account_id: 'ua-1',
    user_id: 'user-1',
    linked_item_id: 'item-1',
    plaid_account_id: 'plaid-1',
    name: 'Checking',
    official_name: 'Primary Checking',
    type: 'depository',
    subtype: 'checking',
    mask: '1234',
    current_balance: 1000,
    available_balance: 950,
    currency_code: 'USD',
    institution_name: 'Test Bank',
    is_hidden: false,
    exclude_investment_sync: false,
    status: 'active',
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
    ...overrides,
  }
}

/**
 * Typed wrapper for the accounts-api mock.
 *
 * The inline `mockUseGetAccounts.mockReturnValue({ ... })` form used by the older tests below
 * contributes two errors each to the tsconfig.test.json ratchet (the mock's return type is inferred
 * as `undefined`, and a plain function is not a `Mock`). New tests go through this instead so they
 * add no debt; the older call sites are left alone rather than swept into an unrelated diff.
 */
function mockAccountsResponse(accounts: Account[]) {
  mockUseGetAccounts.mockReturnValue({
    data: { accounts },
    isLoading: false,
    error: null,
    refetch: () => Promise.resolve(),
  } as unknown as Parameters<typeof mockUseGetAccounts.mockReturnValue>[0])
}

describe('ConsentPreview', () => {
  beforeEach(() => {
    mockUseGetAccounts.mockClear()
  })

  it('shows a loading state while accounts are fetching', () => {
    mockUseGetAccounts.mockReturnValue({ data: undefined, isLoading: true, error: null, refetch: () => Promise.resolve() })

    render(<ConsentPreview onHiddenAccountIdsChange={() => {}} />)

    expect(screen.getByText(/loading your accounts/i)).toBeInTheDocument()
  })

  it('shows an empty-accounts message when the invitee has no linked accounts', () => {
    mockUseGetAccounts.mockReturnValue({
      data: { accounts: [] },
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })

    render(<ConsentPreview onHiddenAccountIdsChange={() => {}} />)

    expect(screen.getByText(/no linked accounts yet/i)).toBeInTheDocument()
  })

  it('renders one row per account, each defaulted to shared (switch on)', () => {
    const accounts = [
      makeAccount({ id: 'account-1', user_account_id: 'ua-1', name: 'Checking' }),
      makeAccount({ id: 'account-2', user_account_id: 'ua-2', name: 'Savings', official_name: 'Primary Savings' }),
    ]
    mockUseGetAccounts.mockReturnValue({ data: { accounts }, isLoading: false, error: null, refetch: () => Promise.resolve() })

    render(<ConsentPreview onHiddenAccountIdsChange={() => {}} />)

    const switches = screen.getAllByRole('switch')
    expect(switches).toHaveLength(2)
    switches.forEach((sw) => expect(sw).toHaveAttribute('aria-checked', 'true'))
    expect(screen.getByText('Primary Checking')).toBeInTheDocument()
    expect(screen.getByText('Primary Savings')).toBeInTheDocument()
  })

  it('toggling an account off reports its user_account_id via onHiddenAccountIdsChange', () => {
    const accounts = [
      makeAccount({ id: 'account-1', user_account_id: 'ua-1', name: 'Checking' }),
      makeAccount({ id: 'account-2', user_account_id: 'ua-2', name: 'Savings', official_name: 'Primary Savings' }),
    ]
    mockUseGetAccounts.mockReturnValue({ data: { accounts }, isLoading: false, error: null, refetch: () => Promise.resolve() })

    let reported: string[] = []
    render(<ConsentPreview onHiddenAccountIdsChange={(ids) => { reported = ids }} />)

    const switches = screen.getAllByRole('switch')
    fireEvent.click(switches[0])

    expect(reported).toEqual(['ua-1'])
    expect(switches[0]).toHaveAttribute('aria-checked', 'false')
    expect(switches[1]).toHaveAttribute('aria-checked', 'true')
  })

  it('toggling an account back on removes it from the reported hidden ids', () => {
    const accounts = [makeAccount({ id: 'account-1', user_account_id: 'ua-1', name: 'Checking' })]
    mockUseGetAccounts.mockReturnValue({ data: { accounts }, isLoading: false, error: null, refetch: () => Promise.resolve() })

    let reported: string[] = []
    render(<ConsentPreview onHiddenAccountIdsChange={(ids) => { reported = ids }} />)

    const [sw] = screen.getAllByRole('switch')
    fireEvent.click(sw) // off
    expect(reported).toEqual(['ua-1'])
    fireEvent.click(sw) // back on
    expect(reported).toEqual([])
  })

  it('never reports account.id as a hidden id for an account with no user_account_id (backend IDOR-check guard)', () => {
    // Regression test: the backend's AcceptInvite IDOR/subset check validates
    // hidden_account_ids against the caller's own user_account fingerprint
    // ids, NOT Account.id. Falling back to account.id here would send an
    // id the backend doesn't recognize and reject the whole accept() call.
    const accountWithoutShareId = makeAccount({ id: 'account-1', user_account_id: undefined, name: 'Legacy' })
    mockUseGetAccounts.mockReturnValue({
      data: { accounts: [accountWithoutShareId] },
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })

    let reported: string[] | null = null
    render(<ConsentPreview onHiddenAccountIdsChange={(ids) => { reported = ids }} />)

    const [sw] = screen.getAllByRole('switch')
    expect(sw).toBeDisabled()
    // ⚠ #1569 T10-FIX item 8: this used to assert 'true'. AcceptInvite builds the shareable set
    // from `user_accounts`, so an account with no user_account_id can NEVER enter
    // household_account_shares — "Shared" was a false statement made on a consent screen, even
    // though it fails closed.
    expect(sw).toHaveAttribute('aria-checked', 'false')
    expect(screen.getByText(HOUSEHOLD_SHARE_CONSENT_COPY.unshareableStatus)).toBeInTheDocument()
    expect(screen.getByText(HOUSEHOLD_SHARE_CONSENT_COPY.unshareableNote)).toBeInTheDocument()

    fireEvent.click(sw)

    // The callback must never have been invoked with 'account-1' (or at all,
    // since the switch is disabled and the click is a no-op).
    expect(reported).toBeNull()
  })

  // Issue #1569 T10.3 item C: two accounts, ONE sharing preference.
  //
  // `user_account_id` is the chain/fingerprint key, and two MASKLESS accounts at one institution
  // really can share it (see backend/internal/repository/account_liveness_sql.go, which documents
  // the collision at length and refuses to collapse portfolios on it). The backend hides by that
  // id, so the sharing UNIT is the user_account, not the account.
  //
  // The component used to render one card and one switch per ACCOUNT while binding every switch to
  // the shared id. It failed CLOSED — hiding either card hid both, so nothing was ever over-shared
  // — but it presented two independently-named, independently-toggleable controls that were not
  // independent. On a consent surface a false affordance is a defect even when the failure is safe:
  // the user believes they chose to share the Brokerage and did not.
  //
  // The fix makes the UI match reality — one card per sharing unit — rather than faking a
  // per-account granularity the backend cannot honour.
  it('renders ONE control per sharing unit when two accounts share a user_account_id', () => {
    const accounts = [
      makeAccount({ id: 'account-ira', user_account_id: 'ua-shared', name: 'IRA', official_name: 'Retirement IRA', mask: undefined }),
      makeAccount({ id: 'account-brokerage', user_account_id: 'ua-shared', name: 'Brokerage', official_name: 'Taxable Brokerage', mask: undefined }),
    ]
    mockAccountsResponse(accounts)

    render(<ConsentPreview onHiddenAccountIdsChange={() => {}} />)

    expect(screen.getAllByRole('switch')).toHaveLength(1)
    // Neither account is hidden from the user: they are told what the one control covers.
    expect(screen.getByText('Retirement IRA')).toBeInTheDocument()
    expect(screen.getByText('Taxable Brokerage')).toBeInTheDocument()
  })

  it('the one control for a shared unit reports the shared id once and still fails closed', () => {
    const accounts = [
      makeAccount({ id: 'account-ira', user_account_id: 'ua-shared', name: 'IRA', official_name: 'Retirement IRA', mask: undefined }),
      makeAccount({ id: 'account-brokerage', user_account_id: 'ua-shared', name: 'Brokerage', official_name: 'Taxable Brokerage', mask: undefined }),
      makeAccount({ id: 'account-solo', user_account_id: 'ua-solo', name: 'Checking' }),
    ]
    mockAccountsResponse(accounts)

    let reported: string[] = []
    render(<ConsentPreview onHiddenAccountIdsChange={(ids) => { reported = ids }} />)

    const switches = screen.getAllByRole('switch')
    expect(switches).toHaveLength(2)

    fireEvent.click(switches[0])
    // Reported once, not twice: the payload is a list of user_account_ids, and a duplicate would
    // be a second claim about the same unit.
    expect(reported).toEqual(['ua-shared'])
    // The independent unit is untouched — the direction that would be over-SHARING if it broke.
    expect(switches[1]).toHaveAttribute('aria-checked', 'true')
  })

  it('accounts with no user_account_id are never grouped together', () => {
    // They share `undefined`, which is the absence of a sharing unit rather than a shared one.
    // Grouping on it would merge unrelated accounts into a single read-only card and hide one of
    // them from the preview entirely.
    const accounts = [
      makeAccount({ id: 'account-1', user_account_id: undefined, name: 'Legacy One', official_name: 'Legacy One' }),
      makeAccount({ id: 'account-2', user_account_id: undefined, name: 'Legacy Two', official_name: 'Legacy Two' }),
    ]
    mockAccountsResponse(accounts)

    render(<ConsentPreview onHiddenAccountIdsChange={() => {}} />)

    const switches = screen.getAllByRole('switch')
    expect(switches).toHaveLength(2)
    switches.forEach((sw) => expect(sw).toBeDisabled())
    expect(screen.getByText('Legacy One')).toBeInTheDocument()
    expect(screen.getByText('Legacy Two')).toBeInTheDocument()
  })

  it('renders the canonical invite-preview intro copy from the householdShareConsent module (issue #1372)', () => {
    // Regression guard: this copy must stay sourced from the shared module
    // so it never drifts from the ToS/Privacy Policy legal text, which is
    // written to match it.
    const accounts = [makeAccount({ id: 'account-1', user_account_id: 'ua-1', name: 'Checking' })]
    mockUseGetAccounts.mockReturnValue({ data: { accounts }, isLoading: false, error: null, refetch: () => Promise.resolve() })

    render(<ConsentPreview onHiddenAccountIdsChange={() => {}} />)

    expect(screen.getByText(HOUSEHOLD_SHARE_CONSENT_COPY.invitePreviewIntro)).toBeInTheDocument()
  })
})
