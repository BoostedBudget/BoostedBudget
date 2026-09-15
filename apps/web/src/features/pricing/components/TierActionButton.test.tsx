import { describe, it, expect, beforeEach, mock, afterEach, spyOn } from 'bun:test'
import React from 'react'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { TierActionButton } from './TierActionButton'
import type { PricingState } from '../hooks/usePricingContext'
import type { TierName } from '../../subscription/types'
import { mockUseClerk, createMockClerk, mockUseAuth, createMockClerkAuth } from '@/test/mocks/clerk'
import { revenueCatCheckout } from '../../billing/revenueCatRuntime'

const mockStartCheckout = mock(async () => {})
let restoreStartCheckout = () => {}

interface PortalProps {
  url?: string
  loading?: boolean
  error?: unknown
}

function renderButton(
  pricingState: PricingState,
  tierKey: TierName = 'household_2',
  billingInterval: 'month' | 'year' = 'month',
  portal: PortalProps = {},
) {
  return render(
    <MemoryRouter>
      <TierActionButton
        tierKey={tierKey}
        billingInterval={billingInterval}
        pricingState={pricingState}
        billingPortalUrl={portal.url}
        isLoadingPortal={portal.loading}
        portalError={portal.error}
      />
    </MemoryRouter>
  )
}

it('links unresolved subscription state to billing recovery', () => {
  renderButton({status:'unresolved'})
  expect(screen.getByRole('link',{name:'Check subscription status'}).getAttribute('href')).toBe('/billing/manage')
})

beforeEach(() => {
  mockUseClerk.mockClear()
  mockUseClerk.mockReturnValue(createMockClerk())
  mockUseAuth.mockReturnValue(createMockClerkAuth({ userId: 'user_button', isSignedIn: true }))
  const startSpy = spyOn(revenueCatCheckout, 'start').mockImplementation(mockStartCheckout)
  restoreStartCheckout = () => { startSpy.mockRestore() }
  mockStartCheckout.mockClear()
  revenueCatCheckout.setIdentity(null)
  sessionStorage.clear()
})

afterEach(() => {
  restoreStartCheckout()
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

    it('calls openSignUp on click after storing checkout intent', () => {
      const before = Date.now()
      const clerkMock = createMockClerk()
      mockUseClerk.mockReturnValue(clerkMock)

      renderButton({ status: 'signed-out' }, 'premium', 'year')
      fireEvent.click(screen.getByRole('button'))

      const stored = JSON.parse(sessionStorage.getItem('checkout_intent') ?? '{}')
      expect(stored.tier).toBe('premium')
      expect(stored.billingInterval).toBe('year')
      expect(stored.storedAt).toBeGreaterThanOrEqual(before)
      expect(clerkMock.openSignUp).toHaveBeenCalledTimes(1)
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

  // #1652: every paid subscription is managed through the provider's portal — one action, no
  // in-app Upgrade/Downgrade/Resubscribe/Update-Payment mutations.
  describe('subscribed states route to the management portal', () => {
    const activeState: PricingState = {
      status: 'active',
      tier: 'premium',
      billingInterval: 'month',
      subscriptionId: 'sub_123',
      cancelAtPeriodEnd: false,
    }
    const trialingState: PricingState = {
      status: 'trialing',
      tier: 'premium',
      billingInterval: 'month',
      subscriptionId: 'sub_123',
    }
    const cancelledState: PricingState = {
      status: 'cancelled',
      tier: 'premium',
      billingInterval: 'month',
      subscriptionId: 'sub_123',
      cancelAtPeriodEnd: true,
    }
    const pastDueState: PricingState = {
      status: 'past_due',
      tier: 'premium',
      billingInterval: 'month',
      subscriptionId: 'sub_123',
    }
    const pausedState: PricingState = {
      status: 'paused',
      tier: 'premium',
      billingInterval: 'month',
      subscriptionId: 'sub_123',
    }

    for (const [name, state] of [
      ['active', activeState],
      ['trialing', trialingState],
      ['cancelled', cancelledState],
      ['past_due', pastDueState],
      ['paused', pausedState],
    ] as const) {
      it(`renders the resolved management link for ${name}`, () => {
        renderButton(state, 'household_2', 'month', { url: 'https://portal.paddle.com/sub' })
        const link = screen.getByRole('link', { name: /manage subscription/i })
        expect(link).toHaveAttribute('href', 'https://portal.paddle.com/sub')
        expect(link).toHaveAttribute('target', '_blank')
      })

      it(`renders a disabled control (not a dead mutation) for ${name} while the portal is unresolved`, () => {
        renderButton(state)
        const btn = screen.getByRole('button', { name: /manage subscription/i })
        expect(btn).toBeDisabled()
      })
    }

    it('shows an error state when the portal URL fails to resolve', () => {
      renderButton(activeState, 'household_2', 'month', { error: { status: 503 } })
      expect(screen.getByRole('alert')).toHaveTextContent(/couldn't load the billing portal/i)
      expect(screen.queryByRole('link', { name: /manage subscription/i })).toBeNull()
    })

    it('shows a loading control while the portal URL is being resolved', () => {
      renderButton(activeState, 'household_2', 'month', { loading: true })
      expect(screen.getByRole('button')).toBeDisabled()
      expect(screen.getByRole('button').textContent).toContain('Loading')
    })
  })

  // Issue #1413: comped users keep full commerce functionality — they may legitimately want to
  // buy a paid plan (e.g. their comp has an end date), so only their own comped tier is disabled.
  describe('comped state', () => {
    it("allows purchasing the comped user's own tier", () => {
      const compedState: PricingState = { status: 'comped', tier: 'household_2' }
      renderButton(compedState, 'household_2', 'month')
      const btn = screen.getByRole('button')
      expect(btn).not.toBeDisabled()
      expect(btn.textContent).toContain('Subscribe')
    })

    it('shows an enabled Subscribe CTA for tiers other than the comped tier', () => {
      const compedState: PricingState = { status: 'comped', tier: 'premium' }
      renderButton(compedState, 'household_2', 'month')
      const btn = screen.getByRole('button')
      expect(btn).not.toBeDisabled()
      expect(btn.textContent).toContain('Subscribe')
    })

    it('commerce stays functional: clicking Subscribe on a non-comped tier still checks out', () => {
      const compedState: PricingState = {
        status: 'comped',
        tier: 'premium',
        compEndsAt: '2026-08-01T00:00:00Z',
      }
      renderButton(compedState, 'household_2', 'month')
      fireEvent.click(screen.getByRole('button'))
      expect(mockStartCheckout).toHaveBeenCalled()
    })
  })
})
