/**
 * AccountsPage Component Tests
 * Issue #839: Remove sync limits feature + improve empty state UX
 *
 * Tests the improved empty state (direct CTA button, benefit-focused copy)
 * and verifies the Monthly Sync Limits card is fully removed.
 */

import { describe, it, expect, mock, beforeEach } from 'bun:test'

// ============================================================================
// Mocks — must be declared before importing the component
// ============================================================================

const mockRefetchAccounts = mock(() => {})
const mockRefetchProfile = mock(() => {})

const mockUseGetAccounts = mock(() => ({
  data: { accounts: [] },
  isLoading: false,
  error: null,
  refetch: mockRefetchAccounts,
}))

const mockUseDeleteAccount = mock(() => ({
  mutateAsync: mock(async () => {}),
}))

const mockUseCreateLinkToken = mock(() => ({
  mutateAsync: mock(async () => ({ link_token: 'test-token' })),
}))

const mockUseExchangePublicToken = mock(() => ({
  mutateAsync: mock(async () => ({})),
  isPending: false,
}))

const mockUseProfile = mock(() => ({
  user: { id: 'user-1', subscription_tier: 'premium', effective_tier: null },
  loading: false,
  refetch: mockRefetchProfile,
}))

const mockUseSubscriptionTiers = mock(() => ({
  tiers: {
    tiers: {
      premium: { limits: { accounts: -1 } },
    },
  },
  loading: false,
}))

const mockUseTierLimits = mock(() => ({
  isAtLimit: false,
  shouldShowUpgradeBanner: false,
  accountCountDisplay: '0 accounts',
  isUnlimited: true,
}))

mock.module('@boostedbudget/api-client/endpoints/accounts/accounts', () => ({
  useGetAccounts: (...args: unknown[]) => mockUseGetAccounts(...args),
  useDeleteAccount: (...args: unknown[]) => mockUseDeleteAccount(...args),
  getGetAccountsQueryKey: () => ['accounts'],
}))

mock.module('@boostedbudget/api-client/endpoints/plaid/plaid', () => ({
  useCreateLinkToken: (...args: unknown[]) => mockUseCreateLinkToken(...args),
  useExchangePublicToken: (...args: unknown[]) => mockUseExchangePublicToken(...args),
}))

mock.module('../profile/hooks/useProfile', () => ({
  useProfile: (...args: unknown[]) => mockUseProfile(...args),
}))

mock.module('../subscription/hooks/useSubscriptionTiers', () => ({
  useSubscriptionTiers: (...args: unknown[]) => mockUseSubscriptionTiers(...args),
}))

mock.module('../subscription/hooks/useTierLimits', () => ({
  useTierLimits: (...args: unknown[]) => mockUseTierLimits(...args),
}))

// Mock react-plaid-link to avoid browser environment requirements
mock.module('react-plaid-link', () => ({
  usePlaidLink: () => ({ open: mock(() => {}), ready: true }),
}))

// Mock react-router-dom navigate
mock.module('react-router-dom', () => ({
  useNavigate: () => mock(() => {}),
  useSearchParams: () => [new URLSearchParams(), mock(() => {})],
  Link: ({ children, to }: { children: React.ReactNode; to: string }) => (
    <a href={to}>{children}</a>
  ),
}))

// Mock react-query client
mock.module('@tanstack/react-query', () => ({
  useQueryClient: () => ({ setQueryData: mock(() => {}) }),
}))

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

import { render, screen } from '@testing-library/react'
import { AccountsPage } from './AccountsPage'

describe('AccountsPage', () => {
  beforeEach(() => {
    mockUseGetAccounts.mockClear()
    mockUseProfile.mockClear()
    mockUseSubscriptionTiers.mockClear()
    mockUseTierLimits.mockClear()
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
      render(<AccountsPage />)
      expect(screen.getByText('Connect your first bank account')).toBeInTheDocument()
    })

    it('shows the benefit-focused description', () => {
      render(<AccountsPage />)
      expect(
        screen.getByText(/automatically track balances, transactions, and spending/i)
      ).toBeInTheDocument()
    })

    it('shows a Connect Bank Account CTA button inside the empty state', async () => {
      render(<AccountsPage />)
      // PlaidLinkButton async loads and then renders a "Connect Bank Account" button
      const ctaButtons = await screen.findAllByRole('button', { name: /connect bank account/i })
      expect(ctaButtons.length).toBeGreaterThan(0)
    })

    it('does not show Monthly Sync Limits card', () => {
      render(<AccountsPage />)
      expect(screen.queryByText('Monthly Sync Limits')).not.toBeInTheDocument()
    })

    it('does not show account list or count', () => {
      render(<AccountsPage />)
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
      render(<AccountsPage />)
      expect(screen.queryByText('Connect your first bank account')).not.toBeInTheDocument()
    })

    it('shows account cards', () => {
      render(<AccountsPage />)
      expect(screen.getByTestId('account-card')).toBeInTheDocument()
    })

    it('does not show Monthly Sync Limits card', () => {
      render(<AccountsPage />)
      expect(screen.queryByText('Monthly Sync Limits')).not.toBeInTheDocument()
    })
  })
})
