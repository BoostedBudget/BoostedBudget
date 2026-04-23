import { describe, it, expect, beforeEach } from 'bun:test'
import React from 'react'
import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { usePricingContext } from './usePricingContext'
import { mockUseUser } from '@/test/mocks/clerk'
import {
  mockUseGetCurrentSubscription,
  mockUseGetSubscriptionTiers,
  mockRefetchSubscriptionTiers,
} from '@/test/mocks/subscription-api'

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
      {tiers && <span data-testid="tiers-loaded">yes</span>}
    </div>
  )
}

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
      limits: { accounts: 5, transaction_history_days: 365, executions_per_day: 50 },
      features: { ads_enabled: false, ai_insights: false, api_access: false, priority_support: false, custom_categories: true, custom_tags: true, advanced_reports: false },
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
})

describe('usePricingContext', () => {
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

  it('returns inactive when subscription query fails with non-404 error', () => {
    mockUseUser.mockReturnValue(signedInUser)
    mockUseGetCurrentSubscription.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: { status: 500, message: 'Internal Server Error' },
    })
    renderWithProviders()
    // Non-404 errors should show Subscribe CTAs (inactive), not spin forever (loading)
    expect(screen.getByTestId('status').textContent).toBe('inactive')
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
})
