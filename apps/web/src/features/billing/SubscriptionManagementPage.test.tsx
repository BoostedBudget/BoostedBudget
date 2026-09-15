/**
 * SubscriptionManagementPage Tests
 * Issue #843: Polished loading/error states for billing page
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router'
import { SubscriptionManagementPage } from './SubscriptionManagementPage'
import {
  mockUseGetCurrentSubscription,
  mockUseGetSubscriptionTiers,
  mockUseGetBillingPortalUrl,
} from '@/test/mocks/subscription-api'
import { mockUseGetUserProfile } from '@/test/mocks/profile-api'
import { mockSubscriptionTiersResponse } from '@/test/fixtures/subscriptionFixtures'

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/billing/manage']}>
      <Routes>
        <Route path="/billing/manage" element={<SubscriptionManagementPage />} />
        <Route path="/pricing" element={<div data-testid="pricing-page">Pricing Page</div>} />
      </Routes>
    </MemoryRouter>
  )
}

describe('SubscriptionManagementPage', () => {
  beforeEach(() => {
    // Default: loading state
    mockUseGetCurrentSubscription.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve({ data: undefined, error: null }),
    })
    mockUseGetSubscriptionTiers.mockReturnValue({
      data: { tiers: mockSubscriptionTiersResponse.tiers },
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })
    mockUseGetBillingPortalUrl.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })
    // Non-comped by default (the page's hook also consults the profile-derived presentation as a
    // resilience fallback for comped users, Issue #1413).
    mockUseGetUserProfile.mockClear()
    mockUseGetUserProfile.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })
  })

  describe('Loading state', () => {
	  it('offers plans for a confirmed inactive account without paid billing controls', () => {
		mockUseGetCurrentSubscription.mockReturnValue({data:{status:'inactive',subscription_id:'',tier:'premium',is_comped:false},isLoading:false,error:null,refetch:()=>Promise.resolve()} as unknown as ReturnType<typeof mockUseGetCurrentSubscription>)
		renderPage()
		expect(screen.getByRole('button',{name:'View Plans'})).toBeDefined()
		expect(screen.queryByRole('button',{name:'Cancel Subscription'})).toBeNull()
	  })
    it('renders skeleton with role="status" when subscription is loading', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: undefined,
        isLoading: true,
        error: null,
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      renderPage()

      expect(screen.getByRole('status')).toBeDefined()
    })

    it('renders skeleton when tiers are loading', () => {
      mockUseGetSubscriptionTiers.mockReturnValue({
        data: undefined,
        isLoading: true,
        error: null,
        refetch: () => Promise.resolve(),
      })

      renderPage()

      expect(screen.getByRole('status')).toBeDefined()
    })

    it('does not render error state when loading', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: undefined,
        isLoading: true,
        error: null,
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      renderPage()

      expect(screen.queryByRole('alert')).toBeNull()
    })
  })

  describe('Error state (non-404)', () => {
    it('renders error alert for non-404 errors', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: { status: 500, message: 'Server error' },
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      renderPage()

      expect(screen.getByRole('alert')).toBeDefined()
    })

    it('shows "Unable to load subscription" heading for errors', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: { status: 500, message: 'Server error' },
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      renderPage()

      expect(screen.getByText('Unable to load subscription')).toBeDefined()
    })

    it('does not expose raw HTTP status codes', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: { status: 500, message: 'Internal Server Error' },
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      const { container } = renderPage()

      expect(container.textContent).not.toContain('500')
      expect(container.textContent).not.toContain('Internal Server Error')
    })

    it('renders Try Again button on error', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: { status: 503, message: 'Service unavailable' },
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      renderPage()

      expect(screen.getByRole('button', { name: /try again/i })).toBeDefined()
    })
  })

  describe('404 / No subscription state', () => {
    it('renders no-subscription empty state when 404', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: { status: 404, message: 'Not Found' },
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      renderPage()

      expect(screen.getByText('No Active Subscription')).toBeDefined()
    })

    it('does not render error alert for 404', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: { status: 404, message: 'Not Found' },
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      renderPage()

      expect(screen.queryByRole('alert')).toBeNull()
    })

    it('renders "View Plans" CTA for 404', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: { status: 404, message: 'Not Found' },
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      renderPage()

      expect(screen.getByRole('button', { name: /view plans/i })).toBeDefined()
    })

    it('does not expose raw "404" text for 404 error', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: { status: 404, message: 'Not Found' },
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      const { container } = renderPage()

      expect(container.textContent).not.toContain('404')
    })

    it('renders empty state when subscription data is null/undefined', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: null,
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      renderPage()

      expect(screen.getByText('No Active Subscription')).toBeDefined()
    })
  })

  describe('Active subscription', () => {
    it('renders subscription details for active status', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: {
          subscription_id: 'sub_paid',
          status: 'active',
          tier: 'premium',
          amount: '8.99',
          currency: 'USD',
          billing_interval: 'month',
          next_billing_date: '2026-04-09',
        },
        isLoading: false,
        error: null,
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      renderPage()

      expect(screen.queryByRole('status')).toBeNull()
      expect(screen.queryByRole('alert')).toBeNull()
      expect(screen.getByText('Manage Subscription')).toBeDefined()
    })

    it('opens the resolved management portal as the single self-service action', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: {
          subscription_id: 'sub_paid',
          status: 'active',
          tier: 'premium',
          amount: '8.99',
          currency: 'USD',
          billing_interval: 'month',
          next_billing_date: '2026-04-09',
        },
        isLoading: false,
        error: null,
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })
      mockUseGetBillingPortalUrl.mockReturnValue({
        data: { portal_url: 'https://portal.paddle.com/active' },
        isLoading: false,
        error: null,
        refetch: () => Promise.resolve(),
      })

      renderPage()

      const manage = screen.getByRole('link', { name: /manage subscription/i })
      expect(manage).toHaveAttribute('href', 'https://portal.paddle.com/active')
      expect(screen.queryByRole('button', { name: /change plan/i })).toBeNull()
      expect(screen.queryByRole('button', { name: /cancel subscription/i })).toBeNull()
    })

    it('shows an error state (not a dead button) when the portal URL fails to resolve', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: {
          subscription_id: 'sub_paid',
          status: 'active',
          tier: 'premium',
          amount: '8.99',
          currency: 'USD',
          billing_interval: 'month',
        },
        isLoading: false,
        error: null,
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })
      mockUseGetBillingPortalUrl.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: { status: 503, message: 'Service Unavailable' },
        refetch: () => Promise.resolve(),
      })

      renderPage()

      expect(screen.getByRole('alert')).toHaveTextContent(/couldn't load the billing portal/i)
      expect(screen.queryByRole('link', { name: /manage subscription/i })).toBeNull()
    })
  })

  // Issue #1413: admin-comped users get real access with no Paddle subscription behind it.
  describe('Comped user', () => {
    it('preserves observed subscription details when a refresh is unavailable', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: { status: 'active', tier: 'premium', subscription_id: 'sub_paid', amount: '8.99', currency: 'USD', billing_interval: 'month' },
        isLoading: false, error: { status: 503 }, refetch: () => Promise.resolve(),
      } as unknown as ReturnType<typeof mockUseGetCurrentSubscription>)
      renderPage()
      expect(screen.getByRole('heading', { name: 'Current Subscription' })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Refresh subscription status' })).toBeInTheDocument()
    })
    it('offers the single Manage subscription action for paid plus complimentary access', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: { status: 'active', tier: 'premium', subscription_id: 'sub_paid', is_comped: true, amount: '8.99', currency: 'USD', billing_interval: 'month' },
        isLoading: false, error: null, refetch: () => Promise.resolve(),
      } as unknown as ReturnType<typeof mockUseGetCurrentSubscription>)
      mockUseGetBillingPortalUrl.mockReturnValue({
        data: { portal_url: 'https://portal.paddle.com/paid' },
        isLoading: false, error: null, refetch: () => Promise.resolve(),
      })
      renderPage()
      expect(screen.getByRole('link', { name: /manage subscription/i })).toHaveAttribute('href', 'https://portal.paddle.com/paid')
      expect(screen.queryByRole('button', { name: /change plan/i })).toBeNull()
      expect(screen.queryByRole('button', { name: /cancel subscription/i })).toBeNull()
    })
    it('renders a complimentary view instead of the empty state when the subscription 404s', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: { status: 404, message: 'Not Found' },
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })
      mockUseGetUserProfile.mockReturnValue({
        data: {
          id: 'user-1',
          subscription_status: 'inactive',
          subscription_tier: 'premium',
          is_comped: true,
          comp_ends_at: '2026-08-01T00:00:00Z',
        },
        isLoading: false,
        error: null,
        refetch: () => Promise.resolve(),
      })

      renderPage()

      expect(screen.queryByText('No Active Subscription')).toBeNull()
      expect(screen.getByText(/complimentary access ends 2026-08-01/i)).toBeInTheDocument()
    })

    it('does not render BillingDetails — no next-billing date, no billing-cycle amount row', () => {
      // Realistic comped payload: amount is zeroed (no real Paddle charge behind it).
      mockUseGetCurrentSubscription.mockReturnValue({
        data: {
          status: 'active',
          tier: 'premium',
          is_comped: true,
          amount: '0.00',
          currency: 'USD',
          billing_interval: 'month',
          next_billing_date: '2026-09-01',
        },
        isLoading: false,
        error: null,
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      renderPage()

      // BillingDetails entirely absent — its heading and its "Next Billing Date:"/"Amount:" rows.
      expect(screen.queryByText('Billing Details')).toBeNull()
      expect(screen.queryByText('Next Billing Date:')).toBeNull()
      expect(screen.queryByText('Amount:')).toBeNull()
    })

    it('does not render Change Plan or Cancel Subscription actions for a comped user', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: { status: 'active', tier: 'premium', is_comped: true },
        isLoading: false,
        error: null,
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      renderPage()

      expect(screen.queryByRole('button', { name: /change plan/i })).toBeNull()
      expect(screen.queryByRole('button', { name: /cancel subscription/i })).toBeNull()
    })

    it('offers a link to /pricing for a comped user', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: { status: 'active', tier: 'premium', is_comped: true },
        isLoading: false,
        error: null,
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      renderPage()

      expect(screen.getByRole('button', { name: /view plans/i })).toBeInTheDocument()
    })

    it('shows explicit no-end-date copy for a permanent comp', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: { status: 'active', tier: 'premium', is_comped: true },
        isLoading: false,
        error: null,
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      renderPage()

      expect(screen.getByText(/no end date/i)).toBeInTheDocument()
    })

    it('reuses CurrentSubscriptionDisplay for the tier block when subscription data is present', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: {
          status: 'active',
          tier: 'premium',
          is_comped: true,
          amount: '0.00',
          currency: 'USD',
          billing_interval: 'month',
        },
        isLoading: false,
        error: null,
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      renderPage()

      expect(screen.getByLabelText('Current subscription details')).toBeInTheDocument()
    })

    it('regression: non-comped 404 still shows the empty state', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: { status: 404, message: 'Not Found' },
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      renderPage()

      expect(screen.getByText('No Active Subscription')).toBeInTheDocument()
    })
  })

  // Issue #1413 M11: the backend deliberately preserves the raw Paddle status for a comped user
  // who also has a real (e.g. past_due) Paddle subscription behind them. That raw status must
  // never be surfaced as this user's access status — it contradicts the complimentary-access
  // banner and is alarming for someone who actually has access.
  describe('Comped user with a raw past_due Paddle subscription (M11)', () => {
    it('does not show the raw past_due billing status in the status display', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: {
          status: 'past_due',
          tier: 'premium',
          is_comped: true,
          amount: '0.00',
          currency: 'USD',
          billing_interval: 'month',
        },
        isLoading: false,
        error: null,
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      const { container } = renderPage()

      // Matches both 'Past Due' and 'Past_due' (the underscore-preserving pre-fix rendering).
      expect(container.textContent).not.toMatch(/past[\s_]?due/i)
    })

    it('still shows the complimentary view and no next-billing date or amount for that same user', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: {
          status: 'past_due',
          tier: 'premium',
          is_comped: true,
          amount: '0.00',
          currency: 'USD',
          billing_interval: 'month',
          next_billing_date: '2026-09-01',
        },
        isLoading: false,
        error: null,
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      renderPage()

      expect(screen.getByText(/no end date/i)).toBeInTheDocument()
      expect(screen.queryByText('Next Billing Date:')).toBeNull()
      expect(screen.queryByText('Amount:')).toBeNull()
    })

    it('regression: a non-comped past_due user still sees "Past Due"', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: {
          subscription_id: 'sub_paid',
          status: 'past_due',
          tier: 'premium',
          is_comped: false,
          amount: '8.99',
          currency: 'USD',
          billing_interval: 'month',
        },
        isLoading: false,
        error: null,
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      renderPage()

      expect(screen.getByText('Payment Past Due')).toBeInTheDocument()
    })
  })
})
