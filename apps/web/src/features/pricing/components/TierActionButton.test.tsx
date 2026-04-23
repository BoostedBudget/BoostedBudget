import { describe, it, expect, beforeEach, mock, afterEach } from 'bun:test'
import React from 'react'
import { render, screen, fireEvent } from '@testing-library/react'
import * as ReactRouterDom from 'react-router-dom'
import { MemoryRouter } from 'react-router-dom'
import { TierActionButton } from './TierActionButton'
import type { PricingState } from '../hooks/usePricingContext'
import type { SubscriptionTier } from '../../subscription/types'
import type { TierName } from '../../subscription/types'
import {
  mockUseCreatePaddleCheckout,
  mockCreatePaddleCheckoutMutateAsync,
} from '@/test/mocks/subscription-api'

const mockNavigate = mock(() => {})
mock.module('react-router-dom', () => ({
  ...ReactRouterDom,
  useNavigate: () => mockNavigate,
}))

function makeTier(priceMonthly: number): SubscriptionTier {
  return {
    name: 'premium',
    display_name: 'Premium',
    description: '',
    limits: { accounts: -1, transaction_history_days: 365, executions_per_day: -1 },
    features: {
      ads_enabled: false,
      ai_insights: false,
      api_access: false,
      priority_support: false,
      custom_categories: true,
      custom_tags: true,
      advanced_reports: false,
    },
    pricing: {
      price_monthly: priceMonthly,
      price_yearly: priceMonthly * 10,
      mobile_price_yearly: priceMonthly * 10,
      users_included: 1,
    },
  }
}

const tiers: Record<TierName, SubscriptionTier> = {
  premium: makeTier(8.99),
  household_2: makeTier(12.99),
  household_3: makeTier(14.99),
  household_4: makeTier(16.99),
  household_5: makeTier(18.99),
}

const mockOnChangePlan = mock(() => {})

function renderButton(pricingState: PricingState, tierKey: TierName = 'household_2', billingInterval: 'month' | 'year' = 'month') {
  return render(
    <MemoryRouter>
      <TierActionButton
        tierKey={tierKey}
        billingInterval={billingInterval}
        pricingState={pricingState}
        tiers={tiers}
        onChangePlan={mockOnChangePlan}
      />
    </MemoryRouter>
  )
}

beforeEach(() => {
  mockOnChangePlan.mockClear()
  mockNavigate.mockClear()
  mockUseCreatePaddleCheckout.mockReturnValue({
    mutateAsync: mockCreatePaddleCheckoutMutateAsync,
    isPending: false,
    isSuccess: false,
    isError: false,
    error: null,
    reset: mock(() => {}),
  })
  mockCreatePaddleCheckoutMutateAsync.mockResolvedValue({
    checkout_url: 'https://checkout.paddle.com/txn_123',
    transaction_id: 'txn_123',
  })
  sessionStorage.clear()
})

afterEach(() => {
  sessionStorage.clear()
})

describe('TierActionButton', () => {
  describe('loading state', () => {
    it('shows disabled loading button', () => {
      renderButton({ status: 'loading' })
      const btn = screen.getByRole('button')
      expect(btn).toBeDisabled()
    })
  })

  describe('signed-out state', () => {
    it('shows Subscribe CTA', () => {
      renderButton({ status: 'signed-out' })
      expect(screen.getByRole('button').textContent).toContain('Subscribe')
    })

    it('stores checkout intent in sessionStorage on click', () => {
      const before = Date.now()
      renderButton({ status: 'signed-out' }, 'premium', 'year')
      fireEvent.click(screen.getByRole('button'))
      const stored = JSON.parse(sessionStorage.getItem('checkout_intent') ?? '{}')
      expect(stored.tier).toBe('premium')
      expect(stored.billingInterval).toBe('year')
      expect(stored.storedAt).toBeGreaterThanOrEqual(before)
    })

    it('navigates to sign-up on click', () => {
      renderButton({ status: 'signed-out' })
      fireEvent.click(screen.getByRole('button'))
      expect(mockNavigate).toHaveBeenCalledWith('/sign-up', expect.anything())
    })
  })

  describe('inactive state', () => {
    it('shows Subscribe CTA', () => {
      renderButton({ status: 'inactive' })
      expect(screen.getByRole('button').textContent).toContain('Subscribe')
    })

    it('is not disabled', () => {
      renderButton({ status: 'inactive' })
      expect(screen.getByRole('button')).not.toBeDisabled()
    })
  })

  describe('trial state', () => {
    const trialState: PricingState = {
      status: 'trial',
      tier: 'premium',
      daysRemaining: 14,
      trialEndsAt: '2026-05-01',
    }

    it('shows Start plan CTA', () => {
      renderButton(trialState)
      expect(screen.getByRole('button').textContent).toContain('Start')
    })

    it('is not disabled', () => {
      renderButton(trialState)
      expect(screen.getByRole('button')).not.toBeDisabled()
    })
  })

  describe('active state', () => {
    it('shows disabled Current Plan for same tier + interval', () => {
      const activeState: PricingState = {
        status: 'active',
        tier: 'premium',
        billingInterval: 'month',
        subscriptionId: 'sub_123',
        cancelAtPeriodEnd: false,
      }
      renderButton(activeState, 'premium', 'month')
      const btn = screen.getByRole('button')
      expect(btn).toBeDisabled()
      expect(btn.textContent).toContain('Current Plan')
    })

    it('shows Upgrade for higher-price tier', () => {
      const activeState: PricingState = {
        status: 'active',
        tier: 'premium',
        billingInterval: 'month',
        subscriptionId: 'sub_123',
        cancelAtPeriodEnd: false,
      }
      renderButton(activeState, 'household_2', 'month')
      expect(screen.getByRole('button').textContent).toContain('Upgrade')
    })

    it('shows Downgrade for lower-price tier', () => {
      const activeState: PricingState = {
        status: 'active',
        tier: 'household_5',
        billingInterval: 'month',
        subscriptionId: 'sub_123',
        cancelAtPeriodEnd: false,
      }
      renderButton(activeState, 'premium', 'month')
      expect(screen.getByRole('button').textContent).toContain('Downgrade')
    })

    it('shows interval switch label when same tier but different interval', () => {
      const activeState: PricingState = {
        status: 'active',
        tier: 'premium',
        billingInterval: 'month',
        subscriptionId: 'sub_123',
        cancelAtPeriodEnd: false,
      }
      renderButton(activeState, 'premium', 'year')
      expect(screen.getByRole('button').textContent?.toLowerCase()).toContain('yearly')
    })

    it('calls onChangePlan with target tier and interval on Upgrade click', () => {
      const activeState: PricingState = {
        status: 'active',
        tier: 'premium',
        billingInterval: 'month',
        subscriptionId: 'sub_123',
        cancelAtPeriodEnd: false,
      }
      renderButton(activeState, 'household_2', 'month')
      fireEvent.click(screen.getByRole('button'))
      expect(mockOnChangePlan).toHaveBeenCalledWith('household_2', 'month')
    })
  })

  describe('trialing state', () => {
    it('shows disabled Current Plan for same tier + interval', () => {
      const trialingState: PricingState = {
        status: 'trialing',
        tier: 'premium',
        billingInterval: 'month',
        subscriptionId: 'sub_123',
      }
      renderButton(trialingState, 'premium', 'month')
      const btn = screen.getByRole('button')
      expect(btn).toBeDisabled()
      expect(btn.textContent).toContain('Current Plan')
    })

    it('shows Upgrade for higher-price tier', () => {
      const trialingState: PricingState = {
        status: 'trialing',
        tier: 'premium',
        billingInterval: 'month',
        subscriptionId: 'sub_123',
      }
      renderButton(trialingState, 'household_3', 'month')
      expect(screen.getByRole('button').textContent).toContain('Upgrade')
    })
  })

  describe('cancelled state', () => {
    it('shows disabled Current Plan for same tier', () => {
      const cancelledState: PricingState = {
        status: 'cancelled',
        tier: 'premium',
        billingInterval: 'month',
        subscriptionId: 'sub_123',
        cancelAtPeriodEnd: true,
        scheduledChangeAt: '2026-06-01T00:00:00Z',
      }
      renderButton(cancelledState, 'premium', 'month')
      const btn = screen.getByRole('button')
      expect(btn).toBeDisabled()
    })

    it('shows Change Plan for different tier', () => {
      const cancelledState: PricingState = {
        status: 'cancelled',
        tier: 'premium',
        billingInterval: 'month',
        subscriptionId: 'sub_123',
        cancelAtPeriodEnd: true,
      }
      renderButton(cancelledState, 'household_2', 'month')
      const btn = screen.getByRole('button')
      expect(btn).not.toBeDisabled()
    })

    it('calls onChangePlan on change click for different tier', () => {
      const cancelledState: PricingState = {
        status: 'cancelled',
        tier: 'premium',
        billingInterval: 'month',
        subscriptionId: 'sub_123',
        cancelAtPeriodEnd: true,
      }
      renderButton(cancelledState, 'household_2', 'month')
      fireEvent.click(screen.getByRole('button'))
      expect(mockOnChangePlan).toHaveBeenCalledWith('household_2', 'month')
    })
  })

  describe('past_due state', () => {
    it('shows disabled button', () => {
      const pastDueState: PricingState = {
        status: 'past_due',
        tier: 'premium',
        billingInterval: 'month',
        subscriptionId: 'sub_123',
      }
      renderButton(pastDueState, 'household_2', 'month')
      expect(screen.getByRole('button')).toBeDisabled()
    })
  })
})
