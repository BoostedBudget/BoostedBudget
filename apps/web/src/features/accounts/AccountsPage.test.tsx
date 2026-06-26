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

import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
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
import { mockNavigate } from '@/test/mocks/react-router'

// ============================================================================
// Test helper
// ============================================================================

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
    mockNavigate.mockClear()
    mockUseGetAccounts.mockClear()
    mockUseDeleteAccount.mockClear()
    mockUseGetUserProfile.mockClear()
    mockUseCreateLinkToken.mockClear()
    mockCreateLinkTokenMutateAsync.mockClear()
    mockUseExchangePublicToken.mockClear()
    mockExchangePublicTokenMutateAsync.mockClear()

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

      expect(mockNavigate).toHaveBeenCalledWith('/accounts/acc-1')
    })
  })
})
