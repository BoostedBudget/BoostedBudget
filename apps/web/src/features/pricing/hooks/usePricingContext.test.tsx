import { describe, it, expect, beforeEach } from 'bun:test'
import React from 'react'
import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router'
import { usePricingContext } from './usePricingContext'
import { mockUseUser } from '@/test/mocks/clerk'
import {
  mockUseGetCurrentSubscription,
  mockUseGetSubscriptionTiers,
  mockRefetchSubscriptionTiers,
} from '@/test/mocks/subscription-api'
import { mockUseGetUserProfile } from '@/test/mocks/profile-api'

function PricingContextReader() {
  const { pricingState, tiers } = usePricingContext()
  return (
    <div>
      <span data-testid="status">{pricingState.status}</span>
      {pricingState.status === 'trial' && (
        <>
          <span data-testid="tier">{pricingState.tier}</span>
          <span data-testid="days">{pricingState.daysRemaining}</span>
        </>
      )}
      {pricingState.status === 'active' && (
        <>
          <span data-testid="tier">{pricingState.tier}</span>
          <span data-testid="interval">{pricingState.billingInterval}</span>
          <span data-testid="cancel-at-period-end">{String(pricingState.cancelAtPeriodEnd)}</span>
        </>
      )}
      {pricingState.status === 'cancelled' && (
        <span data-testid="scheduled-change-at">{pricingState.scheduledChangeAt}</span>
      )}
      {pricingState.status === 'comped' && (
        <>
          <span data-testid="tier">{pricingState.tier}</span>
          <span data-testid="comp-ends-at">{pricingState.compEndsAt}</span>
        </>
      )}
      {tiers && <span data-testid="tiers-loaded">yes</span>}
    </div>
  )
}

it.each(['inactive', 'unresolved'] as const)('handles confirmed HTTP 200 %s without permanent loading', (status) => {
  mockUseUser.mockReturnValue(signedInUser as unknown as ReturnType<typeof mockUseUser>)
  mockUseGetCurrentSubscription.mockReturnValue({data:{status,subscription_id:'',tier:'premium',is_comped:false},isLoading:false,error:null} as unknown as ReturnType<typeof mockUseGetCurrentSubscription>)
  renderWithProviders()
  expect(screen.getByTestId('status').textContent).toBe(status)
})

function renderWithProviders() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <PricingContextReader />
      </MemoryRouter>
    </QueryClientProvider>
  )
}

const baseTiersData = {
  tiers: {
    premium: {
      name: 'premium',
      display_name: 'Premium',
      description: '',
      limits: { transaction_history_days: 365 },
      features: { api_access: false, priority_support: false, custom_categories: true, custom_tags: true, advanced_reports: false },
      pricing: { price_monthly: 8.99, price_yearly: 89.90, mobile_price_yearly: 89.90, users_included: 1 },
    },
  },
}

const signedInUser = {
  isLoaded: true,
  isSignedIn: true,
  user: { id: 'user_123', publicMetadata: { subscription_tier: 'premium' } },
}

beforeEach(() => {
  mockUseUser.mockReturnValue({ isLoaded: true, isSignedIn: false, user: null })
  mockUseGetCurrentSubscription.mockReturnValue({ data: undefined, isLoading: false, error: null })
  mockUseGetSubscriptionTiers.mockReturnValue({
    data: baseTiersData,
    isLoading: false,
    error: null,
    refetch: mockRefetchSubscriptionTiers,
  })
  // Non-comped by default — usePricingContext also consults the profile-derived presentation as a
  // resilience fallback for comped users (Issue #1413); keep it a no-op unless a test overrides it.
  mockUseGetUserProfile.mockClear()
  mockUseGetUserProfile.mockReturnValue({
    data: undefined,
    isLoading: false,
    error: null,
    refetch: () => Promise.resolve(),
  })
})

describe('usePricingContext', () => {
  it.each(['inactive', 'trial', 'active'])('does not offer checkout from cached %s data after a failed refresh', (status) => {
    mockUseUser.mockReturnValue(signedInUser as unknown as ReturnType<typeof mockUseUser>)
    mockUseGetCurrentSubscription.mockReturnValue({
      data: { status, tier: 'premium', subscription_id: '', is_comped: status === 'active' },
      isLoading: false,
      error: { status: 503, message: 'Unavailable' },
    } as unknown as ReturnType<typeof mockUseGetCurrentSubscription>)
    renderWithProviders()
    expect(screen.getByTestId('status').textContent).toBe('unresolved')
  })

  it('returns loading when auth is not yet loaded', () => {
    mockUseUser.mockReturnValue({ isLoaded: false, isSignedIn: false, user: null })
    renderWithProviders()
    expect(screen.getByTestId('status').textContent).toBe('loading')
  })

  it('returns loading when tiers are loading', () => {
    mockUseGetSubscriptionTiers.mockReturnValue({ data: undefined, isLoading: true, error: null, refetch: () => Promise.resolve() })
    renderWithProviders()
    expect(screen.getByTestId('status').textContent).toBe('loading')
  })

  it('returns signed-out when user is not signed in', () => {
    renderWithProviders()
    expect(screen.getByTestId('status').textContent).toBe('signed-out')
  })

  it('returns inactive when signed in but subscription returns 404', () => {
    mockUseUser.mockReturnValue(signedInUser)
    mockUseGetCurrentSubscription.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: { status: 404, message: 'Not Found' },
    })
    renderWithProviders()
    expect(screen.getByTestId('status').textContent).toBe('inactive')
  })

  it('returns unresolved when subscription query fails with non-404 error', () => {
    mockUseUser.mockReturnValue(signedInUser)
    mockUseGetCurrentSubscription.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: { status: 500, message: 'Internal Server Error' },
    })
    renderWithProviders()
    // Billing failures must offer recovery rather than imply there is no paid plan.
    expect(screen.getByTestId('status').textContent).toBe('unresolved')
  })

  it('returns trial state with days remaining', () => {
    mockUseUser.mockReturnValue(signedInUser)
    mockUseGetCurrentSubscription.mockReturnValue({
      data: {
        status: 'trial',
        tier: 'premium',
        trial_days_remaining: 42,
        trial_ends_at: '2026-06-01',
        billing_interval: 'month',
        amount: '0.00',
        currency: 'USD',
        cancel_at_period_end: false,
      },
      isLoading: false,
      error: null,
    })
    renderWithProviders()
    expect(screen.getByTestId('status').textContent).toBe('trial')
    expect(screen.getByTestId('tier').textContent).toBe('premium')
    expect(screen.getByTestId('days').textContent).toBe('42')
  })

  it('returns active state with tier and billing interval', () => {
    mockUseUser.mockReturnValue(signedInUser)
    mockUseGetCurrentSubscription.mockReturnValue({
      data: {
        status: 'active',
        tier: 'premium',
        billing_interval: 'month',
        subscription_id: 'sub_123',
        amount: '8.99',
        currency: 'USD',
        cancel_at_period_end: false,
      },
      isLoading: false,
      error: null,
    })
    renderWithProviders()
    expect(screen.getByTestId('status').textContent).toBe('active')
    expect(screen.getByTestId('tier').textContent).toBe('premium')
    expect(screen.getByTestId('interval').textContent).toBe('month')
    expect(screen.getByTestId('cancel-at-period-end').textContent).toBe('false')
  })

  it('returns cancelled state with scheduledChangeAt', () => {
    mockUseUser.mockReturnValue(signedInUser)
    mockUseGetCurrentSubscription.mockReturnValue({
      data: {
        status: 'cancelled',
        tier: 'premium',
        billing_interval: 'month',
        subscription_id: 'sub_123',
        amount: '8.99',
        currency: 'USD',
        cancel_at_period_end: true,
        scheduled_change_at: '2026-05-01T00:00:00Z',
      },
      isLoading: false,
      error: null,
    })
    renderWithProviders()
    expect(screen.getByTestId('status').textContent).toBe('cancelled')
    expect(screen.getByTestId('scheduled-change-at').textContent).toBe('2026-05-01T00:00:00Z')
  })

  it('returns past_due state', () => {
    mockUseUser.mockReturnValue(signedInUser)
    mockUseGetCurrentSubscription.mockReturnValue({
      data: {
        status: 'past_due',
        tier: 'premium',
        billing_interval: 'month',
        subscription_id: 'sub_123',
        amount: '8.99',
        currency: 'USD',
        cancel_at_period_end: false,
      },
      isLoading: false,
      error: null,
    })
    renderWithProviders()
    expect(screen.getByTestId('status').textContent).toBe('past_due')
  })

  it('exposes tiers map when loaded', () => {
    renderWithProviders()
    expect(screen.getByTestId('tiers-loaded').textContent).toBe('yes')
  })

  // Issue #1413: admin-comped users get real access even though they have no Paddle subscription.
  describe('Comped users (#1413)', () => {
    it('keeps paused paid subscriptions in a recoverable pricing state', () => {
      mockUseUser.mockReturnValue(signedInUser as unknown as ReturnType<typeof mockUseUser>)
      mockUseGetCurrentSubscription.mockReturnValue({
        data: { status: 'paused', tier: 'premium', subscription_id: 'sub_paused' },
        isLoading: false, error: null,
      } as unknown as ReturnType<typeof mockUseGetCurrentSubscription>)
      renderWithProviders()
      expect(screen.getByTestId('status').textContent).toBe('paused')
    })
    it('uses paid subscription controls when a comp also exists', () => {
      mockUseUser.mockReturnValue(signedInUser as unknown as ReturnType<typeof mockUseUser>)
      mockUseGetCurrentSubscription.mockReturnValue({
        data: { status: 'active', tier: 'household_2', subscription_id: 'sub_paid', is_comped: true },
        isLoading: false, error: null,
      } as unknown as ReturnType<typeof mockUseGetCurrentSubscription>)
      renderWithProviders()
      expect(screen.getByTestId('status').textContent).toBe('active')
    })
    it('returns comped state (with tier and comp end date) from the subscription payload', () => {
      mockUseUser.mockReturnValue(signedInUser)
      mockUseGetCurrentSubscription.mockReturnValue({
        data: {
          status: 'active',
          tier: 'household_2',
          is_comped: true,
          comp_ends_at: '2026-08-01T00:00:00Z',
          amount: '0.00',
          currency: 'USD',
          cancel_at_period_end: false,
        },
        isLoading: false,
        error: null,
      })
      renderWithProviders()
      expect(screen.getByTestId('status').textContent).toBe('comped')
      expect(screen.getByTestId('tier').textContent).toBe('household_2')
      expect(screen.getByTestId('comp-ends-at').textContent).toBe('2026-08-01T00:00:00Z')
    })

    it('returns comped state via the profile when /subscription/current 404s (resilience fallback)', () => {
      mockUseUser.mockReturnValue(signedInUser)
      mockUseGetCurrentSubscription.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: { status: 404, message: 'Not Found' },
      })
      mockUseGetUserProfile.mockReturnValue({
        data: {
          id: 'user-1',
          subscription_status: 'inactive',
          subscription_tier: 'premium',
          effective_tier: 'premium',
          is_comped: true,
          comp_ends_at: '2026-09-01T00:00:00Z',
        },
        isLoading: false,
        error: null,
        refetch: () => Promise.resolve(),
      })
      renderWithProviders()
      expect(screen.getByTestId('status').textContent).toBe('comped')
      expect(screen.getByTestId('tier').textContent).toBe('premium')
      expect(screen.getByTestId('comp-ends-at').textContent).toBe('2026-09-01T00:00:00Z')
    })

    it('regression: 404 + non-comped profile still returns inactive', () => {
      mockUseUser.mockReturnValue(signedInUser)
      mockUseGetCurrentSubscription.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: { status: 404, message: 'Not Found' },
      })
      renderWithProviders()
      expect(screen.getByTestId('status').textContent).toBe('inactive')
    })
  })
})
