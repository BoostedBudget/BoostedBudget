/**
 * AccountsPage Component Tests
 * Issue #839: Remove sync limits feature + improve empty state UX
 *
 * Tests the improved empty state (direct CTA button, benefit-focused copy)
 * and verifies the Monthly Sync Limits card is fully removed.
 *
 * Issue #1323: This test runs via test-conflict-files.sh (from /tmp) where the
 * inline mock.module() calls below are process-isolated and cannot pollute other
 * test files. AccountCard.test.tsx, AccountTable.test.tsx, useAccountFilters.test.ts,
 * useAccountCustomization.test.tsx, AccountCustomizationModal.test.tsx, and
 * confirmation-dialog.test.tsx run in separate groups within test-conflict-files.sh.
 */

import { describe, it, expect, mock, beforeEach, spyOn } from 'bun:test'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { createLocationHarness } from '@/test/utils/locationProbe'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

// ============================================================================
// Local component stubs — safe because this file runs from /tmp (no bunfig.toml)
// ============================================================================

// Simplify AccountCustomizationModal to avoid complex dependencies
mock.module('./components/AccountCustomizationModal', () => ({
  AccountCustomizationModal: () => null,
}))

// Issue #1323: Use centralized ConfirmationDialog stub to avoid clobbering
// @/components/ui/confirmation-dialog with an ad-hoc inline stub.
import { STUB_CONFIRMATION_DIALOG } from '@/test/mocks/confirmation-dialog'
mock.module('@/components/ui/confirmation-dialog', () => STUB_CONFIRMATION_DIALOG)

// Simplify AccountCard — exposes a Delete button that calls onDelete with the full account object
mock.module('./AccountCard', () => ({
  AccountCard: ({ account, onDelete }: { account: { name: string; [key: string]: unknown }; onDelete?: (account: unknown) => void }) => (
    <div data-testid="account-card">
      {account.name}
      <button onClick={() => onDelete?.(account)}>Delete</button>
    </div>
  ),
}))

// Simplify AccountTable — exposes row click for navigation tests
mock.module('./components/AccountTable', () => ({
  AccountTable: ({ onRowClick }: { onRowClick?: (account: { id: string; name: string }) => void }) => (
    <table data-testid="account-table">
      <tbody>
        <tr data-testid="table-row" onClick={() => onRowClick?.({ id: 'acc-1', name: 'Chase Checking' })}>
          <td>Chase Checking</td>
        </tr>
      </tbody>
    </table>
  ),
}))

// Issue #1370 Task 9: Simplify PlaidLinkButton — exposes a single button that
// fires `onSuccess` with a test-controllable ExchangePublicTokenResponse.
// The real PlaidLinkButton's own success path routes through
// react-plaid-link's `open()`, which is globally mocked to a no-op (see
// plaid-api.ts) — there is no way to drive its real onSuccess callback from
// a test. Stubbing here lets this file test AccountsPage's own
// `handleAccountsLinked` orchestration (which accounts get passed to
// ShareNewAccountsDialog, whether it opens at all) directly, without
// duplicating PlaidLinkButton's own (separately-scoped) internals.
// `item_id` (the Plaid PROVIDER's item id) is deliberately given a value
// that never matches any test account's `linked_item_id` below — proving
// AccountsPage filters on `linked_item_id`, not `item_id` (Issue #1370 Task
// 9 fix: these are genuinely different backend fields; see
// AccountsPage.tsx's handleAccountsLinked comment).
export const plaidLinkStubConfig: {
  response: { success: boolean; item_id: string; linked_item_id: string; accounts_linked: number }
} = {
  response: { success: true, item_id: 'plaid-provider-item-decoy', linked_item_id: 'item-1', accounts_linked: 1 },
}
mock.module('./PlaidLinkButton', () => ({
  PlaidLinkButton: ({ onSuccess }: { onSuccess?: (response: typeof plaidLinkStubConfig.response) => void }) => (
    <button onClick={() => onSuccess?.(plaidLinkStubConfig.response)}>Connect Bank Account</button>
  ),
}))

// Issue #1370 Task 9: Simplify the household module — controls `isInHousehold`
// per test and exposes a minimal ShareNewAccountsDialog stub that renders the
// `accounts` prop it was given (so tests can assert exactly which accounts
// AccountsPage passed in). ShareNewAccountsDialog's own per-row rendering,
// toggle state, and Save/Skip write behavior are covered by its own test
// file (ShareNewAccountsDialog.test.tsx) — this file only verifies
// AccountsPage's orchestration: gating on isInHousehold and filtering by
// linked_item_id.
export const mockUseHousehold = mock(() => ({ isInHousehold: false }))
mock.module('@/features/household', () => ({
  useHousehold: mockUseHousehold,
  ShareNewAccountsDialog: ({
    open,
    accounts,
  }: {
    open: boolean
    onOpenChange: (open: boolean) => void
    accounts: { id: string; name: string }[]
  }) =>
    open ? (
      <div data-testid="share-new-accounts-dialog">
        {accounts.map((account) => (
          <span key={account.id} data-testid="share-dialog-account">
            {account.name}
          </span>
        ))}
      </div>
    ) : null,
}))

// Mock useAccountCustomization
mock.module('./hooks/useAccountCustomization', () => ({
  useAccountCustomization: () => ({ updateAccount: mock(async () => {}), isUpdating: false }),
}))

// Mock useAccountFilters
mock.module('./hooks/useAccountFilters', () => ({
  useAccountFilters: () => ({
    institutionFilter: [],
    typeFilter: [],
    balanceRange: undefined,
    setInstitutionFilter: mock(() => {}),
    setTypeFilter: mock(() => {}),
    setBalanceRange: mock(() => {}),
  }),
}))

// ============================================================================
// Imports — after mock declarations
// ============================================================================

import { AccountsPage } from './AccountsPage'

// Import centralized mocks for configuration
import {
  mockUseGetAccounts,
  mockRefetchAccounts,
  mockUseDeleteAccount,
} from '@/test/mocks/accounts-api'
import {
  mockUseGetUserProfile,
  mockRefetchUserProfile,
} from '@/test/mocks/profile-api'
import {
  mockUseCreateLinkToken,
  mockCreateLinkTokenMutateAsync,
  mockUseExchangePublicToken,
  mockExchangePublicTokenMutateAsync,
} from '@/test/mocks/plaid-api'
// ============================================================================
// Test helper
// ============================================================================

const { locationRef, LocationProbe, reset: resetLocationProbe } = createLocationHarness()

function renderAccountsPage() {
  return render(
    <QueryClientProvider
      client={
        new QueryClient({
          defaultOptions: {
            queries: { retry: false, gcTime: 0 },
            mutations: { retry: false },
          },
        })
      }
    >
      <MemoryRouter>
        <LocationProbe />
        <AccountsPage />
      </MemoryRouter>
    </QueryClientProvider>
  )
}

const mockUser = {
  id: 'user-1',
  clerk_user_id: 'user_123',
  email: 'test@example.com',
  email_verified: true,
  subscription_tier: 'premium',
  subscription_status: 'active',
  primary_region: 'iad',
  is_active: true,
  is_deleted: false,
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-01T00:00:00Z',
}

describe('AccountsPage', () => {
  beforeEach(() => {
    // Clear all mock call history
    resetLocationProbe()
    mockUseGetAccounts.mockClear()
    mockUseDeleteAccount.mockClear()
    mockUseGetUserProfile.mockClear()
    mockUseCreateLinkToken.mockClear()
    mockCreateLinkTokenMutateAsync.mockClear()
    mockUseExchangePublicToken.mockClear()
    mockExchangePublicTokenMutateAsync.mockClear()
    mockUseHousehold.mockClear()
    mockUseHousehold.mockReturnValue({ isInHousehold: false })
    plaidLinkStubConfig.response = { success: true, item_id: 'plaid-provider-item-decoy', linked_item_id: 'item-1', accounts_linked: 1 }

    // Configure default: loaded state with premium user, unlimited accounts
    mockUseGetUserProfile.mockReturnValue({
      data: mockUser,
      isLoading: false,
      error: null,
      refetch: mockRefetchUserProfile,
    })

    mockUseCreateLinkToken.mockReturnValue({
      mutateAsync: mockCreateLinkTokenMutateAsync,
      isPending: false,
      isError: false,
      error: null,
    })

    mockUseExchangePublicToken.mockReturnValue({
      mutateAsync: mockExchangePublicTokenMutateAsync,
      isPending: false,
      isError: false,
      error: null,
    })
  })

  describe('empty state (0 accounts)', () => {
    beforeEach(() => {
      mockUseGetAccounts.mockReturnValue({
        data: { accounts: [] },
        isLoading: false,
        error: null,
        refetch: mockRefetchAccounts,
      })
    })

    it('shows the improved empty state title', () => {
      renderAccountsPage()
      expect(screen.getByText('Connect your first bank account')).toBeInTheDocument()
    })

    it('shows the benefit-focused description', () => {
      renderAccountsPage()
      expect(
        screen.getByText(/automatically track balances, transactions, and spending/i)
      ).toBeInTheDocument()
    })

    it('shows a Connect Bank Account CTA button inside the empty state', async () => {
      renderAccountsPage()
      // PlaidLinkButton async loads and then renders a "Connect Bank Account" button
      const ctaButtons = await screen.findAllByRole('button', { name: /connect bank account/i })
      expect(ctaButtons.length).toBeGreaterThan(0)
    })

    it('does not show Monthly Sync Limits card', () => {
      renderAccountsPage()
      expect(screen.queryByText('Monthly Sync Limits')).not.toBeInTheDocument()
    })

    it('does not show account list or count', () => {
      renderAccountsPage()
      expect(screen.queryByTestId('account-card')).not.toBeInTheDocument()
      expect(screen.queryByTestId('account-table')).not.toBeInTheDocument()
    })
  })

  describe('loaded state (accounts present)', () => {
    beforeEach(() => {
      mockUseGetAccounts.mockReturnValue({
        data: {
          accounts: [
            {
              id: 'acc-1',
              name: 'Chase Checking',
              institution_name: 'Chase',
              type: 'depository',
              subtype: 'checking',
              status: 'active',
              is_hidden: false,
              current_balance: 1000,
              created_at: '2025-01-01T00:00:00Z',
            },
          ],
        },
        isLoading: false,
        error: null,
        refetch: mockRefetchAccounts,
      })
    })

    it('does not show the empty state', () => {
      renderAccountsPage()
      expect(screen.queryByText('Connect your first bank account')).not.toBeInTheDocument()
    })

    it('shows account cards', () => {
      renderAccountsPage()
      expect(screen.getByTestId('account-card')).toBeInTheDocument()
    })

    it('does not show Monthly Sync Limits card', () => {
      renderAccountsPage()
      expect(screen.queryByText('Monthly Sync Limits')).not.toBeInTheDocument()
    })
  })

  describe('delete dialog integration (Task 7)', () => {
    const mockAccount = {
      id: 'acc-1',
      linked_item_id: 'item-1',
      name: 'Chase Checking',
      institution_name: 'Chase',
      type: 'depository',
      subtype: 'checking',
      status: 'active',
      is_hidden: false,
      current_balance: 1000,
      created_at: '2025-01-01T00:00:00Z',
    }

    beforeEach(() => {
      mockUseGetAccounts.mockReturnValue({
        data: { accounts: [mockAccount] },
        isLoading: false,
        error: null,
        refetch: mockRefetchAccounts,
      })
      mockUseDeleteAccount.mockReturnValue({
        mutateAsync: mock(() => Promise.resolve()),
        isPending: false,
        isError: false,
        error: null,
      })
    })

    it('does not show ConfirmationDialog initially', () => {
      renderAccountsPage()
      expect(screen.queryByTestId('confirm-delete-dialog')).not.toBeInTheDocument()
    })

    it('shows ConfirmationDialog when account delete is triggered from card', () => {
      renderAccountsPage()

      fireEvent.click(screen.getByText('Delete'))

      expect(screen.getByTestId('confirm-delete-dialog')).toBeInTheDocument()
    })

    it('passes correct title to ConfirmationDialog', () => {
      renderAccountsPage()

      fireEvent.click(screen.getByText('Delete'))

      expect(screen.getByTestId('confirm-delete-dialog')).toHaveAttribute('data-title', 'Delete Account')
    })

    it('passes account name as confirmText to ConfirmationDialog', () => {
      renderAccountsPage()

      fireEvent.click(screen.getByText('Delete'))

      expect(screen.getByTestId('confirm-delete-dialog')).toHaveAttribute('data-confirm-text', 'Chase Checking')
    })

    it('shows transaction-preserved and balance-removed messages in description', () => {
      renderAccountsPage()

      fireEvent.click(screen.getByText('Delete'))

      const desc = screen.getByTestId('dialog-description')
      expect(desc.textContent).toContain('transactions will be preserved')
      expect(desc.textContent).toContain('Balance history will be removed')
    })

    it('shows institution removal message when account is the last for its linked_item_id', () => {
      // Only one account → last for item-1
      renderAccountsPage()
      fireEvent.click(screen.getByText('Delete'))

      const desc = screen.getByTestId('dialog-description')
      expect(desc.textContent).toContain('bank connection to')
      expect(desc.textContent).toContain('Chase')
    })

    it('does NOT show institution removal message when other accounts share the same linked_item_id', () => {
      const secondAccount = {
        ...mockAccount,
        id: 'acc-2',
        name: 'Chase Savings',
        linked_item_id: 'item-1', // same linked_item_id
      }
      mockUseGetAccounts.mockReturnValue({
        data: { accounts: [mockAccount, secondAccount] },
        isLoading: false,
        error: null,
        refetch: mockRefetchAccounts,
      })

      renderAccountsPage()

      // Click delete on the first card
      const deleteButtons = screen.getAllByText('Delete')
      fireEvent.click(deleteButtons[0])

      const desc = screen.getByTestId('dialog-description')
      expect(desc.textContent).not.toContain('bank connection')
    })
  })

  describe('table row click navigation (Task 6)', () => {
    beforeEach(() => {
      mockUseGetAccounts.mockReturnValue({
        data: {
          accounts: [
            {
              id: 'acc-1',
              name: 'Chase Checking',
              institution_name: 'Chase',
              type: 'depository',
              subtype: 'checking',
              status: 'active',
              is_hidden: false,
              current_balance: 1000,
              created_at: '2025-01-01T00:00:00Z',
            },
          ],
        },
        isLoading: false,
        error: null,
        refetch: mockRefetchAccounts,
      })
    })

    it('passes onRowClick to AccountTable and navigates to /accounts/:id on row click', () => {
      renderAccountsPage()

      // Switch to table view to render AccountTable
      const tableToggle = screen.getByTitle('Table view')
      fireEvent.click(tableToggle)

      // Click the row — the stub fires onRowClick({ id: 'acc-1', name: 'Chase Checking' })
      const row = screen.getByTestId('table-row')
      fireEvent.click(row)

      expect(locationRef.current.pathname).toBe('/accounts/acc-1')
    })
  })

  describe('post-Plaid-link household share dialog (Task 9)', () => {
    const newAccount = {
      id: 'acc-new',
      name: 'New Checking',
      linked_item_id: 'item-1',
      institution_name: 'Chase',
      type: 'depository',
      subtype: 'checking',
      status: 'active',
      is_hidden: false,
      current_balance: 500,
      created_at: '2025-01-01T00:00:00Z',
    }
    const unrelatedAccount = {
      id: 'acc-unrelated',
      name: 'Unrelated Savings',
      linked_item_id: 'item-2',
      institution_name: 'Ally',
      type: 'depository',
      subtype: 'savings',
      status: 'active',
      is_hidden: false,
      current_balance: 2000,
      created_at: '2025-01-01T00:00:00Z',
    }

    beforeEach(() => {
      mockUseGetAccounts.mockReturnValue({
        data: { accounts: [] },
        isLoading: false,
        error: null,
        refetch: mockRefetchAccounts,
      })
    })

    it('opens the share dialog with only the just-linked accounts when in a household', async () => {
      mockUseHousehold.mockReturnValue({ isInHousehold: true })
      mockRefetchAccounts.mockResolvedValueOnce({
        data: { accounts: [newAccount, unrelatedAccount] },
      })

      renderAccountsPage()

      const [ctaButton] = await screen.findAllByRole('button', { name: /connect bank account/i })
      fireEvent.click(ctaButton)

      const dialog = await screen.findByTestId('share-new-accounts-dialog')
      const accountNames = within(dialog)
        .getAllByTestId('share-dialog-account')
        .map((el) => el.textContent)
      expect(accountNames).toEqual(['New Checking'])
    })

    it('never opens the share dialog for a solo (non-household) user', async () => {
      mockUseHousehold.mockReturnValue({ isInHousehold: false })
      mockRefetchAccounts.mockResolvedValueOnce({
        data: { accounts: [newAccount] },
      })

      renderAccountsPage()

      const [ctaButton] = await screen.findAllByRole('button', { name: /connect bank account/i })
      fireEvent.click(ctaButton)

      await waitFor(() => expect(mockRefetchAccounts).toHaveBeenCalled())
      expect(screen.queryByTestId('share-new-accounts-dialog')).not.toBeInTheDocument()
    })

    it('filters strictly by linked_item_id === the response linked_item_id, not "any newly present account", and NOT by item_id', async () => {
      mockUseHousehold.mockReturnValue({ isInHousehold: true })
      // The exchange response reports linked_item_id "item-2", even though
      // item-1's account is also "newly present" in this refetch — only the
      // item-2 account must be treated as just-linked. `item_id` is set to a
      // decoy value that doesn't match ANY test account's linked_item_id —
      // if AccountsPage regressed to filtering on `item_id` (the bug this
      // test guards against), zero accounts would match and this assertion
      // would fail.
      plaidLinkStubConfig.response = {
        success: true,
        item_id: 'plaid-provider-item-decoy',
        linked_item_id: 'item-2',
        accounts_linked: 1,
      }
      mockRefetchAccounts.mockResolvedValueOnce({
        data: { accounts: [newAccount, unrelatedAccount] },
      })

      renderAccountsPage()

      const [ctaButton] = await screen.findAllByRole('button', { name: /connect bank account/i })
      fireEvent.click(ctaButton)

      const dialog = await screen.findByTestId('share-new-accounts-dialog')
      const accountNames = within(dialog)
        .getAllByTestId('share-dialog-account')
        .map((el) => el.textContent)
      expect(accountNames).toEqual(['Unrelated Savings'])
    })

    it('does not open the dialog when no account matches the linked_item_id', async () => {
      mockUseHousehold.mockReturnValue({ isInHousehold: true })
      plaidLinkStubConfig.response = {
        success: true,
        item_id: 'plaid-provider-item-decoy',
        linked_item_id: 'item-does-not-exist',
        accounts_linked: 1,
      }
      mockRefetchAccounts.mockResolvedValueOnce({
        data: { accounts: [newAccount, unrelatedAccount] },
      })

      renderAccountsPage()

      const [ctaButton] = await screen.findAllByRole('button', { name: /connect bank account/i })
      fireEvent.click(ctaButton)

      await waitFor(() => expect(mockRefetchAccounts).toHaveBeenCalled())
      expect(screen.queryByTestId('share-new-accounts-dialog')).not.toBeInTheDocument()
    })

    it('never lets a refetchAccounts failure become an unhandled rejection (PlaidLinkButton does not await/catch onSuccess)', async () => {
      // Regression guard: handleAccountsLinked is now `async`, but
      // PlaidLinkButton.tsx's real onSuccess call site is fire-and-forget
      // (no `await`, no `.catch()`). If handleAccountsLinked didn't catch
      // its own errors, a refetchAccounts() rejection would become an
      // unhandled promise rejection instead of being handled gracefully.
      mockUseHousehold.mockReturnValue({ isInHousehold: true })
      const consoleErrorSpy = spyOn(console, 'error').mockImplementation(() => {})
      mockRefetchAccounts.mockRejectedValueOnce(new Error('network error'))

      renderAccountsPage()

      const [ctaButton] = await screen.findAllByRole('button', { name: /connect bank account/i })
      fireEvent.click(ctaButton)

      await waitFor(() => {
        expect(consoleErrorSpy).toHaveBeenCalledWith(
          'Failed to refresh accounts after linking:',
          expect.any(Error)
        )
      })
      expect(screen.queryByTestId('share-new-accounts-dialog')).not.toBeInTheDocument()

      consoleErrorSpy.mockRestore()
    })
  })
})
