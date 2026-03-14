/**
 * AccountsPage Component Tests
 * Issue #839: Remove sync limits feature + improve empty state UX
 *
 * Tests the improved empty state (direct CTA button, benefit-focused copy)
 * and verifies the Monthly Sync Limits card is fully removed.
 *
 * NOTE: This file uses local component stubs that could pollute AccountTable
 * and AccountCard tests if run in the same process. It is intentionally
 * isolated via scripts/test-isolated.sh (#839).
 */

import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

// ============================================================================
// Local component stubs — only affect modules within this feature folder
// ============================================================================

// Simplify AccountCustomizationModal to avoid complex dependencies
mock.module('./components/AccountCustomizationModal', () => ({
  AccountCustomizationModal: () => null,
}))

// Simplify AccountCard
mock.module('./AccountCard', () => ({
  AccountCard: ({ account }: { account: { name: string } }) => (
    <div data-testid="account-card">{account.name}</div>
  ),
}))

// Simplify AccountTable
mock.module('./components/AccountTable', () => ({
  AccountTable: () => <table data-testid="account-table" />,
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
import { mockSubscriptionTiersResponse } from '@/test/fixtures/subscriptionFixtures'

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
  mockUseGetSubscriptionTiers,
  mockRefetchSubscriptionTiers,
} from '@/test/mocks/subscription-api'
import {
  mockUseCreateLinkToken,
  mockCreateLinkTokenMutateAsync,
  mockUseExchangePublicToken,
  mockExchangePublicTokenMutateAsync,
} from '@/test/mocks/plaid-api'

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
    mockUseGetAccounts.mockClear()
    mockUseDeleteAccount.mockClear()
    mockUseGetUserProfile.mockClear()
    mockUseGetSubscriptionTiers.mockClear()
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

    mockUseGetSubscriptionTiers.mockReturnValue({
      data: mockSubscriptionTiersResponse,
      isLoading: false,
      error: null,
      refetch: mockRefetchSubscriptionTiers,
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
})
