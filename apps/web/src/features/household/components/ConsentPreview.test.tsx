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
    status: 'active',
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
    ...overrides,
  }
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
    expect(sw).toHaveAttribute('aria-checked', 'true')

    fireEvent.click(sw)

    // The callback must never have been invoked with 'account-1' (or at all,
    // since the switch is disabled and the click is a no-op).
    expect(reported).toBeNull()
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
