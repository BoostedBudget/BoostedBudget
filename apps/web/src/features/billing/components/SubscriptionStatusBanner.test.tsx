import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { SubscriptionStatusBanner } from './SubscriptionStatusBanner'
import { bannerCtaVariants } from '@/shared/components/bannerVariants'
import { mockUseGetUserProfile } from '@/test/mocks/profile-api'
import { mockUseGetBillingPortalUrl } from '@/test/mocks/subscription-api'
import { mockTrackEvent } from '@/test/mocks/track-event'
import {
  mockUseSessionDismiss,
  mockDismiss,
} from '@/test/mocks/session-dismiss'
import type { User } from '@boostedbudget/api-client/models'

const baseUser: User = {
  id: 'user-123',
  clerk_user_id: 'user_123',
  email: 'test@example.com',
  email_verified: true,
  primary_region: 'iad',
  subscription_tier: 'premium',
  subscription_status: 'active',
  is_active: true,
  is_deleted: false,
  had_trial: false,
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-15T10:00:00Z',
  timezone: 'UTC',
  timezone_source: 'auto',
}

function makeUser(overrides: Partial<User>): User {
  return { ...baseUser, ...overrides }
}

describe('SubscriptionStatusBanner', () => {
  beforeEach(() => {
    mockUseGetUserProfile.mockClear()
    mockUseGetBillingPortalUrl.mockClear()
    mockTrackEvent.mockClear()
    mockDismiss.mockClear()
    mockUseSessionDismiss.mockClear()

    // Defaults
    mockUseGetBillingPortalUrl.mockReturnValue({
      data: { portal_url: 'https://billing.paddle.com/portal' },
      isLoading: false,
      error: null,
    })
    mockUseSessionDismiss.mockReturnValue({ isDismissed: false, dismiss: mockDismiss })
  })

  it('returns null for active status', () => {
    mockUseGetUserProfile.mockReturnValue({
      data: makeUser({ subscription_status: 'active' }),
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })
    const { container } = render(<SubscriptionStatusBanner />)
    expect(container.firstChild).toBeNull()
  })

  it('returns null for trialing status', () => {
    mockUseGetUserProfile.mockReturnValue({
      data: makeUser({ subscription_status: 'trialing' }),
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })
    const { container } = render(<SubscriptionStatusBanner />)
    expect(container.firstChild).toBeNull()
  })

  it('renders past_due warning banner with message', () => {
    mockUseGetUserProfile.mockReturnValue({
      data: makeUser({ subscription_status: 'past_due' }),
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })
    render(<SubscriptionStatusBanner />)
    expect(screen.getByRole('alert')).toBeDefined()
    expect(screen.getByText(/Your payment didn't go through/i)).toBeDefined()
    expect(screen.getByText(/Update payment method/i)).toBeDefined()
  })

  it('renders cancelled destructive banner with retention date', () => {
    mockUseGetUserProfile.mockReturnValue({
      data: makeUser({
        subscription_status: 'cancelled',
        retention_expires_at: '2025-03-15T00:00:00Z',
      }),
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })
    render(<SubscriptionStatusBanner />)
    expect(screen.getByRole('alert')).toBeDefined()
    expect(screen.getByText(/Your subscription has been cancelled/i)).toBeDefined()
    expect(screen.getByText(/2025-03-15/i)).toBeDefined()
    expect(screen.getByText(/Resubscribe/i)).toBeDefined()
  })

  it('renders cancelled banner without date when retention_expires_at is null', () => {
    mockUseGetUserProfile.mockReturnValue({
      data: makeUser({
        subscription_status: 'cancelled',
        retention_expires_at: undefined,
      }),
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })
    render(<SubscriptionStatusBanner />)
    expect(screen.getByText(/Your subscription has been cancelled/i)).toBeDefined()
    // Should not contain "Access ends" clause
    expect(screen.queryByText(/Access ends/i)).toBeNull()
  })

  it('renders inactive banner', () => {
    mockUseGetUserProfile.mockReturnValue({
      data: makeUser({ subscription_status: 'inactive' }),
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })
    render(<SubscriptionStatusBanner />)
    expect(screen.getByRole('alert')).toBeDefined()
    expect(screen.getByText(/Your subscription has expired/i)).toBeDefined()
    expect(screen.getByText(/Resubscribe/i)).toBeDefined()
  })

  // GAP1 regression: inactive must use semantic info token (bg-info/10), not a raw palette class.
  // Both cancelled and inactive CTAs must use the same label spelling ("Resubscribe").
  it('inactive banner uses bg-info/10 semantic token, not raw palette blue', () => {
    mockUseGetUserProfile.mockReturnValue({
      data: makeUser({ subscription_status: 'inactive' }),
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })
    render(<SubscriptionStatusBanner />)
    const alert = screen.getByRole('alert')
    expect(alert.className).toContain('bg-info/10')
    // Must not fall back to a raw palette literal (e.g. the old blue-500 class).
    const rawBluePalette = ['blue', '500'].join('-')
    expect(alert.className).not.toContain('bg-' + rawBluePalette)
    expect(alert.className).toContain('border-info/30')
  })

  it('past_due banner uses bg-warning/10 semantic token', () => {
    mockUseGetUserProfile.mockReturnValue({
      data: makeUser({ subscription_status: 'past_due' }),
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })
    render(<SubscriptionStatusBanner />)
    const alert = screen.getByRole('alert')
    expect(alert.className).toContain('bg-warning/10')
    expect(alert.className).toContain('border-warning/30')
  })

  it('cancelled banner uses bg-destructive/10 semantic token', () => {
    mockUseGetUserProfile.mockReturnValue({
      data: makeUser({ subscription_status: 'cancelled' }),
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })
    render(<SubscriptionStatusBanner />)
    const alert = screen.getByRole('alert')
    expect(alert.className).toContain('bg-destructive/10')
    expect(alert.className).toContain('border-destructive/30')
  })

  it('inactive banner resubscribe CTA uses the shared bannerCtaVariants primary variant', () => {
    mockUseGetUserProfile.mockReturnValue({
      data: makeUser({ subscription_status: 'inactive' }),
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })
    render(<SubscriptionStatusBanner />)
    const cta = screen.getByText('Resubscribe').closest('a')
    expect(cta?.className).toBe(bannerCtaVariants({ variant: 'primary' }))
  })

  it('CTA label is Resubscribe for both cancelled and inactive (consistent spelling)', () => {
    for (const status of ['cancelled', 'inactive'] as const) {
      mockUseGetUserProfile.mockReturnValue({
        data: makeUser({ subscription_status: status }),
        isLoading: false,
        error: null,
        refetch: () => Promise.resolve(),
      })
      const { unmount } = render(<SubscriptionStatusBanner />)
      expect(screen.getByText('Resubscribe')).toBeDefined()
      unmount()
    }
  })

  it('past_due banner is dismissible, hides after dismiss', () => {
    mockUseGetUserProfile.mockReturnValue({
      data: makeUser({ subscription_status: 'past_due' }),
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })
    mockUseSessionDismiss.mockReturnValue({ isDismissed: false, dismiss: mockDismiss })
    render(<SubscriptionStatusBanner />)

    const dismissBtn = screen.getByRole('button', { name: /Dismiss banner/i })
    expect(dismissBtn).toBeDefined()
    fireEvent.click(dismissBtn)
    expect(mockDismiss).toHaveBeenCalledTimes(1)
  })

  it('past_due banner hidden when already dismissed', () => {
    mockUseGetUserProfile.mockReturnValue({
      data: makeUser({ subscription_status: 'past_due' }),
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })
    mockUseSessionDismiss.mockReturnValue({ isDismissed: true, dismiss: mockDismiss })
    const { container } = render(<SubscriptionStatusBanner />)
    expect(container.firstChild).toBeNull()
  })

  it('cancelled banner is NOT dismissible', () => {
    mockUseGetUserProfile.mockReturnValue({
      data: makeUser({ subscription_status: 'cancelled' }),
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })
    render(<SubscriptionStatusBanner />)
    expect(screen.queryByRole('button', { name: /Dismiss banner/i })).toBeNull()
  })

  it('fires SUBSCRIPTION_BANNER_VIEWED on mount', () => {
    mockUseGetUserProfile.mockReturnValue({
      data: makeUser({ subscription_status: 'past_due' }),
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })
    render(<SubscriptionStatusBanner />)
    expect(mockTrackEvent).toHaveBeenCalledWith('subscription_banner_viewed', expect.any(Object))
  })

  it('fires SUBSCRIPTION_BANNER_CTA_CLICKED on CTA click', () => {
    mockUseGetUserProfile.mockReturnValue({
      data: makeUser({ subscription_status: 'inactive' }),
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })
    render(<SubscriptionStatusBanner />)
    const ctaLink = screen.getByText(/Resubscribe/i).closest('a') ?? screen.getByText(/Resubscribe/i)
    fireEvent.click(ctaLink)
    expect(mockTrackEvent).toHaveBeenCalledWith(
      'subscription_banner_cta_clicked',
      expect.objectContaining({ status: 'inactive' })
    )
  })

  it('fires SUBSCRIPTION_BANNER_DISMISSED on dismiss', () => {
    mockUseGetUserProfile.mockReturnValue({
      data: makeUser({ subscription_status: 'past_due' }),
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })
    mockUseSessionDismiss.mockReturnValue({ isDismissed: false, dismiss: mockDismiss })
    render(<SubscriptionStatusBanner />)
    const dismissBtn = screen.getByRole('button', { name: /Dismiss banner/i })
    fireEvent.click(dismissBtn)
    expect(mockTrackEvent).toHaveBeenCalledWith(
      'subscription_banner_dismissed',
      expect.objectContaining({ status: 'past_due' })
    )
  })
})
