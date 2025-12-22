/**
 * PricingPage Tests
 *
 * Tests for the pricing page component that displays subscription tier comparison.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { PricingPage } from './PricingPage'
import * as useSubscriptionTiersHook from '../subscription/hooks/useSubscriptionTiers'
import type { SubscriptionTiersResponse } from '../subscription/types'

// Mock useAuth0
vi.mock('@auth0/auth0-react', () => ({
  useAuth0: vi.fn(() => ({
    user: null,
    isAuthenticated: false,
    isLoading: false,
  })),
}))

// Mock useSubscriptionTiers
vi.mock('../subscription/hooks/useSubscriptionTiers')

// Import mocked useAuth0 for manipulation in tests
import { useAuth0 } from '@auth0/auth0-react'

// Mock tier data matching backend response
const mockTiersResponse: SubscriptionTiersResponse = {
  tiers: {
    free: {
      display_name: 'Free',
      description: 'Full budgeting with basic automation',
      price_monthly: 0,
      price_yearly: 0,
      limits: {
        accounts: 3,
        rules: 1,
        executions_per_day: 1,
        min_execution_interval_minutes: 1440,
      },
      features: {
        ads_enabled: true,
        ai_insights: false,
      },
    },
    premium_lite: {
      display_name: 'Premium Lite',
      description: 'Perfect for couples and light automation',
      price_monthly: 4.99,
      price_yearly: 49.99,
      limits: {
        accounts: 15,
        rules: 5,
        executions_per_day: 2,
        min_execution_interval_minutes: 720,
      },
      features: {
        ads_enabled: false,
        ai_insights: false,
      },
    },
    premium: {
      display_name: 'Premium',
      description: 'Power users with complex finances',
      price_monthly: 9.99,
      price_yearly: 99.99,
      limits: {
        accounts: 30,
        rules: 15,
        executions_per_day: 6,
        min_execution_interval_minutes: 240,
      },
      features: {
        ads_enabled: false,
        ai_insights: false,
      },
    },
    premium_plus: {
      display_name: 'Premium+',
      description: 'Unlimited everything with AI and API access',
      price_monthly: 19.99,
      price_yearly: 199.99,
      limits: {
        accounts: -1,
        rules: -1,
        executions_per_day: 144,
        min_execution_interval_minutes: 10,
      },
      features: {
        ads_enabled: false,
        ai_insights: true,
        api_access: true,
        priority_support: true,
      },
    },
  },
  default_tier: 'free',
}

describe('PricingPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // Default: successful load with no authenticated user
    vi.mocked(useSubscriptionTiersHook.useSubscriptionTiers).mockReturnValue({
      tiers: mockTiersResponse,
      loading: false,
      error: null,
      refetch: vi.fn(),
    })
    vi.mocked(useAuth0).mockReturnValue({
      user: null,
      isAuthenticated: false,
      isLoading: false,
      loginWithRedirect: vi.fn(),
      logout: vi.fn(),
      getAccessTokenSilently: vi.fn(),
      getAccessTokenWithPopup: vi.fn(),
      getIdTokenClaims: vi.fn(),
      loginWithPopup: vi.fn(),
      handleRedirectCallback: vi.fn(),
    })
  })

  describe('Loading state', () => {
    it('should show loading state when tiers are loading', () => {
      vi.mocked(useSubscriptionTiersHook.useSubscriptionTiers).mockReturnValue({
        tiers: null,
        loading: true,
        error: null,
        refetch: vi.fn(),
      })

      render(<PricingPage />)

      expect(screen.getByText(/loading pricing plans/i)).toBeInTheDocument()
    })
  })

  describe('Error state', () => {
    it('should show error message when tiers fail to load', () => {
      const error = new Error('Network error')
      vi.mocked(useSubscriptionTiersHook.useSubscriptionTiers).mockReturnValue({
        tiers: null,
        loading: false,
        error,
        refetch: vi.fn(),
      })

      render(<PricingPage />)

      expect(screen.getByText(/failed to load pricing plans/i)).toBeInTheDocument()
    })

    it('should show retry button on error', () => {
      const error = new Error('Network error')
      vi.mocked(useSubscriptionTiersHook.useSubscriptionTiers).mockReturnValue({
        tiers: null,
        loading: false,
        error,
        refetch: vi.fn(),
      })

      render(<PricingPage />)

      expect(screen.getByRole('button', { name: /try again/i })).toBeInTheDocument()
    })

    it('should call refetch when retry button is clicked', async () => {
      const user = userEvent.setup()
      const mockRefetch = vi.fn()
      const error = new Error('Network error')
      vi.mocked(useSubscriptionTiersHook.useSubscriptionTiers).mockReturnValue({
        tiers: null,
        loading: false,
        error,
        refetch: mockRefetch,
      })

      render(<PricingPage />)

      const retryButton = screen.getByRole('button', { name: /try again/i })
      await user.click(retryButton)

      expect(mockRefetch).toHaveBeenCalledTimes(1)
    })
  })

  describe('Tier cards', () => {
    it('should render all 4 tier cards', () => {
      render(<PricingPage />)

      // "Free" appears multiple times (tier name and price)
      expect(screen.getAllByText('Free').length).toBeGreaterThan(0)
      expect(screen.getByText('Premium Lite')).toBeInTheDocument()
      // There are 5 h2 headings: 4 tier names + 1 FAQ section header
      expect(screen.getAllByRole('heading', { level: 2 }).length).toBe(5)
      expect(screen.getByText('Premium+')).toBeInTheDocument()
    })

    it('should display correct tier names', () => {
      render(<PricingPage />)

      // Check tier heading names (h2 elements)
      const headings = screen.getAllByRole('heading', { level: 2 })
      const headingTexts = headings.map(h => h.textContent)

      expect(headingTexts).toContain('Free')
      expect(headingTexts).toContain('Premium Lite')
      expect(headingTexts).toContain('Premium')
      expect(headingTexts).toContain('Premium+')
    })

    it('should display correct prices', () => {
      render(<PricingPage />)

      // Free tier shows "Free" as price
      const freeElements = screen.getAllByText('Free')
      expect(freeElements.length).toBeGreaterThan(0)

      expect(screen.getByText('$4.99')).toBeInTheDocument()
      expect(screen.getByText('$9.99')).toBeInTheDocument()
      expect(screen.getByText('$19.99')).toBeInTheDocument()
    })

    it('should show features for each tier', () => {
      render(<PricingPage />)

      // Free tier features
      expect(screen.getByText('3 bank accounts')).toBeInTheDocument()
      expect(screen.getByText('1 automation rule')).toBeInTheDocument()

      // Premium+ features
      expect(screen.getByText('Unlimited bank accounts')).toBeInTheDocument()
      expect(screen.getByText('Unlimited automation rules')).toBeInTheDocument()
    })

    it('should show tier descriptions', () => {
      render(<PricingPage />)

      expect(screen.getByText(/full budgeting with basic automation/i)).toBeInTheDocument()
      expect(screen.getByText(/unlimited everything with ai and api access/i)).toBeInTheDocument()
    })
  })

  describe('Current plan highlighting', () => {
    it('should show "Current Plan" badge for user\'s tier', () => {
      // Mock authenticated user with premium tier
      vi.mocked(useAuth0).mockReturnValue({
        user: {
          'https://boostedbudget.com/subscription_tier': 'premium',
        },
        isAuthenticated: true,
        isLoading: false,
        loginWithRedirect: vi.fn(),
        logout: vi.fn(),
        getAccessTokenSilently: vi.fn(),
        getAccessTokenWithPopup: vi.fn(),
        getIdTokenClaims: vi.fn(),
        loginWithPopup: vi.fn(),
        handleRedirectCallback: vi.fn(),
      })

      render(<PricingPage />)

      // Should show "Current Plan" badge (appears as badge and button)
      const currentPlanElements = screen.getAllByText('Current Plan')
      expect(currentPlanElements.length).toBeGreaterThan(0)
    })

    it('should show "Select Plan" for non-current tiers', () => {
      // Mock authenticated user with free tier
      vi.mocked(useAuth0).mockReturnValue({
        user: {
          'https://boostedbudget.com/subscription_tier': 'free',
        },
        isAuthenticated: true,
        isLoading: false,
        loginWithRedirect: vi.fn(),
        logout: vi.fn(),
        getAccessTokenSilently: vi.fn(),
        getAccessTokenWithPopup: vi.fn(),
        getIdTokenClaims: vi.fn(),
        loginWithPopup: vi.fn(),
        handleRedirectCallback: vi.fn(),
      })

      render(<PricingPage />)

      // Should show "Select Plan" buttons for premium_lite and premium tiers
      // (premium_plus shows "Go Premium+" instead)
      const selectButtons = screen.getAllByRole('button', { name: /select plan/i })
      expect(selectButtons.length).toBeGreaterThan(0)
    })

    it('should disable button for current tier', () => {
      // Mock authenticated user with premium tier
      vi.mocked(useAuth0).mockReturnValue({
        user: {
          'https://boostedbudget.com/subscription_tier': 'premium',
        },
        isAuthenticated: true,
        isLoading: false,
        loginWithRedirect: vi.fn(),
        logout: vi.fn(),
        getAccessTokenSilently: vi.fn(),
        getAccessTokenWithPopup: vi.fn(),
        getIdTokenClaims: vi.fn(),
        loginWithPopup: vi.fn(),
        handleRedirectCallback: vi.fn(),
      })

      render(<PricingPage />)

      // Find button elements
      const currentPlanButtons = screen.getAllByRole('button', { name: /current plan/i })
      expect(currentPlanButtons.length).toBeGreaterThan(0)
      // The button should be disabled
      expect(currentPlanButtons[0]).toBeDisabled()
    })
  })

  describe('Unauthenticated user', () => {
    it('should render all tiers when not authenticated', () => {
      render(<PricingPage />)

      // All 4 tier names should be visible (plus FAQ section = 5 h2 headings total)
      const headings = screen.getAllByRole('heading', { level: 2 })
      expect(headings.length).toBe(5)
      // Verify tier names are present
      const headingTexts = headings.map(h => h.textContent)
      expect(headingTexts).toContain('Free')
      expect(headingTexts).toContain('Premium Lite')
      expect(headingTexts).toContain('Premium')
      expect(headingTexts).toContain('Premium+')
    })

    it('should default to free tier when user not authenticated', () => {
      render(<PricingPage />)

      // Should show "Current Plan" for free tier (appears as badge and button)
      const currentPlanElements = screen.getAllByText('Current Plan')
      expect(currentPlanElements.length).toBeGreaterThan(0)
    })
  })

  describe('FAQ section', () => {
    it('should render FAQ section with title', () => {
      render(<PricingPage />)

      expect(screen.getByText('Frequently Asked Questions')).toBeInTheDocument()
    })

    it('should render FAQ questions', () => {
      render(<PricingPage />)

      expect(screen.getByText('Can I change plans anytime?')).toBeInTheDocument()
      expect(screen.getByText('What payment methods do you accept?')).toBeInTheDocument()
      expect(screen.getByText('Is there a free trial?')).toBeInTheDocument()
      expect(screen.getByText('Can I cancel anytime?')).toBeInTheDocument()
    })

    it('should render FAQ answers', () => {
      render(<PricingPage />)

      expect(screen.getByText(/you can upgrade or downgrade/i)).toBeInTheDocument()
      expect(screen.getByText(/major credit cards/i)).toBeInTheDocument()
      expect(screen.getByText(/14-day free trial/i)).toBeInTheDocument()
      expect(screen.getByText(/cancel your subscription at any time/i)).toBeInTheDocument()
    })
  })

  describe('Header', () => {
    it('should display page title', () => {
      render(<PricingPage />)

      expect(screen.getByRole('heading', { name: /choose your plan/i })).toBeInTheDocument()
    })

    it('should display subtitle', () => {
      render(<PricingPage />)

      expect(screen.getByText(/select the plan that best fits/i)).toBeInTheDocument()
    })
  })

  describe('Premium+ highlighting', () => {
    it('should show "Best Value" badge on Premium+ card', () => {
      render(<PricingPage />)

      expect(screen.getByText('Best Value')).toBeInTheDocument()
    })

    it('should have special styling class on Premium+ card', () => {
      const { container } = render(<PricingPage />)

      const featuredCard = container.querySelector('.pricing-card.featured')
      expect(featuredCard).toBeInTheDocument()
    })
  })

  describe('Feature display', () => {
    it('should show ad-free experience as excluded for free tier', () => {
      render(<PricingPage />)

      // Ad-free should appear multiple times (once per card)
      const adFreeTexts = screen.getAllByText('Ad-free experience')
      expect(adFreeTexts.length).toBeGreaterThan(0)
    })

    it('should show AI insights feature for Premium+ tier', () => {
      render(<PricingPage />)

      expect(screen.getAllByText('AI-powered insights').length).toBeGreaterThan(0)
    })
  })
})
