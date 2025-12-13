/**
 * AutoCategorizationPageWrapper Component Tests
 *
 * Issue #393: Integrate AutoCategorizationPage into App Router and Navigation
 * Issue #399: Add tags autocomplete for target tags
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { AutoCategorizationPageWrapper } from './AutoCategorizationPageWrapper'
import * as categoriesApi from '../../categories/api/categoriesApi'
import * as plaidApi from '../../accounts/api/plaidApi'
import * as tagsApi from '../../tags/api/tagsApi'
import * as useProfileHook from '../../profile/hooks/useProfile'
import * as useSubscriptionTiersHook from '../../subscription/hooks/useSubscriptionTiers'
import type { Account } from '../../accounts/types'
import type { UserTag } from '../../tags/types'

// Mock dependencies
vi.mock('../../categories/api/categoriesApi')
vi.mock('../../accounts/api/plaidApi')
vi.mock('../../tags/api/tagsApi')
vi.mock('../../profile/hooks/useProfile')
vi.mock('../../subscription/hooks/useSubscriptionTiers')
vi.mock('./AutoCategorizationPage', () => ({
  AutoCategorizationPage: ({ categories, accounts, tags, maxRules, currentTier }: { categories: string[][], accounts?: Account[], tags?: UserTag[], maxRules?: number, currentTier?: string }) => (
    <div data-testid="auto-categorization-page">
      <span data-testid="categories-count">{categories.length}</span>
      <span data-testid="accounts-count">{accounts?.length ?? 0}</span>
      <span data-testid="tags-count">{tags?.length ?? 0}</span>
      <span data-testid="max-rules">{maxRules ?? 'undefined'}</span>
      <span data-testid="current-tier">{currentTier ?? 'undefined'}</span>
    </div>
  ),
}))

describe('AutoCategorizationPageWrapper', () => {
  const mockCategories = [
    { id: '1', user_id: 'user-1', name: 'Coffee', parent_category: 'Food & Drink', color: null, icon: null, usage_count: 5, created_at: '2025-01-01', updated_at: '2025-01-01' },
    { id: '2', user_id: 'user-1', name: 'Groceries', parent_category: null, color: null, icon: null, usage_count: 10, created_at: '2025-01-01', updated_at: '2025-01-01' },
  ]

  const mockAccounts: Account[] = [
    { id: 'acc-1', name: 'Checking', type: 'depository', mask: '1234', linked_item_id: 'item-1', user_id: 'user-1', plaid_account_id: 'plaid-1', currency_code: 'USD', created_at: '2025-01-01', updated_at: '2025-01-01' },
    { id: 'acc-2', name: 'Savings', type: 'depository', mask: '5678', linked_item_id: 'item-1', user_id: 'user-1', plaid_account_id: 'plaid-2', currency_code: 'USD', created_at: '2025-01-01', updated_at: '2025-01-01' },
  ]

  const mockTags: UserTag[] = [
    { id: 'tag-1', user_id: 'user-1', name: 'Bills', color: '#ff0000', usage_count: 5, created_at: '2025-01-01', updated_at: '2025-01-01' },
    { id: 'tag-2', user_id: 'user-1', name: 'Recurring', color: '#00ff00', usage_count: 3, created_at: '2025-01-01', updated_at: '2025-01-01' },
  ]

  const mockPlaidCategories = ['FOOD_AND_DRINK', 'GENERAL_MERCHANDISE', 'TRAVEL']

  const mockUser = {
    id: 'user-1',
    auth0_user_id: 'auth0|test123',
    email: 'test@example.com',
    email_verified: true,
    subscription_tier: 'premium' as const,
    subscription_status: 'active',
    primary_region: 'iad',
    is_active: true,
    is_deleted: false,
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
    last_login_at: '2025-01-01T00:00:00Z',
  }

  const mockTiers = {
    tiers: {
      free: {
        display_name: 'Free',
        description: 'Free tier',
        price_monthly: 0,
        price_yearly: 0,
        limits: { accounts: 3, rules: 1, executions_per_day: 1, min_execution_interval_minutes: 1440 },
        features: { ads_enabled: true, ai_insights: false },
      },
      premium_lite: {
        display_name: 'Premium Lite',
        description: 'Premium Lite tier',
        price_monthly: 4.99,
        price_yearly: 49.99,
        limits: { accounts: 15, rules: 5, executions_per_day: 2, min_execution_interval_minutes: 720 },
        features: { ads_enabled: false, ai_insights: false },
      },
      premium: {
        display_name: 'Premium',
        description: 'Premium tier',
        price_monthly: 9.99,
        price_yearly: 99.99,
        limits: { accounts: 30, rules: 15, executions_per_day: 6, min_execution_interval_minutes: 240 },
        features: { ads_enabled: false, ai_insights: false },
      },
      premium_plus: {
        display_name: 'Premium+',
        description: 'Premium+ tier',
        price_monthly: 19.99,
        price_yearly: 199.99,
        limits: { accounts: -1, rules: -1, executions_per_day: 144, min_execution_interval_minutes: 10 },
        features: { ads_enabled: false, ai_insights: true, api_access: true },
      },
    },
    default_tier: 'free' as const,
  }

  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(categoriesApi.fetchCategories).mockResolvedValue(mockCategories)
    vi.mocked(categoriesApi.fetchPlaidCategories).mockResolvedValue(mockPlaidCategories)
    vi.mocked(plaidApi.fetchAccounts).mockResolvedValue({ accounts: mockAccounts, total_count: mockAccounts.length })
    vi.mocked(tagsApi.fetchTags).mockResolvedValue(mockTags)
    vi.mocked(useProfileHook.useProfile).mockReturnValue({
      user: mockUser,
      loading: false,
      error: null,
      refetch: vi.fn(),
    })
    vi.mocked(useSubscriptionTiersHook.useSubscriptionTiers).mockReturnValue({
      tiers: mockTiers,
      loading: false,
      error: null,
      refetch: vi.fn(),
    })
  })

  it('should render AutoCategorizationPage with fetched categories', async () => {
    render(<AutoCategorizationPageWrapper />)

    await waitFor(() => {
      expect(screen.getByTestId('auto-categorization-page')).toBeInTheDocument()
    })

    await waitFor(() => {
      expect(screen.getByTestId('categories-count')).toHaveTextContent('2')
    })
  })

  it('should convert categories to string[][] format', async () => {
    render(<AutoCategorizationPageWrapper />)

    await waitFor(() => {
      expect(categoriesApi.fetchCategories).toHaveBeenCalledTimes(1)
    })

    // The mock AutoCategorizationPage receives the converted categories
    // Coffee has parent_category so it becomes ['Food & Drink', 'Coffee']
    // Groceries has no parent so it becomes ['Groceries']
    // Wait for state update to be flushed to the UI
    await waitFor(() => {
      expect(screen.getByTestId('categories-count')).toHaveTextContent('2')
    })
  })

  it('should pass maxRules from tier config based on user subscription', async () => {
    render(<AutoCategorizationPageWrapper />)

    await waitFor(() => {
      expect(screen.getByTestId('auto-categorization-page')).toBeInTheDocument()
    })

    // Premium tier has 15 rules
    expect(screen.getByTestId('max-rules')).toHaveTextContent('15')
  })

  it('should pass currentTier from user profile', async () => {
    render(<AutoCategorizationPageWrapper />)

    await waitFor(() => {
      expect(screen.getByTestId('auto-categorization-page')).toBeInTheDocument()
    })

    expect(screen.getByTestId('current-tier')).toHaveTextContent('premium')
  })

  it('should default to free tier when user is null', async () => {
    vi.mocked(useProfileHook.useProfile).mockReturnValue({
      user: null,
      loading: false,
      error: null,
      refetch: vi.fn(),
    })

    render(<AutoCategorizationPageWrapper />)

    await waitFor(() => {
      expect(screen.getByTestId('auto-categorization-page')).toBeInTheDocument()
    })

    expect(screen.getByTestId('current-tier')).toHaveTextContent('free')
    expect(screen.getByTestId('max-rules')).toHaveTextContent('1')
  })

  it('should handle data fetch error gracefully', async () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.mocked(categoriesApi.fetchCategories).mockRejectedValue(new Error('Network error'))

    render(<AutoCategorizationPageWrapper />)

    await waitFor(() => {
      expect(screen.getByTestId('auto-categorization-page')).toBeInTheDocument()
    })

    // Should still render with empty categories and accounts
    expect(screen.getByTestId('categories-count')).toHaveTextContent('0')
    expect(screen.getByTestId('accounts-count')).toHaveTextContent('0')
    expect(consoleError).toHaveBeenCalledWith('Failed to load data:', expect.any(Error))

    consoleError.mockRestore()
  })

  it('should fetch both categories and accounts', async () => {
    render(<AutoCategorizationPageWrapper />)

    await waitFor(() => {
      expect(screen.getByTestId('auto-categorization-page')).toBeInTheDocument()
    })

    expect(categoriesApi.fetchCategories).toHaveBeenCalledTimes(1)
    expect(plaidApi.fetchAccounts).toHaveBeenCalledTimes(1)
    // Wait for async state update to be flushed to the UI
    await waitFor(() => {
      expect(screen.getByTestId('accounts-count')).toHaveTextContent('2')
    })
  })

  it('should handle undefined tiers gracefully', async () => {
    vi.mocked(useSubscriptionTiersHook.useSubscriptionTiers).mockReturnValue({
      tiers: null,
      loading: false,
      error: null,
      refetch: vi.fn(),
    })

    render(<AutoCategorizationPageWrapper />)

    await waitFor(() => {
      expect(screen.getByTestId('auto-categorization-page')).toBeInTheDocument()
    })

    expect(screen.getByTestId('max-rules')).toHaveTextContent('undefined')
  })
})
