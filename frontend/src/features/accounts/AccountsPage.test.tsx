import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { AccountsPage } from './AccountsPage'
import * as plaidApi from './api/plaidApi'
import * as useProfileHook from '../profile/hooks/useProfile'
import { useSubscriptionTiers } from '../subscription/hooks/useSubscriptionTiers'
import type { GetAccountsResponse, Account } from './types'
import type { User } from '../profile/types'
import { mockSubscriptionTiersResponse } from '@/test/fixtures/subscriptionFixtures'

// Mock the Plaid API
vi.mock('./api/plaidApi')

// Mock the subscription tiers hook - MUST be hoisted before component imports
vi.mock('../subscription/hooks/useSubscriptionTiers', () => ({
  useSubscriptionTiers: vi.fn(),
}))

// Mock the account customization hook to avoid React Query provider issues
vi.mock('./hooks/useAccountCustomization', () => ({
  useAccountCustomization: vi.fn().mockReturnValue({
    updateAccount: vi.fn().mockResolvedValue(null),
    isUpdating: false,
    updateError: null,
    reorderAccounts: vi.fn().mockResolvedValue(undefined),
    isReordering: false,
    reorderError: null,
  }),
}))

// Mock the useProfile hook to avoid React Query provider issues
vi.mock('../profile/hooks/useProfile', () => ({
  useProfile: vi.fn(),
}))

// Mock PlaidLinkButton component to simplify testing
vi.mock('./PlaidLinkButton', () => ({
  PlaidLinkButton: ({ onSuccess, disabled }: { onSuccess?: () => void; disabled?: boolean }) => (
    <button
      onClick={() => onSuccess?.()}
      data-testid="plaid-link-button"
      disabled={disabled}
    >
      Connect Bank Account
    </button>
  ),
}))

/**
 * Test wrapper component that provides React Router context
 */
function renderWithRouter(ui: React.ReactElement) {
  return render(
    <MemoryRouter>
      {ui}
    </MemoryRouter>
  )
}

describe('AccountsPage', () => {
  const mockUser: User = {
    id: '550e8400-e29b-41d4-a716-446655440000',
    auth0_user_id: 'auth0|507f1f77bcf86cd799439011',
    email: 'user@example.com',
    email_verified: true,
    subscription_tier: 'premium_plus', // Default to unlimited to not affect old tests
    subscription_status: 'active',
    primary_region: 'iad',
    is_active: true,
    is_deleted: false,
    created_at: '2025-10-18T14:30:00Z',
    updated_at: '2025-10-20T10:15:00Z',
    last_login_at: '2025-10-25T08:00:00Z',
  }

  beforeEach(() => {
    vi.restoreAllMocks()

    // Default mocks for hooks used in ALL tests
    vi.mocked(useSubscriptionTiers).mockReturnValue({
      tiers: mockSubscriptionTiersResponse,
      loading: false,
      error: null,
      refetch: vi.fn(),
    })

    // Mock useProfile hook
    vi.mocked(useProfileHook.useProfile).mockReturnValue({
      user: mockUser,
      loading: false,
      error: null,
      refetch: vi.fn(),
    })
  })

  const mockAccounts: Account[] = [
    {
      id: '550e8400-e29b-41d4-a716-446655440000',
      linked_item_id: '770e9511-f3ac-52e5-b827-557766551112',
      user_id: 'auth0|507f1f77bcf86cd799439011',
      plaid_account_id: 'plaid-acc-1',
      name: 'Chase Checking',
      official_name: 'Chase Total Checking',
      type: 'depository',
      subtype: 'checking',
      mask: '0000',
      current_balance: 2500.50,
      available_balance: 2300.50,
      currency_code: 'USD',
      institution_name: 'Chase',
      is_hidden: false,
      created_at: '2025-10-18T14:30:00Z',
      updated_at: '2025-10-20T10:15:00Z',
    },
    {
      id: '660f9511-f3ac-52e5-b827-557766551111',
      linked_item_id: '770e9511-f3ac-52e5-b827-557766551112',
      user_id: 'auth0|507f1f77bcf86cd799439011',
      plaid_account_id: 'plaid-acc-2',
      name: 'Wells Fargo Savings',
      type: 'depository',
      subtype: 'savings',
      mask: '1234',
      current_balance: 10000.00,
      available_balance: 10000.00,
      currency_code: 'USD',
      institution_name: 'Wells Fargo',
      is_hidden: false,
      created_at: '2025-10-19T09:20:00Z',
      updated_at: '2025-10-20T10:15:00Z',
    },
  ]

  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('should show loading state initially', () => {
    vi.mocked(plaidApi.fetchAccounts).mockImplementation(() => new Promise(() => {}))

    renderWithRouter(<AccountsPage />)

    expect(screen.getByText('Loading your accounts...')).toBeInTheDocument()
  })

  it('should fetch and display accounts on mount', async () => {
    const mockResponse: GetAccountsResponse = {
      accounts: mockAccounts,
      total_count: 2,
    }

    vi.mocked(plaidApi.fetchAccounts).mockResolvedValue(mockResponse)

    renderWithRouter(<AccountsPage />)

    // Wait for loading to finish
    await waitFor(() => {
      expect(screen.queryByText('Loading your accounts...')).not.toBeInTheDocument()
    })

    // Check that accounts are displayed
    expect(screen.getByText('My Accounts')).toBeInTheDocument()
    // Default mock uses premium_plus (unlimited), so should show "2 accounts" (no limit display)
    expect(screen.getByText(/^2 accounts$/i)).toBeInTheDocument()
    expect(screen.getByText('Chase Total Checking')).toBeInTheDocument()
    expect(screen.getByText('Wells Fargo Savings')).toBeInTheDocument()
  })

  it('should display empty state when no accounts exist', async () => {
    const emptyResponse: GetAccountsResponse = {
      accounts: [],
      total_count: 0,
    }

    vi.mocked(plaidApi.fetchAccounts).mockResolvedValue(emptyResponse)

    renderWithRouter(<AccountsPage />)

    await waitFor(() => {
      expect(screen.queryByText('Loading your accounts...')).not.toBeInTheDocument()
    })

    expect(screen.getByText(/No accounts connected yet/i)).toBeInTheDocument()
    expect(screen.getByText(/Click "Connect Bank Account" above to get started/i)).toBeInTheDocument()
  })

  it('should display error state when fetch fails', async () => {
    vi.mocked(plaidApi.fetchAccounts).mockRejectedValue(new Error('Network error'))

    renderWithRouter(<AccountsPage />)

    await waitFor(() => {
      expect(screen.queryByText('Loading your accounts...')).not.toBeInTheDocument()
    })

    expect(screen.getByText('Failed to load accounts. Please try again.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument()
  })

  it('should retry loading when Retry button is clicked', async () => {
    const user = userEvent.setup()

    // First call fails
    vi.mocked(plaidApi.fetchAccounts).mockRejectedValueOnce(new Error('Network error'))

    renderWithRouter(<AccountsPage />)

    await waitFor(() => {
      expect(screen.getByText('Failed to load accounts. Please try again.')).toBeInTheDocument()
    })

    // Second call succeeds
    const mockResponse: GetAccountsResponse = {
      accounts: mockAccounts,
      total_count: 2,
    }
    vi.mocked(plaidApi.fetchAccounts).mockResolvedValue(mockResponse)

    // Click retry button
    const retryButton = screen.getByRole('button', { name: 'Retry' })
    await user.click(retryButton)

    // Wait for accounts to load (loading state may resolve too quickly to test)
    await waitFor(() => {
      expect(screen.getByText(/^2 accounts$/i)).toBeInTheDocument()
    })

    expect(screen.getByText('Chase Total Checking')).toBeInTheDocument()
  })

  it('should reload accounts after successful Plaid Link flow', async () => {
    const user = userEvent.setup()

    // Initial load with no accounts
    vi.mocked(plaidApi.fetchAccounts).mockResolvedValueOnce({
      accounts: [],
      total_count: 0,
    })

    renderWithRouter(<AccountsPage />)

    await waitFor(() => {
      expect(screen.getByText(/No accounts connected yet/i)).toBeInTheDocument()
    })

    // After PlaidLink success, reload with accounts
    vi.mocked(plaidApi.fetchAccounts).mockResolvedValue({
      accounts: mockAccounts,
      total_count: 2,
    })

    // Simulate PlaidLink success
    const plaidButton = screen.getByTestId('plaid-link-button')
    await user.click(plaidButton)

    // Should reload and show accounts
    await waitFor(() => {
      expect(screen.getByText(/^2 accounts$/i)).toBeInTheDocument()
    })
  })

  it('should display single account correctly', async () => {
    const singleAccountResponse: GetAccountsResponse = {
      accounts: [mockAccounts[0]],
      total_count: 1,
    }

    vi.mocked(plaidApi.fetchAccounts).mockResolvedValue(singleAccountResponse)

    renderWithRouter(<AccountsPage />)

    await waitFor(() => {
      expect(screen.queryByText('Loading your accounts...')).not.toBeInTheDocument()
    })

    // Singular form
    expect(screen.getByText(/^1 account$/i)).toBeInTheDocument()
  })

  it('should render PlaidLinkButton for adding more accounts', async () => {
    const mockResponse: GetAccountsResponse = {
      accounts: mockAccounts,
      total_count: 2,
    }

    vi.mocked(plaidApi.fetchAccounts).mockResolvedValue(mockResponse)

    renderWithRouter(<AccountsPage />)

    await waitFor(() => {
      expect(screen.getByText(/^2 accounts$/i)).toBeInTheDocument()
    })

    // PlaidLinkButton should be visible even with existing accounts
    expect(screen.getByTestId('plaid-link-button')).toBeInTheDocument()
  })

  // TDD RED: Account Limit Enforcement Tests (Issue #174)
  describe('Account Limit Enforcement', () => {
    const mockFreeUser: User = {
      id: '550e8400-e29b-41d4-a716-446655440000',
      auth0_user_id: 'auth0|507f1f77bcf86cd799439011',
      email: 'user@example.com',
      email_verified: true,
      subscription_tier: 'free',
      subscription_status: 'active',
      primary_region: 'iad',
      is_active: true,
      is_deleted: false,
      created_at: '2025-10-18T14:30:00Z',
      updated_at: '2025-10-20T10:15:00Z',
      last_login_at: '2025-10-25T08:00:00Z',
    }

    const mockPremiumUser: User = {
      ...mockFreeUser,
      subscription_tier: 'premium',
    }

    const mockPremiumPlusUser: User = {
      ...mockFreeUser,
      subscription_tier: 'premium_plus',
    }

    it('should show account usage (X/Y accounts used) for free tier', async () => {
      vi.mocked(useSubscriptionTiers).mockReturnValue({
        tiers: mockSubscriptionTiersResponse,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      vi.mocked(useProfileHook.useProfile).mockReturnValue({
        user: mockFreeUser,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })
      vi.mocked(plaidApi.fetchAccounts).mockResolvedValue({
        accounts: mockAccounts,
        total_count: 2,
      })

      renderWithRouter(<AccountsPage />)

      await waitFor(() => {
        expect(screen.queryByText('Loading your accounts...')).not.toBeInTheDocument()
      })

      // Should show "2/3 accounts used" for free tier
      expect(screen.getByText(/2\/3 accounts used/i)).toBeInTheDocument()
    })

    it('should disable PlaidLinkButton when at account limit', async () => {
      vi.mocked(useSubscriptionTiers).mockReturnValue({
        tiers: mockSubscriptionTiersResponse,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      // User has 3 accounts (at limit for free tier)
      const threeAccounts = [...mockAccounts, { ...mockAccounts[0], id: 'third-account' }]

      vi.mocked(useProfileHook.useProfile).mockReturnValue({
        user: mockFreeUser,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })
      vi.mocked(plaidApi.fetchAccounts).mockResolvedValue({
        accounts: threeAccounts,
        total_count: 3,
      })

      renderWithRouter(<AccountsPage />)

      await waitFor(() => {
        expect(screen.queryByText('Loading your accounts...')).not.toBeInTheDocument()
      })

      // PlaidLinkButton should be disabled
      const plaidButton = screen.getByTestId('plaid-link-button')
      expect(plaidButton).toBeDisabled()
    })

    it('should show upgrade banner when at account limit', async () => {
      vi.mocked(useSubscriptionTiers).mockReturnValue({
        tiers: mockSubscriptionTiersResponse,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      // User has 3 accounts (at limit for free tier)
      const threeAccounts = [...mockAccounts, { ...mockAccounts[0], id: 'third-account' }]

      vi.mocked(useProfileHook.useProfile).mockReturnValue({
        user: mockFreeUser,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })
      vi.mocked(plaidApi.fetchAccounts).mockResolvedValue({
        accounts: threeAccounts,
        total_count: 3,
      })

      renderWithRouter(<AccountsPage />)

      await waitFor(() => {
        expect(screen.queryByText('Loading your accounts...')).not.toBeInTheDocument()
      })

      // Upgrade banner should be shown
      expect(screen.getByText(/account limit reached/i)).toBeInTheDocument()
      expect(screen.getByText(/upgrade/i)).toBeInTheDocument()
    })

    it('should enable PlaidLinkButton when under account limit', async () => {
      vi.mocked(useSubscriptionTiers).mockReturnValue({
        tiers: mockSubscriptionTiersResponse,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      vi.mocked(useProfileHook.useProfile).mockReturnValue({
        user: mockFreeUser,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })
      vi.mocked(plaidApi.fetchAccounts).mockResolvedValue({
        accounts: mockAccounts, // 2 accounts, under limit of 3
        total_count: 2,
      })

      renderWithRouter(<AccountsPage />)

      await waitFor(() => {
        expect(screen.queryByText('Loading your accounts...')).not.toBeInTheDocument()
      })

      // PlaidLinkButton should be enabled
      const plaidButton = screen.getByTestId('plaid-link-button')
      expect(plaidButton).not.toBeDisabled()
    })

    it('should show "X accounts" for unlimited tier (no limit display)', async () => {
      vi.mocked(useSubscriptionTiers).mockReturnValue({
        tiers: mockSubscriptionTiersResponse,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      vi.mocked(useProfileHook.useProfile).mockReturnValue({
        user: mockPremiumPlusUser,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })
      vi.mocked(plaidApi.fetchAccounts).mockResolvedValue({
        accounts: mockAccounts,
        total_count: 2,
      })

      renderWithRouter(<AccountsPage />)

      await waitFor(() => {
        expect(screen.queryByText('Loading your accounts...')).not.toBeInTheDocument()
      })

      // Should show "2 accounts" (no limit display)
      expect(screen.getByText(/^2 accounts$/i)).toBeInTheDocument()
      // Should NOT show usage like "2/X"
      expect(screen.queryByText(/\//)).not.toBeInTheDocument()
    })

    it('should not show upgrade banner for premium_plus tier even with many accounts', async () => {
      vi.mocked(useSubscriptionTiers).mockReturnValue({
        tiers: mockSubscriptionTiersResponse,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      vi.mocked(useProfileHook.useProfile).mockReturnValue({
        user: mockPremiumPlusUser,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })
      vi.mocked(plaidApi.fetchAccounts).mockResolvedValue({
        accounts: mockAccounts,
        total_count: 2,
      })

      renderWithRouter(<AccountsPage />)

      await waitFor(() => {
        expect(screen.queryByText('Loading your accounts...')).not.toBeInTheDocument()
      })

      // No upgrade banner for unlimited tier
      expect(screen.queryByText(/account limit reached/i)).not.toBeInTheDocument()
      expect(screen.queryByText(/upgrade/i)).not.toBeInTheDocument()
    })

    it('should show correct limit for premium tier (30 accounts)', async () => {
      vi.mocked(useSubscriptionTiers).mockReturnValue({
        tiers: mockSubscriptionTiersResponse,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })

      vi.mocked(useProfileHook.useProfile).mockReturnValue({
        user: mockPremiumUser,
        loading: false,
        error: null,
        refetch: vi.fn(),
      })
      vi.mocked(plaidApi.fetchAccounts).mockResolvedValue({
        accounts: mockAccounts,
        total_count: 2,
      })

      renderWithRouter(<AccountsPage />)

      await waitFor(() => {
        expect(screen.queryByText('Loading your accounts...')).not.toBeInTheDocument()
      })

      // Should show "2/30 accounts used" for premium tier
      expect(screen.getByText(/2\/30 accounts used/i)).toBeInTheDocument()
    })
  })
})
