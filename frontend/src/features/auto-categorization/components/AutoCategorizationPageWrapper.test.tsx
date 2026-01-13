import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import type { Account } from '../../accounts/types'
import type { UserTag } from '../../tags/types'
import type { CustomCategory } from '../../categories/types'
import { mockSubscriptionTiersResponse } from '@/test/fixtures/subscriptionFixtures'
import { mockUseGetUserTags } from '@/test/mocks/tags-api'
import { mockUseGetAccounts } from '@/test/mocks/accounts-api'
import { mockUseGetUserProfile, mockRefetchUserProfile } from '@/test/mocks/profile-api'
import { mockUseGetSubscriptionTiers, mockRefetchSubscriptionTiers } from '@/test/mocks/subscription-api'

// Categories API mock (no centralized mock yet)
const mockUseGetUserCategories = mock(() => ({
  data: [],
  isLoading: false,
  error: null,
}))

const mockUseGetPlaidCategories = mock(() => ({
  data: [],
  isLoading: false,
  error: null,
}))

mock.module('@/api/generated/endpoints/categories/categories', () => ({
  useGetUserCategories: mockUseGetUserCategories,
  useGetPlaidCategories: mockUseGetPlaidCategories,
}))

mock.module('./AutoCategorizationPage', () => ({
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

import { AutoCategorizationPageWrapper } from './AutoCategorizationPageWrapper'

function createWrapper() {
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })}>
      {children}
    </QueryClientProvider>
  )
}

describe('AutoCategorizationPageWrapper', () => {
  const mockCategories: CustomCategory[] = [
    { id: '1', user_id: 'user-1', name: 'Coffee', parent_category: 'Food & Drink', color: null, icon: null, usage_count: 5, created_at: '2025-01-01', updated_at: '2025-01-01' },
    { id: '2', user_id: 'user-1', name: 'Groceries', parent_category: null, color: null, icon: null, usage_count: 10, created_at: '2025-01-01', updated_at: '2025-01-01' },
  ]

  const mockAccounts: Account[] = [
    {
      id: 'acc-1',
      name: 'Checking',
      type: 'depository',
      mask: '1234',
      linked_item_id: 'item-1',
      user_id: 'user-1',
      plaid_account_id: 'plaid-1',
      currency_code: 'USD',
      is_hidden: false,
      status: 'active',
      created_at: '2025-01-01',
      updated_at: '2025-01-01',
    },
    {
      id: 'acc-2',
      name: 'Savings',
      type: 'depository',
      mask: '5678',
      linked_item_id: 'item-1',
      user_id: 'user-1',
      plaid_account_id: 'plaid-2',
      currency_code: 'USD',
      is_hidden: false,
      status: 'active',
      created_at: '2025-01-01',
      updated_at: '2025-01-01',
    },
  ]

  const mockTags: UserTag[] = [
    { id: 'tag-1', user_id: 'user-1', name: 'Bills', color: '#ff0000', usage_count: 5, created_at: '2025-01-01', updated_at: '2025-01-01' },
    { id: 'tag-2', user_id: 'user-1', name: 'Recurring', color: '#00ff00', usage_count: 3, created_at: '2025-01-01', updated_at: '2025-01-01' },
  ]

  const mockPlaidCategories = ['FOOD_AND_DRINK', 'GENERAL_MERCHANDISE', 'TRAVEL']

  const mockUser = {
    id: 'user-1',
    clerk_user_id: 'user_test123',
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

  function configureHooks(overrides?: {
    categories?: CustomCategory[] | null
    categoriesError?: Error | null
    plaidCategories?: string[] | null
    accounts?: Account[] | null
    tags?: UserTag[] | null
    user?: typeof mockUser | null
    tiers?: typeof mockSubscriptionTiersResponse | null
  }) {
    mockUseGetUserCategories.mockReturnValue({
      data: overrides?.categories === undefined ? mockCategories : overrides?.categories,
      isLoading: false,
      error: overrides?.categoriesError ?? null,
    })

    mockUseGetPlaidCategories.mockReturnValue({
      data: overrides?.plaidCategories === undefined ? mockPlaidCategories : overrides?.plaidCategories,
      isLoading: false,
      error: null,
    })

    mockUseGetAccounts.mockReturnValue({
      data: { accounts: overrides?.accounts === undefined ? mockAccounts : (overrides?.accounts ?? []) },
      isLoading: false,
      error: null,
    })

    mockUseGetUserTags.mockReturnValue({
      data: overrides?.tags === undefined ? mockTags : overrides?.tags,
      isLoading: false,
      error: null,
    })

    // Use centralized API mocks - the wrapper hooks will use these
    mockUseGetUserProfile.mockReturnValue({
      data: overrides?.user === undefined ? mockUser : overrides?.user,
      isLoading: false,
      error: null,
      refetch: mockRefetchUserProfile,
    })

    mockUseGetSubscriptionTiers.mockReturnValue({
      data: overrides?.tiers === undefined ? mockSubscriptionTiersResponse : overrides?.tiers,
      isLoading: false,
      error: null,
      refetch: mockRefetchSubscriptionTiers,
    })
  }

  beforeEach(() => {
    mockUseGetUserCategories.mockClear()
    mockUseGetPlaidCategories.mockClear()
    mockUseGetAccounts.mockClear()
    mockUseGetUserTags.mockClear()
    mockUseGetUserProfile.mockClear()
    mockUseGetSubscriptionTiers.mockClear()
    configureHooks()
  })

  it('should render AutoCategorizationPage with fetched categories', async () => {
    render(<AutoCategorizationPageWrapper />, { wrapper: createWrapper() })
    await waitFor(() => {
      expect(screen.getByTestId('auto-categorization-page')).toBeInTheDocument()
      expect(screen.getByTestId('categories-count')).toHaveTextContent('2')
    })
  })

  it('should convert categories to string[][] format', async () => {
    render(<AutoCategorizationPageWrapper />, { wrapper: createWrapper() })
    await waitFor(() => {
      expect(screen.getByTestId('categories-count')).toHaveTextContent('2')
    })
  })

  it('should pass maxRules from tier config based on user subscription', async () => {
    render(<AutoCategorizationPageWrapper />, { wrapper: createWrapper() })
    await waitFor(() => {
      expect(screen.getByTestId('auto-categorization-page')).toBeInTheDocument()
    })
    expect(screen.getByTestId('max-rules')).toHaveTextContent('15')
  })

  it('should pass currentTier from user profile', async () => {
    render(<AutoCategorizationPageWrapper />, { wrapper: createWrapper() })
    await waitFor(() => {
      expect(screen.getByTestId('auto-categorization-page')).toBeInTheDocument()
    })
    expect(screen.getByTestId('current-tier')).toHaveTextContent('premium')
  })

  it('should default to free tier when user is null', async () => {
    configureHooks({ user: null })
    render(<AutoCategorizationPageWrapper />, { wrapper: createWrapper() })
    await waitFor(() => {
      expect(screen.getByTestId('auto-categorization-page')).toBeInTheDocument()
    })
    expect(screen.getByTestId('current-tier')).toHaveTextContent('free')
    expect(screen.getByTestId('max-rules')).toHaveTextContent('1')
  })

  it('should handle data fetch error gracefully', async () => {
    configureHooks({ categories: null, accounts: null })
    render(<AutoCategorizationPageWrapper />, { wrapper: createWrapper() })
    await waitFor(() => {
      expect(screen.getByTestId('auto-categorization-page')).toBeInTheDocument()
    })
    expect(screen.getByTestId('categories-count')).toHaveTextContent('0')
    expect(screen.getByTestId('accounts-count')).toHaveTextContent('0')
  })

  it('should fetch both categories and accounts', async () => {
    render(<AutoCategorizationPageWrapper />, { wrapper: createWrapper() })
    await waitFor(() => {
      expect(screen.getByTestId('auto-categorization-page')).toBeInTheDocument()
    })
    expect(mockUseGetUserCategories).toHaveBeenCalled()
    expect(mockUseGetAccounts).toHaveBeenCalled()
    await waitFor(() => {
      expect(screen.getByTestId('accounts-count')).toHaveTextContent('2')
    })
  })

  it('should handle undefined tiers gracefully', async () => {
    configureHooks({ tiers: null })
    render(<AutoCategorizationPageWrapper />, { wrapper: createWrapper() })
    await waitFor(() => {
      expect(screen.getByTestId('auto-categorization-page')).toBeInTheDocument()
    })
    expect(screen.getByTestId('max-rules')).toHaveTextContent('undefined')
  })
})
