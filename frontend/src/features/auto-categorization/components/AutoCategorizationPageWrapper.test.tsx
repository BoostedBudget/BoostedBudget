/**
 * AutoCategorizationPageWrapper Component Tests
 *
 * Issue #393: Integrate AutoCategorizationPage into App Router and Navigation
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { AutoCategorizationPageWrapper } from './AutoCategorizationPageWrapper'
import * as categoriesApi from '../../categories/api/categoriesApi'
import * as useProfileHook from '../../profile/hooks/useProfile'
import * as useSubscriptionTiersHook from '../../subscription/hooks/useSubscriptionTiers'

// Mock dependencies
vi.mock('../../categories/api/categoriesApi')
vi.mock('../../profile/hooks/useProfile')
vi.mock('../../subscription/hooks/useSubscriptionTiers')
vi.mock('./AutoCategorizationPage', () => ({
  AutoCategorizationPage: ({ categories, maxRules, currentTier }: { categories: string[][], maxRules?: number, currentTier?: string }) => (
    <div data-testid="auto-categorization-page">
      <span data-testid="categories-count">{categories.length}</span>
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

  const mockUser = {
    id: 'user-1',
    email: 'test@example.com',
    name: 'Test User',
    subscription_tier: 'premium' as const,
    created_at: '2025-01-01',
    updated_at: '2025-01-01',
  }

  const mockTiers = {
    tiers: {
      free: { limits: { rules: 1, accounts: 3, tags: 50 } },
      premium_lite: { limits: { rules: 5, accounts: 15, tags: 100 } },
      premium: { limits: { rules: 15, accounts: 30, tags: 200 } },
      premium_plus: { limits: { rules: -1, accounts: -1, tags: -1 } },
    },
  }

  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(categoriesApi.fetchCategories).mockResolvedValue(mockCategories)
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

    expect(screen.getByTestId('categories-count')).toHaveTextContent('2')
  })

  it('should convert categories to string[][] format', async () => {
    render(<AutoCategorizationPageWrapper />)

    await waitFor(() => {
      expect(categoriesApi.fetchCategories).toHaveBeenCalledTimes(1)
    })

    // The mock AutoCategorizationPage receives the converted categories
    // Coffee has parent_category so it becomes ['Food & Drink', 'Coffee']
    // Groceries has no parent so it becomes ['Groceries']
    expect(screen.getByTestId('categories-count')).toHaveTextContent('2')
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

  it('should handle category fetch error gracefully', async () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.mocked(categoriesApi.fetchCategories).mockRejectedValue(new Error('Network error'))

    render(<AutoCategorizationPageWrapper />)

    await waitFor(() => {
      expect(screen.getByTestId('auto-categorization-page')).toBeInTheDocument()
    })

    // Should still render with empty categories
    expect(screen.getByTestId('categories-count')).toHaveTextContent('0')
    expect(consoleError).toHaveBeenCalledWith('Failed to load categories:', expect.any(Error))

    consoleError.mockRestore()
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
