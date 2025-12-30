/**
 * AutoCategorizationPageWrapper Component Tests
 *
 * Issue #393: Integrate AutoCategorizationPage into App Router and Navigation
 * Issue #399: Add tags autocomplete for target tags
 * Issue #564: Migrated to mock generated React Query hooks
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { AutoCategorizationPageWrapper } from './AutoCategorizationPageWrapper'
import * as useProfileHook from '../../profile/hooks/useProfile'
import * as useSubscriptionTiersHook from '../../subscription/hooks/useSubscriptionTiers'
import type { Account } from '../../accounts/types'
import type { UserTag } from '../../tags/types'
import type { CustomCategory } from '../../categories/types'
import { mockSubscriptionTiersResponse } from '@/test/fixtures/subscriptionFixtures'

// Mock the generated hooks
vi.mock('@/api/generated/endpoints/categories/categories', () => ({
  useGetUserCategories: vi.fn(),
  useGetPlaidCategories: vi.fn(),
}))
vi.mock('@/api/generated/endpoints/accounts/accounts', () => ({
  useGetAccounts: vi.fn(),
}))
vi.mock('@/api/generated/endpoints/tags/tags', () => ({
  useGetUserTags: vi.fn(),
}))

import { useGetUserCategories, useGetPlaidCategories } from '@/api/generated/endpoints/categories/categories'
import { useGetAccounts } from '@/api/generated/endpoints/accounts/accounts'
import { useGetUserTags } from '@/api/generated/endpoints/tags/tags'

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

// Helper to create wrapper with QueryClient
function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

describe('AutoCategorizationPageWrapper', () => {
  const mockCategories: CustomCategory[] = [
    { id: '1', user_id: 'user-1', name: 'Coffee', parent_category: 'Food & Drink', color: null, icon: null, usage_count: 5, created_at: '2025-01-01', updated_at: '2025-01-01' },
    { id: '2', user_id: 'user-1', name: 'Groceries', parent_category: null, color: null, icon: null, usage_count: 10, created_at: '2025-01-01', updated_at: '2025-01-01' },
  ]

  const mockAccounts: Account[] = [
    { id: 'acc-1', name: 'Checking', type: 'depository', mask: '1234', linked_item_id: 'item-1', user_id: 'user-1', plaid_account_id: 'plaid-1', currency_code: 'USD', is_hidden: false, created_at: '2025-01-01', updated_at: '2025-01-01' },
    { id: 'acc-2', name: 'Savings', type: 'depository', mask: '5678', linked_item_id: 'item-1', user_id: 'user-1', plaid_account_id: 'plaid-2', currency_code: 'USD', is_hidden: false, created_at: '2025-01-01', updated_at: '2025-01-01' },
  ]

  const mockTags: UserTag[] = [
    { id: 'tag-1', user_id: 'user-1', name: 'Bills', color: '#ff0000', usage_count: 5, created_at: '2025-01-01', updated_at: '2025-01-01' },
    { id: 'tag-2', user_id: 'user-1', name: 'Recurring', color: '#00ff00', usage_count: 3, created_at: '2025-01-01', updated_at: '2025-01-01' },
  ]

  const mockPlaidCategories = ['FOOD_AND_DRINK', 'GENERAL_MERCHANDISE', 'TRAVEL']

  const mockUser = {
    id: 'user-1',
    clerk_user_id: 'auth0|test123',
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

  // Helper to configure all hooks with defaults
  function configureHooks(overrides?: {
    categories?: CustomCategory[] | null
    categoriesError?: Error | null
    plaidCategories?: string[] | null
    accounts?: Account[] | null
    tags?: UserTag[] | null
    user?: typeof mockUser | null
    tiers?: typeof mockSubscriptionTiersResponse | null
  }) {
    vi.mocked(useGetUserCategories).mockReturnValue({
      data: overrides?.categories === undefined ? mockCategories : (overrides?.categories as unknown),
      isLoading: false,
      error: overrides?.categoriesError ?? null,
    } as ReturnType<typeof useGetUserCategories>)

    vi.mocked(useGetPlaidCategories).mockReturnValue({
      data: overrides?.plaidCategories === undefined ? mockPlaidCategories : (overrides?.plaidCategories as unknown),
      isLoading: false,
      error: null,
    } as ReturnType<typeof useGetPlaidCategories>)

    vi.mocked(useGetAccounts).mockReturnValue({
      data: { accounts: overrides?.accounts === undefined ? mockAccounts : (overrides?.accounts ?? []) },
      isLoading: false,
      error: null,
    } as ReturnType<typeof useGetAccounts>)

    vi.mocked(useGetUserTags).mockReturnValue({
      data: overrides?.tags === undefined ? mockTags : (overrides?.tags as unknown),
      isLoading: false,
      error: null,
    } as ReturnType<typeof useGetUserTags>)

    vi.mocked(useProfileHook.useProfile).mockReturnValue({
      user: overrides?.user === undefined ? mockUser : overrides?.user,
      loading: false,
      error: null,
      refetch: vi.fn(),
    })

    vi.mocked(useSubscriptionTiersHook.useSubscriptionTiers).mockReturnValue({
      tiers: overrides?.tiers === undefined ? mockSubscriptionTiersResponse : overrides?.tiers,
      loading: false,
      error: null,
      refetch: vi.fn(),
    })
  }

  beforeEach(() => {
    vi.clearAllMocks()
    configureHooks()
  })

  it('should render AutoCategorizationPage with fetched categories', async () => {
    const Wrapper = createWrapper()
    render(<AutoCategorizationPageWrapper />, { wrapper: Wrapper })

    await waitFor(() => {
      expect(screen.getByTestId('auto-categorization-page')).toBeInTheDocument()
    })

    await waitFor(() => {
      expect(screen.getByTestId('categories-count')).toHaveTextContent('2')
    })
  })

  it('should convert categories to string[][] format', async () => {
    const Wrapper = createWrapper()
    render(<AutoCategorizationPageWrapper />, { wrapper: Wrapper })

    // The mock AutoCategorizationPage receives the converted categories
    // Coffee has parent_category so it becomes ['Food & Drink', 'Coffee']
    // Groceries has no parent so it becomes ['Groceries']
    await waitFor(() => {
      expect(screen.getByTestId('categories-count')).toHaveTextContent('2')
    })
  })

  it('should pass maxRules from tier config based on user subscription', async () => {
    const Wrapper = createWrapper()
    render(<AutoCategorizationPageWrapper />, { wrapper: Wrapper })

    await waitFor(() => {
      expect(screen.getByTestId('auto-categorization-page')).toBeInTheDocument()
    })

    // Premium tier has 15 rules
    expect(screen.getByTestId('max-rules')).toHaveTextContent('15')
  })

  it('should pass currentTier from user profile', async () => {
    const Wrapper = createWrapper()
    render(<AutoCategorizationPageWrapper />, { wrapper: Wrapper })

    await waitFor(() => {
      expect(screen.getByTestId('auto-categorization-page')).toBeInTheDocument()
    })

    expect(screen.getByTestId('current-tier')).toHaveTextContent('premium')
  })

  it('should default to free tier when user is null', async () => {
    configureHooks({ user: null })

    const Wrapper = createWrapper()
    render(<AutoCategorizationPageWrapper />, { wrapper: Wrapper })

    await waitFor(() => {
      expect(screen.getByTestId('auto-categorization-page')).toBeInTheDocument()
    })

    expect(screen.getByTestId('current-tier')).toHaveTextContent('free')
    expect(screen.getByTestId('max-rules')).toHaveTextContent('1')
  })

  it('should handle data fetch error gracefully', async () => {
    // When useGetUserCategories returns null/undefined, component should handle gracefully
    configureHooks({ categories: null, accounts: null })

    const Wrapper = createWrapper()
    render(<AutoCategorizationPageWrapper />, { wrapper: Wrapper })

    await waitFor(() => {
      expect(screen.getByTestId('auto-categorization-page')).toBeInTheDocument()
    })

    // Should still render with empty categories and accounts
    expect(screen.getByTestId('categories-count')).toHaveTextContent('0')
    expect(screen.getByTestId('accounts-count')).toHaveTextContent('0')
  })

  it('should fetch both categories and accounts', async () => {
    const Wrapper = createWrapper()
    render(<AutoCategorizationPageWrapper />, { wrapper: Wrapper })

    await waitFor(() => {
      expect(screen.getByTestId('auto-categorization-page')).toBeInTheDocument()
    })

    expect(useGetUserCategories).toHaveBeenCalled()
    expect(useGetAccounts).toHaveBeenCalled()
    await waitFor(() => {
      expect(screen.getByTestId('accounts-count')).toHaveTextContent('2')
    })
  })

  it('should handle undefined tiers gracefully', async () => {
    configureHooks({ tiers: null })

    const Wrapper = createWrapper()
    render(<AutoCategorizationPageWrapper />, { wrapper: Wrapper })

    await waitFor(() => {
      expect(screen.getByTestId('auto-categorization-page')).toBeInTheDocument()
    })

    expect(screen.getByTestId('max-rules')).toHaveTextContent('undefined')
  })
})
