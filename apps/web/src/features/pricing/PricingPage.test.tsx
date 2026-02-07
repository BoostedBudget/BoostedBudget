/**
 * PricingPage Tests
 *
 * Tests for the pricing page component that displays subscription tier comparison.
 * Migrated to bun:test with centralized mocks (Issue #575).
 * Updated for simplified tier structure: premium + household tiers (no free tier).
 */

import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { PricingPage } from './PricingPage'
import { mockSubscriptionTiersResponse } from '@/test/fixtures/subscriptionFixtures'

// Import centralized mocks
import { mockUseUser } from '@/test/mocks/clerk'
import {
  mockUseGetSubscriptionTiers,
  mockRefetchSubscriptionTiers,
} from '@/test/mocks/subscription-api'

function renderWithProviders(ui: React.ReactElement, routerState?: { frozen?: boolean }) {
  return render(
    <QueryClientProvider client={new QueryClient({
      defaultOptions: {
        queries: { retry: false, gcTime: 0 },
      },
    })}>
      <MemoryRouter initialEntries={[{ pathname: '/pricing', state: routerState }]}>
        {ui}
      </MemoryRouter>
    </QueryClientProvider>
  )
}

describe('PricingPage', () => {
  beforeEach(() => {
    // Reset all mocks
    mockUseUser.mockClear()
    mockUseGetSubscriptionTiers.mockClear()
    mockRefetchSubscriptionTiers.mockClear()

    // Default: successful load with no authenticated user
    mockUseGetSubscriptionTiers.mockReturnValue({
      data: mockSubscriptionTiersResponse,
      isLoading: false,
      error: null,
      refetch: mockRefetchSubscriptionTiers,
    })
    mockUseUser.mockReturnValue({
      user: undefined,
      isSignedIn: false,
      isLoaded: true,
    })
  })

  describe('Loading state', () => {
    it('should show loading state when tiers are loading', () => {
      mockUseGetSubscriptionTiers.mockReturnValue({
        data: undefined,
        isLoading: true,
        error: null,
        refetch: mockRefetchSubscriptionTiers,
      })

      renderWithProviders(<PricingPage />)

      expect(screen.getByText(/loading pricing plans/i)).toBeInTheDocument()
    })
  })

  describe('Error state', () => {
    it('should show error message when tiers fail to load', () => {
      const error = new Error('Network error')
      mockUseGetSubscriptionTiers.mockReturnValue({
        data: undefined,
        isLoading: false,
        error,
        refetch: mockRefetchSubscriptionTiers,
      })

      renderWithProviders(<PricingPage />)

      expect(screen.getByText(/failed to load pricing plans/i)).toBeInTheDocument()
    })

    it('should show retry button on error', () => {
      const error = new Error('Network error')
      mockUseGetSubscriptionTiers.mockReturnValue({
        data: undefined,
        isLoading: false,
        error,
        refetch: mockRefetchSubscriptionTiers,
      })

      renderWithProviders(<PricingPage />)

      expect(screen.getByRole('button', { name: /try again/i })).toBeInTheDocument()
    })

    it('should call refetch when retry button is clicked', async () => {
      const user = userEvent.setup()
      const localRefetch = mock(() => {})
      const error = new Error('Network error')
      mockUseGetSubscriptionTiers.mockReturnValue({
        data: undefined,
        isLoading: false,
        error,
        refetch: localRefetch,
      })

      renderWithProviders(<PricingPage />)

      const retryButton = screen.getByRole('button', { name: /try again/i })
      await user.click(retryButton)

      expect(localRefetch).toHaveBeenCalledTimes(1)
    })
  })

  describe('Tier cards', () => {
    it('should render all 5 tier cards with correct names', () => {
      renderWithProviders(<PricingPage />)

      // 5 tier names + 1 FAQ section header = 6 h2 headings
      const headings = screen.getAllByRole('heading', { level: 2 })
      const headingTexts = headings.map(h => h.textContent)

      expect(headings.length).toBe(6)
      expect(headingTexts).toContain('Premium')
      expect(headingTexts).toContain('Household (2 members)')
      expect(headingTexts).toContain('Household (3 members)')
      expect(headingTexts).toContain('Household (4 members)')
      expect(headingTexts).toContain('Household (5 members)')
    })

    it('should display correct prices', () => {
      renderWithProviders(<PricingPage />)

      expect(screen.getByText('$8.99')).toBeInTheDocument()
      expect(screen.getByText('$12.99')).toBeInTheDocument()
      expect(screen.getByText('$14.99')).toBeInTheDocument()
      expect(screen.getByText('$16.99')).toBeInTheDocument()
      expect(screen.getByText('$18.99')).toBeInTheDocument()
    })

    it('should show features for each tier', () => {
      renderWithProviders(<PricingPage />)

      // Premium tier features
      expect(screen.getByText('30 bank accounts')).toBeInTheDocument()

      // Household 5 features
      expect(screen.getByText('150 bank accounts')).toBeInTheDocument()

      // All tiers should have unlimited automation rules
      const unlimitedRulesElements = screen.getAllByText('Unlimited automation rules')
      expect(unlimitedRulesElements.length).toBe(5) // One per tier
    })

    it('should show tier descriptions', () => {
      renderWithProviders(<PricingPage />)

      // Descriptions from mockSubscriptionTiersResponse fixture
      expect(screen.getByText(/full-featured budgeting for individuals/i)).toBeInTheDocument()
      expect(screen.getByText(/best value for large families/i)).toBeInTheDocument()
    })
  })

  describe('Current plan highlighting', () => {
    beforeEach(() => {
      // Mock authenticated user with premium tier for all tests in this suite
      mockUseUser.mockReturnValue({
        user: {
          id: 'user_123',
          publicMetadata: {
            subscription_tier: 'premium',
          },
        },
        isSignedIn: true,
        isLoaded: true,
      })
    })

    it('should show "Current Plan" badge for user\'s tier', () => {
      renderWithProviders(<PricingPage />)

      const currentPlanElements = screen.getAllByText('Current Plan')
      expect(currentPlanElements.length).toBeGreaterThan(0)
    })

    it('should show "Subscribe" buttons for non-current tiers', () => {
      renderWithProviders(<PricingPage />)

      const selectButtons = screen.getAllByRole('button', { name: /subscribe/i })
      expect(selectButtons.length).toBeGreaterThan(0)
    })

    it('should disable button for current tier', () => {
      renderWithProviders(<PricingPage />)

      const currentPlanButtons = screen.getAllByRole('button', { name: /current plan/i })
      expect(currentPlanButtons.length).toBeGreaterThan(0)
      expect(currentPlanButtons[0]).toBeDisabled()
    })
  })

  describe('Unauthenticated user', () => {
    it('should render all tiers and default to premium when not authenticated', () => {
      renderWithProviders(<PricingPage />)

      const headings = screen.getAllByRole('heading', { level: 2 })
      expect(headings.length).toBe(6)

      const headingTexts = headings.map(h => h.textContent)
      expect(headingTexts).toContain('Premium')
      expect(headingTexts).toContain('Household (2 members)')
      expect(headingTexts).toContain('Household (5 members)')

      // Should show "Current Plan" for premium tier as default
      const currentPlanElements = screen.getAllByText('Current Plan')
      expect(currentPlanElements.length).toBeGreaterThan(0)
    })
  })

  describe('FAQ section', () => {
    it('should render FAQ section with title', () => {
      renderWithProviders(<PricingPage />)

      expect(screen.getByText('Frequently Asked Questions')).toBeInTheDocument()
    })

    it('should render FAQ questions', () => {
      renderWithProviders(<PricingPage />)

      expect(screen.getByText('Can I change plans anytime?')).toBeInTheDocument()
      expect(screen.getByText('What payment methods do you accept?')).toBeInTheDocument()
      expect(screen.getByText('Is there a free trial?')).toBeInTheDocument()
      expect(screen.getByText('Can I cancel anytime?')).toBeInTheDocument()
    })

    it('should render FAQ answers', () => {
      renderWithProviders(<PricingPage />)

      expect(screen.getByText(/you can upgrade or downgrade/i)).toBeInTheDocument()
      expect(screen.getByText(/major credit cards/i)).toBeInTheDocument()
      expect(screen.getByText(/14-day free trial/i)).toBeInTheDocument()
      expect(screen.getByText(/cancel your subscription at any time/i)).toBeInTheDocument()
    })
  })

  describe('Header', () => {
    it('should display page title', () => {
      renderWithProviders(<PricingPage />)

      expect(screen.getByRole('heading', { name: /choose your plan/i })).toBeInTheDocument()
    })

    it('should display subtitle', () => {
      renderWithProviders(<PricingPage />)

      expect(screen.getByText(/select the plan that best fits/i)).toBeInTheDocument()
    })
  })

  describe('Household 5 highlighting', () => {
    it('should show "Best Value" badge on Household 5 card', () => {
      renderWithProviders(<PricingPage />)

      expect(screen.getByText('Best Value')).toBeInTheDocument()
    })

    it('should have special styling class on Household 5 card', () => {
      const { container } = renderWithProviders(<PricingPage />)

      const featuredCard = container.querySelector('.pricing-card.featured')
      expect(featuredCard).toBeInTheDocument()
    })
  })

  describe('Feature display', () => {
    it('should show ad-free experience for all tiers', () => {
      renderWithProviders(<PricingPage />)

      // Ad-free should appear multiple times (once per card)
      const adFreeTexts = screen.getAllByText('Ad-free experience')
      expect(adFreeTexts.length).toBe(5) // All 5 tiers are ad-free
    })

    it('should show AI insights feature for household_3+ tiers', () => {
      renderWithProviders(<PricingPage />)

      expect(screen.getAllByText('AI-powered insights').length).toBeGreaterThan(0)
    })
  })

  describe('Billing period toggle', () => {
    it('should render Monthly and Yearly options', () => {
      renderWithProviders(<PricingPage />)

      expect(screen.getByRole('radio', { name: /monthly/i })).toBeInTheDocument()
      expect(screen.getByRole('radio', { name: /yearly/i })).toBeInTheDocument()
    })

    it('should default to Monthly view', () => {
      renderWithProviders(<PricingPage />)

      const monthlyButton = screen.getByRole('radio', { name: /monthly/i })
      expect(monthlyButton).toHaveAttribute('aria-checked', 'true')
    })

    it('should show monthly prices by default', () => {
      renderWithProviders(<PricingPage />)

      // Premium monthly price
      expect(screen.getByText('$8.99')).toBeInTheDocument()
      // Check for /month period label
      const monthLabels = screen.getAllByText('/month')
      expect(monthLabels.length).toBeGreaterThan(0)
    })

    it('should switch to yearly prices when Yearly clicked', async () => {
      const user = userEvent.setup()
      renderWithProviders(<PricingPage />)

      await user.click(screen.getByRole('radio', { name: /yearly/i }))

      // Premium yearly price
      expect(screen.getByText('$79.00')).toBeInTheDocument()
      // Check for /year period label
      const yearLabels = screen.getAllByText('/year')
      expect(yearLabels.length).toBeGreaterThan(0)
    })

    it('should show savings badge in yearly view for paid tiers', async () => {
      const user = userEvent.setup()
      renderWithProviders(<PricingPage />)

      await user.click(screen.getByRole('radio', { name: /yearly/i }))

      // All paid tiers save ~27%
      const savingsBadges = screen.getAllByText(/save 27%/i)
      expect(savingsBadges.length).toBeGreaterThan(0)
    })

    it('should show equivalent monthly price in yearly view', async () => {
      const user = userEvent.setup()
      renderWithProviders(<PricingPage />)

      await user.click(screen.getByRole('radio', { name: /yearly/i }))

      // $79.00/12 = $6.58/mo
      expect(screen.getByText(/\$6\.58\/mo, billed annually/i)).toBeInTheDocument()
    })

    it('should toggle back to monthly view when Monthly clicked', async () => {
      const user = userEvent.setup()
      renderWithProviders(<PricingPage />)

      // Switch to yearly
      await user.click(screen.getByRole('radio', { name: /yearly/i }))
      expect(screen.getByText('$79.00')).toBeInTheDocument()

      // Switch back to monthly
      await user.click(screen.getByRole('radio', { name: /monthly/i }))
      expect(screen.getByText('$8.99')).toBeInTheDocument()
    })

    it('should be keyboard accessible', async () => {
      renderWithProviders(<PricingPage />)

      const monthlyButton = screen.getByRole('radio', { name: /monthly/i })
      const yearlyButton = screen.getByRole('radio', { name: /yearly/i })

      // Both buttons should be focusable
      expect(monthlyButton).not.toHaveAttribute('tabindex', '-1')
      expect(yearlyButton).not.toHaveAttribute('tabindex', '-1')
    })
  })

  // Issue #679, #680: Frozen account banner tests
  describe('Frozen account banner', () => {
    it('should show frozen account banner when redirected with frozen state', () => {
      renderWithProviders(<PricingPage />, { frozen: true })

      expect(screen.getByText(/your subscription has expired/i)).toBeInTheDocument()
      expect(screen.getByText(/your data is safe/i)).toBeInTheDocument()
    })

    it('should not show frozen account banner without frozen state', () => {
      renderWithProviders(<PricingPage />)

      expect(screen.queryByText(/your subscription has expired/i)).not.toBeInTheDocument()
    })
  })
})
