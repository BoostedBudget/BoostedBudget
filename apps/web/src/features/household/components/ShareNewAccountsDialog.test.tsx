/**
 * ShareNewAccountsDialog Component Tests
 *
 * Issue #1370: Household feature — post-Plaid-link share dialog (Task 9)
 * TDD RED: written before implementation.
 *
 * Renders the accounts just linked via Plaid (passed in by the caller —
 * this component does no fetching/diffing of its own) as per-account share
 * `Switch` rows, defaulting every account to shared (on), matching
 * ConsentPreview's shared-by-default convention. Driven through the real
 * `useHouseholdVisibility` hook, backed by the centralized `household-api`
 * mock (same approach as AccountCustomizationModal.test.tsx) — both
 * `useHousehold`/`useHouseholdVisibility` call `useQueryClient()` internally,
 * so every render needs a `QueryClientProvider` ancestor.
 */
import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import type { Account } from '@boostedbudget/api-client/models'
import {
  mockUseListMyAccountShares,
  mockRefetchMyAccountShares,
  mockUseSetHouseholdShareVisibility,
  mockSetHouseholdShareVisibilityMutateAsync,
} from '@/test/mocks/household-api'
import { mockToastError } from '@/test/mocks/sonner'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import { HOUSEHOLD_SHARE_CONSENT_COPY } from '../householdShareConsent'
import { ShareNewAccountsDialog } from './ShareNewAccountsDialog'

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

function createWrapper() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

function renderDialog(accounts: Account[], onOpenChange: (open: boolean) => void = () => {}) {
  return render(
    <ShareNewAccountsDialog open onOpenChange={onOpenChange} accounts={accounts} />,
    { wrapper: createWrapper() }
  )
}

describe('ShareNewAccountsDialog', () => {
  beforeEach(() => {
    mockUseListMyAccountShares.mockClear()
    mockRefetchMyAccountShares.mockClear()
    mockUseSetHouseholdShareVisibility.mockClear()
    mockSetHouseholdShareVisibilityMutateAsync.mockClear()
    mockToastError.mockClear()
    mockTrackEvent.mockClear()

    mockUseListMyAccountShares.mockReturnValue({
      data: [],
      isLoading: false,
      error: null,
      refetch: mockRefetchMyAccountShares,
    })
    mockUseSetHouseholdShareVisibility.mockReturnValue({
      mutateAsync: mockSetHouseholdShareVisibilityMutateAsync,
      isPending: false,
    })
    mockSetHouseholdShareVisibilityMutateAsync.mockResolvedValue(undefined)
  })

  it('never calls useHouseholdVisibility (useListMyAccountShares) when accounts is empty', () => {
    // Regression guard: AccountsPage renders <ShareNewAccountsDialog> UNCONDITIONALLY
    // (its own `open` prop just toggles Radix visibility) — including for solo
    // users, who are always passed `accounts: []`. If the household-scoped
    // useHouseholdVisibility() hook ran directly in this component's top level
    // regardless of `accounts`, it would fire a real household API request for
    // every user on the accounts page, solo included. The inner content (and
    // its hooks) must only mount once there's something to show.
    mockUseListMyAccountShares.mockClear()
    renderDialog([])

    expect(mockUseListMyAccountShares).not.toHaveBeenCalled()
    expect(screen.queryByRole('switch')).not.toBeInTheDocument()
  })

  it('renders its title and description from the canonical householdShareConsent module (issue #1372)', () => {
    // Regression guard: this copy must stay sourced from the shared module
    // so it never drifts from the ToS/Privacy Policy legal text, which is
    // written to match it.
    const accounts = [makeAccount({ id: 'account-1', user_account_id: 'ua-1', name: 'Checking' })]
    renderDialog(accounts)

    expect(
      screen.getByRole('heading', { name: HOUSEHOLD_SHARE_CONSENT_COPY.shareNewAccountsTitle })
    ).toBeInTheDocument()
    expect(screen.getByText(HOUSEHOLD_SHARE_CONSENT_COPY.shareNewAccountsDescription)).toBeInTheDocument()
  })

  it('renders one row per passed-in account, each defaulted to shared (switch on)', () => {
    const accounts = [
      makeAccount({ id: 'account-1', user_account_id: 'ua-1', name: 'Checking' }),
      makeAccount({ id: 'account-2', user_account_id: 'ua-2', name: 'Savings', official_name: 'Primary Savings' }),
    ]
    renderDialog(accounts)

    const switches = screen.getAllByRole('switch')
    expect(switches).toHaveLength(2)
    switches.forEach((sw) => expect(sw).toHaveAttribute('aria-checked', 'true'))
    expect(screen.getByText('Primary Checking')).toBeInTheDocument()
    expect(screen.getByText('Primary Savings')).toBeInTheDocument()
  })

  it('Save & Continue writes visibility per each account\'s own toggle state', async () => {
    const onOpenChange = mock(() => {})
    const accounts = [
      makeAccount({ id: 'account-1', user_account_id: 'ua-1', name: 'Checking' }),
      makeAccount({ id: 'account-2', user_account_id: 'ua-2', name: 'Savings', official_name: 'Primary Savings' }),
    ]
    renderDialog(accounts, onOpenChange)

    const switches = screen.getAllByRole('switch')
    fireEvent.click(switches[1]) // toggle second account off (hidden)

    fireEvent.click(screen.getByRole('button', { name: /save (&|and) continue/i }))

    await waitFor(() => {
      expect(mockSetHouseholdShareVisibilityMutateAsync).toHaveBeenCalledTimes(2)
    })
    expect(mockSetHouseholdShareVisibilityMutateAsync).toHaveBeenCalledWith({
      id: 'ua-1',
      data: { visibility: 'shared' },
    })
    expect(mockSetHouseholdShareVisibilityMutateAsync).toHaveBeenCalledWith({
      id: 'ua-2',
      data: { visibility: 'hidden' },
    })

    await waitFor(() => {
      expect(onOpenChange).toHaveBeenCalledWith(false)
    })
  })

  it('Skip writes shared for every account, even ones toggled off — not a silent no-op', async () => {
    const onOpenChange = mock(() => {})
    const accounts = [
      makeAccount({ id: 'account-1', user_account_id: 'ua-1', name: 'Checking' }),
      makeAccount({ id: 'account-2', user_account_id: 'ua-2', name: 'Savings', official_name: 'Primary Savings' }),
    ]
    renderDialog(accounts, onOpenChange)

    const switches = screen.getAllByRole('switch')
    fireEvent.click(switches[0]) // toggle first account off — Skip must still write 'shared' for it

    fireEvent.click(screen.getByRole('button', { name: /skip/i }))

    await waitFor(() => {
      expect(mockSetHouseholdShareVisibilityMutateAsync).toHaveBeenCalledTimes(2)
    })
    expect(mockSetHouseholdShareVisibilityMutateAsync).toHaveBeenCalledWith({
      id: 'ua-1',
      data: { visibility: 'shared' },
    })
    expect(mockSetHouseholdShareVisibilityMutateAsync).toHaveBeenCalledWith({
      id: 'ua-2',
      data: { visibility: 'shared' },
    })

    await waitFor(() => {
      expect(onOpenChange).toHaveBeenCalledWith(false)
    })
  })

  it('handles an account with no user_account_id gracefully: shown read-only, excluded from writes', async () => {
    const onOpenChange = mock(() => {})
    const accounts = [
      makeAccount({ id: 'account-1', user_account_id: undefined, name: 'Legacy' }),
      makeAccount({ id: 'account-2', user_account_id: 'ua-2', name: 'Savings', official_name: 'Primary Savings' }),
    ]
    renderDialog(accounts, onOpenChange)

    const switches = screen.getAllByRole('switch')
    expect(switches).toHaveLength(2)
    expect(switches[0]).toBeDisabled()
    // ⚠ #1569 T10-FIX item 8: this used to assert 'true'. AcceptInvite builds the shareable set
    // from `user_accounts`, so an account with no user_account_id can never enter
    // household_account_shares. Labelling it "Shared" under-shares AND misstates what happens.
    expect(switches[0]).toHaveAttribute('aria-checked', 'false')
    expect(screen.getByText(HOUSEHOLD_SHARE_CONSENT_COPY.unshareableStatus)).toBeInTheDocument()
    expect(screen.getByText(HOUSEHOLD_SHARE_CONSENT_COPY.unshareableNote)).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /save (&|and) continue/i }))

    await waitFor(() => {
      expect(mockSetHouseholdShareVisibilityMutateAsync).toHaveBeenCalledTimes(1)
    })
    expect(mockSetHouseholdShareVisibilityMutateAsync).toHaveBeenCalledWith({
      id: 'ua-2',
      data: { visibility: 'shared' },
    })

    await waitFor(() => {
      expect(onOpenChange).toHaveBeenCalledWith(false)
    })
  })

  // ── #1569 T10-FIX item 5: two accounts, ONE sharing preference ────────────────────────────
  //
  // ConsentPreview was fixed for this in T10.3 item C; this dialog, its sibling consent surface,
  // was not. It rendered a switch per ACCOUNT while calling setVisibility(user_account_id, …), so
  // two maskless accounts at one institution — forced to share a user_accounts row by migration
  // 000100 — gave the user two controls over one backend value, and Save issued two writes for it.
  it('renders ONE control per sharing unit when two accounts share a user_account_id', () => {
    const accounts = [
      makeAccount({ id: 'account-ira', user_account_id: 'ua-shared', name: 'IRA', official_name: 'Retirement IRA', mask: undefined }),
      makeAccount({ id: 'account-brokerage', user_account_id: 'ua-shared', name: 'Brokerage', official_name: 'Taxable Brokerage', mask: undefined }),
    ]
    renderDialog(accounts)

    expect(screen.getAllByRole('switch')).toHaveLength(1)
    // Neither account is hidden from the user — they are told what the one control covers.
    expect(screen.getByText('Retirement IRA')).toBeInTheDocument()
    expect(screen.getByText('Taxable Brokerage')).toBeInTheDocument()
    expect(screen.getByText(HOUSEHOLD_SHARE_CONSENT_COPY.sharedUnitNote)).toBeInTheDocument()
  })

  it('writes a shared unit ONCE, and its analytics counts it once', async () => {
    const onOpenChange = mock(() => {})
    const accounts = [
      makeAccount({ id: 'account-ira', user_account_id: 'ua-shared', name: 'IRA', official_name: 'Retirement IRA', mask: undefined }),
      makeAccount({ id: 'account-brokerage', user_account_id: 'ua-shared', name: 'Brokerage', official_name: 'Taxable Brokerage', mask: undefined }),
      makeAccount({ id: 'account-solo', user_account_id: 'ua-solo', name: 'Checking' }),
    ]
    renderDialog(accounts, onOpenChange)

    const switches = screen.getAllByRole('switch')
    expect(switches).toHaveLength(2)
    fireEvent.click(switches[0]) // hide the shared unit

    fireEvent.click(screen.getByRole('button', { name: /save (&|and) continue/i }))

    await waitFor(() => {
      expect(mockSetHouseholdShareVisibilityMutateAsync).toHaveBeenCalledTimes(2)
    })
    // Two writes for three accounts: two PUTs at one key is a race whose winner decides the value.
    expect(mockSetHouseholdShareVisibilityMutateAsync).toHaveBeenCalledWith({
      id: 'ua-shared',
      data: { visibility: 'hidden' },
    })
    expect(mockSetHouseholdShareVisibilityMutateAsync).toHaveBeenCalledWith({
      id: 'ua-solo',
      data: { visibility: 'shared' },
    })

    expect(mockTrackEvent).toHaveBeenCalledWith(
      ANALYTICS_EVENTS.HOUSEHOLD_NEW_ACCOUNTS_SHARE_SAVED,
      { method: 'save', shared_count: 1, hidden_count: 1 }
    )
  })

  it('surfaces an error and does NOT close the dialog when a write fails', async () => {
    const onOpenChange = mock(() => {})
    mockSetHouseholdShareVisibilityMutateAsync.mockRejectedValueOnce(new Error('forbidden'))
    const accounts = [
      makeAccount({ id: 'account-1', user_account_id: 'ua-1', name: 'Checking' }),
    ]
    renderDialog(accounts, onOpenChange)

    fireEvent.click(screen.getByRole('button', { name: /save (&|and) continue/i }))

    await waitFor(() => {
      expect(mockToastError).toHaveBeenCalled()
    })
    expect(onOpenChange).not.toHaveBeenCalledWith(false)
    // The inline error text (role="alert") must also be present, not just
    // the toast — the dialog surfaces failures both ways (see component
    // doc comment).
    expect(screen.getByRole('alert')).toHaveTextContent(/failed to update household sharing/i)
  })

  it('uses a partial-failure count message when only some accounts fail to write', async () => {
    const onOpenChange = mock(() => {})
    // First account's write fails, second succeeds — the message must
    // reflect "1 account" failed, not imply every account failed.
    mockSetHouseholdShareVisibilityMutateAsync
      .mockRejectedValueOnce(new Error('forbidden'))
      .mockResolvedValueOnce(undefined)
    const accounts = [
      makeAccount({ id: 'account-1', user_account_id: 'ua-1', name: 'Checking' }),
      makeAccount({ id: 'account-2', user_account_id: 'ua-2', name: 'Savings', official_name: 'Primary Savings' }),
    ]
    renderDialog(accounts, onOpenChange)

    fireEvent.click(screen.getByRole('button', { name: /save (&|and) continue/i }))

    await waitFor(() => {
      expect(mockToastError).toHaveBeenCalled()
    })
    expect(onOpenChange).not.toHaveBeenCalledWith(false)
    expect(screen.getByRole('alert')).toHaveTextContent(/failed to update household sharing for 1 account\b/i)
  })

  it('never reports account.id as the write key for an account with no user_account_id (backend IDOR-check guard)', async () => {
    // Regression test mirroring ConsentPreview's equivalent: falling back to
    // account.id would send an id the backend's share-visibility endpoint
    // doesn't recognize for this account.
    const onOpenChange = mock(() => {})
    const accounts = [makeAccount({ id: 'account-1', user_account_id: undefined, name: 'Legacy' })]
    renderDialog(accounts, onOpenChange)

    fireEvent.click(screen.getByRole('button', { name: /skip/i }))

    await waitFor(() => {
      expect(onOpenChange).toHaveBeenCalledWith(false)
    })
    expect(mockSetHouseholdShareVisibilityMutateAsync).not.toHaveBeenCalled()
  })

  describe('Analytics (#1370 Task 10)', () => {
    it('fires HOUSEHOLD_NEW_ACCOUNTS_SHARE_SAVED with method "save" and correct counts on Save & Continue', async () => {
      const onOpenChange = mock(() => {})
      const accounts = [
        makeAccount({ id: 'account-1', user_account_id: 'ua-1', name: 'Checking' }),
        makeAccount({ id: 'account-2', user_account_id: 'ua-2', name: 'Savings' }),
      ]
      renderDialog(accounts, onOpenChange)

      const switches = screen.getAllByRole('switch')
      fireEvent.click(switches[1]) // toggle second account off (hidden)

      fireEvent.click(screen.getByRole('button', { name: /save (&|and) continue/i }))

      await waitFor(() => {
        expect(mockTrackEvent).toHaveBeenCalledWith(ANALYTICS_EVENTS.HOUSEHOLD_NEW_ACCOUNTS_SHARE_SAVED, {
          method: 'save',
          shared_count: 1,
          hidden_count: 1,
        })
      })
    })

    it('fires HOUSEHOLD_NEW_ACCOUNTS_SHARE_SAVED with method "skip" and all-shared counts on Skip', async () => {
      const onOpenChange = mock(() => {})
      const accounts = [
        makeAccount({ id: 'account-1', user_account_id: 'ua-1', name: 'Checking' }),
        makeAccount({ id: 'account-2', user_account_id: 'ua-2', name: 'Savings' }),
      ]
      renderDialog(accounts, onOpenChange)

      const switches = screen.getAllByRole('switch')
      fireEvent.click(switches[0]) // toggled off, but Skip force-shares everything

      fireEvent.click(screen.getByRole('button', { name: /skip/i }))

      await waitFor(() => {
        expect(mockTrackEvent).toHaveBeenCalledWith(ANALYTICS_EVENTS.HOUSEHOLD_NEW_ACCOUNTS_SHARE_SAVED, {
          method: 'skip',
          shared_count: 2,
          hidden_count: 0,
        })
      })
    })

    it('does NOT fire HOUSEHOLD_NEW_ACCOUNTS_SHARE_SAVED when the batch write fails', async () => {
      const onOpenChange = mock(() => {})
      mockSetHouseholdShareVisibilityMutateAsync.mockRejectedValueOnce(new Error('forbidden'))
      const accounts = [makeAccount({ id: 'account-1', user_account_id: 'ua-1', name: 'Checking' })]
      renderDialog(accounts, onOpenChange)

      fireEvent.click(screen.getByRole('button', { name: /save (&|and) continue/i }))

      await waitFor(() => {
        expect(mockToastError).toHaveBeenCalled()
      })
      expect(mockTrackEvent).not.toHaveBeenCalledWith(
        ANALYTICS_EVENTS.HOUSEHOLD_NEW_ACCOUNTS_SHARE_SAVED,
        expect.anything()
      )
    })
  })
})
