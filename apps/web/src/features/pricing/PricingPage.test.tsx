/**
 * PricingPage Tests
 *
 * Tests for the pricing page component that displays subscription tier comparison.
 * Migrated to bun:test with centralized mocks (Issue #575).
 * Updated for simplified tier structure: premium + household tiers (no free tier).
 * Updated for context-aware pricing CTAs (Issue #1038).
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
  mockUseGetCurrentSubscription,
  mockUseGetBillingPortalUrl,
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

const defaultTiersReturn = {
  data: mockSubscriptionTiersResponse,
  isLoading: false,
  error: null,
  refetch: mockRefetchSubscriptionTiers,
}

const activeSubscription = {
  subscription_id: 'sub_123',
  status: 'active',
  tier: 'premium',
  billing_interval: 'month',
  cancel_at_period_end: false,
  amount: '8.99',
  currency: 'USD',
  current_period_end: '2026-05-08T00:00:00Z',
}

describe('PricingPage', () => {
  beforeEach(() => {
    // Reset all mocks
    mockUseUser.mockClear()
    mockUseGetSubscriptionTiers.mockClear()
    mockRefetchSubscriptionTiers.mockClear()
    mockUseGetCurrentSubscription.mockClear()
    mockUseGetBillingPortalUrl.mockClear()

    // Default: successful load with no authenticated user
    mockUseGetSubscriptionTiers.mockReturnValue(defaultTiersReturn)
    mockUseUser.mockReturnValue({
      user: undefined,
      isSignedIn: false,
      isLoaded: true,
    })
    mockUseGetCurrentSubscription.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
      refetch: mock(() => Promise.resolve()),
    })
    mockUseGetBillingPortalUrl.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
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

      // All tiers show unlimited bank accounts
      const unlimitedAccountsElements = screen.getAllByText('Unlimited bank accounts')
      expect(unlimitedAccountsElements.length).toBe(5) // One per tier

      // All tiers should have unlimited automation rules
      const unlimitedRulesElements = screen.getAllByText('Unlimited automation rules')
      expect(unlimitedRulesElements.length).toBe(5) // One per tier

      // Users included differentiator
      expect(screen.getByText('For 1 person')).toBeInTheDocument()
      expect(screen.getAllByText(/For \d+ household members/).length).toBeGreaterThan(0)
    })

    it('should show tier descriptions', () => {
      renderWithProviders(<PricingPage />)

      // Descriptions from mockSubscriptionTiersResponse fixture
      expect(screen.getByText(/full-featured budgeting for individuals/i)).toBeInTheDocument()
      expect(screen.getByText(/best value for large families/i)).toBeInTheDocument()
    })
  })

  describe('Unauthenticated user', () => {
    it('should render all tiers without Current Plan badge when signed out', () => {
      renderWithProviders(<PricingPage />)

      const headings = screen.getAllByRole('heading', { level: 2 })
      expect(headings.length).toBe(6)

      const headingTexts = headings.map(h => h.textContent)
      expect(headingTexts).toContain('Premium')
      expect(headingTexts).toContain('Household (2 members)')
      expect(headingTexts).toContain('Household (5 members)')

      // Signed-out users should NOT see "Current Plan" — they should see Subscribe
      expect(screen.queryByText('Current Plan')).not.toBeInTheDocument()
    })

    it('should show Subscribe buttons for all tiers when signed out', () => {
      renderWithProviders(<PricingPage />)

      const subscribeButtons = screen.getAllByRole('button', { name: /subscribe/i })
      expect(subscribeButtons.length).toBe(5)
    })
  })

  describe('Current plan highlighting (active subscriber)', () => {
    beforeEach(() => {
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
      mockUseGetCurrentSubscription.mockReturnValue({
        data: activeSubscription,
        isLoading: false,
        error: null,
      })
    })

    it('should show "Current Plan" badge for user\'s tier', () => {
      renderWithProviders(<PricingPage />)

      const currentPlanElements = screen.getAllByText('Current Plan')
      expect(currentPlanElements.length).toBeGreaterThan(0)
    })

    it('should show directional buttons for non-current tiers', () => {
      renderWithProviders(<PricingPage />)

      // Active premium user — all 4 household tiers are upgrades
      const upgradeButtons = screen.getAllByRole('button', { name: /upgrade/i })
      expect(upgradeButtons.length).toBe(4)
    })

    it('should disable button for current tier', () => {
      renderWithProviders(<PricingPage />)

      const currentPlanButtons = screen.getAllByRole('button', { name: /current plan/i })
      expect(currentPlanButtons.length).toBeGreaterThan(0)
      expect(currentPlanButtons[0]).toBeDisabled()
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
      expect(screen.getByText(/60-day free trial/i)).toBeInTheDocument()
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

    it('should not show execution frequency (all tiers have unlimited executions)', () => {
      renderWithProviders(<PricingPage />)

      // Execution frequency labels should not appear (unlimited for all tiers)
      expect(screen.queryByText(/rules run every/i)).not.toBeInTheDocument()
    })
  })

  describe('Billing period toggle', () => {
    it('should render Monthly and Yearly options', () => {
      renderWithProviders(<PricingPage />)

      expect(screen.getByRole('radio', { name: /monthly/i })).toBeInTheDocument()
      expect(screen.getByRole('radio', { name: /yearly/i })).toBeInTheDocument()
    })

    it('should show savings percentage in yearly toggle label', () => {
      renderWithProviders(<PricingPage />)

      // Yearly button should show savings percent (premium: $8.99*12=$107.88, yearly=$79, savings=~27%)
      const yearlyButton = screen.getByRole('radio', { name: /yearly/i })
      expect(yearlyButton.textContent).toMatch(/yearly \(save \d+%\)/i)
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

  // Issue #772: Trial user detection (GAP 1)
  describe('Trial user detection', () => {
    beforeEach(() => {
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

    it('should NOT show "Current Plan" badge for trial users', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: {
          subscription_id: 'sub_123',
          status: 'trial',
          tier: 'premium',
          billing_interval: 'month',
          amount: '0.00',
          currency: 'USD',
          current_period_end: '2026-04-08T00:00:00Z',
          trial_ends_at: '2026-04-08T00:00:00Z',
          trial_days_remaining: 60,
        },
        isLoading: false,
        error: null,
        refetch: mock(() => Promise.resolve()),
      })

      renderWithProviders(<PricingPage />)

      // No "Current Plan" badges should be visible for trial users
      expect(screen.queryByText('Current Plan')).not.toBeInTheDocument()
    })

    it('should show "Start plan" buttons for all tiers when user is on trial', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: {
          subscription_id: 'sub_123',
          status: 'trial',
          tier: 'premium',
          billing_interval: 'month',
          amount: '0.00',
          currency: 'USD',
          current_period_end: '2026-04-08T00:00:00Z',
          trial_ends_at: '2026-04-08T00:00:00Z',
          trial_days_remaining: 60,
        },
        isLoading: false,
        error: null,
        refetch: mock(() => Promise.resolve()),
      })

      renderWithProviders(<PricingPage />)

      // All tiers should have "Start plan" buttons (5 tiers)
      const startPlanButtons = screen.getAllByRole('button', { name: /start plan/i })
      expect(startPlanButtons.length).toBe(5)
    })

    it('should show trial days remaining banner for trial users', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: {
          subscription_id: 'sub_123',
          status: 'trial',
          tier: 'premium',
          billing_interval: 'month',
          amount: '0.00',
          currency: 'USD',
          current_period_end: '2026-04-08T00:00:00Z',
          trial_ends_at: '2026-04-08T00:00:00Z',
          trial_days_remaining: 45,
        },
        isLoading: false,
        error: null,
        refetch: mock(() => Promise.resolve()),
      })

      renderWithProviders(<PricingPage />)

      expect(screen.getByText(/45 days remaining/i)).toBeInTheDocument()
      expect(screen.getByText(/you're on a free trial/i)).toBeInTheDocument()
    })

    it('should show "Current Plan" badge for paid users (not trial)', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: activeSubscription,
        isLoading: false,
        error: null,
        refetch: mock(() => Promise.resolve()),
      })

      renderWithProviders(<PricingPage />)

      // Should show "Current Plan" badge for active paid users
      const currentPlanElements = screen.getAllByText('Current Plan')
      expect(currentPlanElements.length).toBeGreaterThan(0)
    })

    it('should show directional buttons for non-current tiers when user has active subscription', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: activeSubscription,
        isLoading: false,
        error: null,
        refetch: mock(() => Promise.resolve()),
      })

      renderWithProviders(<PricingPage />)

      // Active premium → 4 household tiers are all upgrades
      const upgradeButtons = screen.getAllByRole('button', { name: /upgrade/i })
      expect(upgradeButtons.length).toBe(4)
    })
  })

  // Issue #1038: Context-aware pricing behavior matrix
  describe('Behavior matrix - context-aware CTAs', () => {
    describe('inactive (no subscription)', () => {
      beforeEach(() => {
        mockUseUser.mockReturnValue({
          user: { id: 'user_123', publicMetadata: {} },
          isSignedIn: true,
          isLoaded: true,
        })
        mockUseGetCurrentSubscription.mockReturnValue({
          data: undefined,
          isLoading: false,
          error: { status: 404, message: 'Not found' },
        })
      })

      it('shows Subscribe buttons for all tiers', () => {
        renderWithProviders(<PricingPage />)
        const subscribeButtons = screen.getAllByRole('button', { name: /subscribe/i })
        expect(subscribeButtons.length).toBe(5)
      })

      it('shows no Current Plan badge', () => {
        renderWithProviders(<PricingPage />)
        expect(screen.queryByText('Current Plan')).not.toBeInTheDocument()
      })
    })

    describe('trialing (paid trial on current tier)', () => {
      beforeEach(() => {
        mockUseUser.mockReturnValue({
          user: { id: 'user_123', publicMetadata: { subscription_tier: 'premium' } },
          isSignedIn: true,
          isLoaded: true,
        })
        mockUseGetCurrentSubscription.mockReturnValue({
          data: {
            subscription_id: 'sub_123',
            status: 'trialing',
            tier: 'premium',
            billing_interval: 'month',
            cancel_at_period_end: false,
          },
          isLoading: false,
          error: null,
        })
      })

      it('shows Current Plan badge for current tier', () => {
        renderWithProviders(<PricingPage />)
        const currentPlanElements = screen.getAllByText('Current Plan')
        expect(currentPlanElements.length).toBeGreaterThan(0)
      })

      it('shows directional buttons for other tiers', () => {
        renderWithProviders(<PricingPage />)
        const upgradeButtons = screen.getAllByRole('button', { name: /upgrade/i })
        expect(upgradeButtons.length).toBe(4)
      })
    })

    describe('cancelled (still within billing period)', () => {
      beforeEach(() => {
        mockUseUser.mockReturnValue({
          user: { id: 'user_123', publicMetadata: { subscription_tier: 'premium' } },
          isSignedIn: true,
          isLoaded: true,
        })
        mockUseGetCurrentSubscription.mockReturnValue({
          data: {
            subscription_id: 'sub_123',
            status: 'cancelled',
            tier: 'premium',
            billing_interval: 'month',
            cancel_at_period_end: true,
            scheduled_change_at: '2026-05-08T00:00:00Z',
          },
          isLoading: false,
          error: null,
        })
        mockUseGetBillingPortalUrl.mockReturnValue({
          data: { portal_url: 'https://portal.paddle.com/123' },
          isLoading: false,
          error: null,
        })
      })

      it('shows Current Plan badge for current tier', () => {
        renderWithProviders(<PricingPage />)
        const currentPlanElements = screen.getAllByText('Current Plan')
        expect(currentPlanElements.length).toBeGreaterThan(0)
      })

      it('shows Resume button for current tier', () => {
        renderWithProviders(<PricingPage />)
        const resumeButton = screen.getByRole('button', { name: /resume/i })
        expect(resumeButton).toBeInTheDocument()
      })

      it('shows directional CTAs (Upgrade/Downgrade/Switch) for other tiers', () => {
        renderWithProviders(<PricingPage />)
        expect(screen.queryAllByRole('button', { name: /change plan/i }).length).toBe(0)
        const directionalButtons = screen.getAllByRole('button', {
          name: /upgrade|downgrade|switch to/i,
        })
        expect(directionalButtons.length).toBeGreaterThan(0)
      })

      it('shows Cancels on {date} label on current tier card when scheduled_change_at is set', () => {
        renderWithProviders(<PricingPage />)
        const expectedDate = new Date('2026-05-08T12:00:00').toLocaleDateString()
        expect(screen.getByText(new RegExp(`Cancels on ${expectedDate}`, 'i'))).toBeInTheDocument()
      })

      it('shows Cancels on {date} label from current_period_end when scheduled_change_at is absent', () => {
        mockUseGetCurrentSubscription.mockReturnValue({
          data: {
            subscription_id: 'sub_123',
            status: 'cancelled',
            tier: 'premium',
            billing_interval: 'month',
            cancel_at_period_end: true,
            current_period_end: '2026-06-01',
          },
          isLoading: false,
          error: null,
        })
        renderWithProviders(<PricingPage />)
        const expectedDate = new Date('2026-06-01T12:00:00').toLocaleDateString()
        expect(screen.getByText(new RegExp(`Cancels on ${expectedDate}`, 'i'))).toBeInTheDocument()
      })
    })

    describe('past_due (expired payment)', () => {
      beforeEach(() => {
        mockUseUser.mockReturnValue({
          user: { id: 'user_123', publicMetadata: { subscription_tier: 'premium' } },
          isSignedIn: true,
          isLoaded: true,
        })
        mockUseGetCurrentSubscription.mockReturnValue({
          data: {
            subscription_id: 'sub_123',
            status: 'past_due',
            tier: 'premium',
            billing_interval: 'month',
          },
          isLoading: false,
          error: null,
        })
      })

      it('shows FrozenAccountBanner', () => {
        renderWithProviders(<PricingPage />)
        expect(screen.getByText(/your subscription has expired/i)).toBeInTheDocument()
      })

      it('shows Resume button for current tier', () => {
        mockUseGetBillingPortalUrl.mockReturnValue({
          data: { portal_url: 'https://portal.paddle.com/past-due' },
          isLoading: false,
          error: null,
        })
        renderWithProviders(<PricingPage />)
        expect(screen.getByRole('button', { name: /resume/i })).toBeInTheDocument()
      })

      it('shows disabled Update Payment buttons for non-current tiers', () => {
        renderWithProviders(<PricingPage />)
        const updatePaymentButtons = screen.getAllByRole('button', { name: /update payment/i })
        expect(updatePaymentButtons.length).toBe(4) // 4 non-current tiers
        updatePaymentButtons.forEach(btn => expect(btn).toBeDisabled())
      })
    })
  })
})
