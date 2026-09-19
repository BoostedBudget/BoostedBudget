import { beforeEach, describe, it, expect } from 'bun:test'
import { fireEvent, render, screen } from '@testing-library/react'
import { ANALYTICS_EVENTS, normalizeAnalyticsEventPayload } from '@/shared/analytics'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { BillingPortalButton } from './BillingPortalButton'

describe('BillingPortalButton', () => {
  beforeEach(() => {
    mockTrackEvent.mockClear()
  })

  it('defaults to the single Manage subscription action', () => {
    render(
      <BillingPortalButton
        billingPortalUrl="https://portal.paddle.com/123"
        isLoadingPortal={false}
      />
    )
    expect(screen.getByRole('link', { name: /Manage subscription/i })).toBeDefined()
  })

  it('renders a different label when provided', () => {
    render(
      <BillingPortalButton
        billingPortalUrl="https://portal.paddle.com/123"
        isLoadingPortal={false}
        label="Update Payment Method"
      />
    )
    expect(screen.getByRole('link', { name: /Update Payment Method/i })).toBeDefined()
  })

  it('opens the resolved billing portal in a new tab', () => {
    render(
      <BillingPortalButton
        billingPortalUrl="https://portal.paddle.com/123"
        isLoadingPortal={false}
      />
    )
    const link = screen.getByRole('link', { name: /Manage subscription/i })
    expect(link).toHaveAttribute('href', 'https://portal.paddle.com/123')
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
  })

  it('shows loading state and is disabled when portal is loading', () => {
    render(
      <BillingPortalButton
        billingPortalUrl={undefined}
        isLoadingPortal={true}
      />
    )
    expect(screen.getByRole('button')).toBeDisabled()
    expect(screen.getByRole('button').textContent).toBe('Loading...')
  })

  it('is disabled when no portal URL and not loading', () => {
    render(
      <BillingPortalButton
        billingPortalUrl={undefined}
        isLoadingPortal={false}
      />
    )
    expect(screen.getByRole('button')).toBeDisabled()
  })

  it('shows an error state instead of a dead button when the portal URL fails to resolve', () => {
    render(
      <BillingPortalButton
        billingPortalUrl={undefined}
        isLoadingPortal={false}
        error={{ status: 503, message: 'Service Unavailable' }}
      />
    )
    expect(screen.getByRole('alert')).toHaveTextContent(/couldn't load the billing portal/i)
    expect(screen.queryByRole('button')).toBeNull()
    expect(screen.queryByRole('link')).toBeNull()
  })

  // #1652 todo 11 (D2): `subscription_managed` is a staging catalog event whose only call site
  // used to be the /billing/manage page MOUNT (a page-view proxy). D2 requires it to fire on the
  // RevenueCat-resolved management-link ACTION, so the fire moved here — the single self-service
  // action both the billing page and the pricing tier cards render.
  it('fires subscription_managed when the resolved management link is actioned', () => {
    render(
      <BillingPortalButton
        billingPortalUrl="https://portal.paddle.com/123"
        isLoadingPortal={false}
      />
    )
    fireEvent.click(screen.getByRole('link', { name: /Manage subscription/i }))
    expect(mockTrackEvent).toHaveBeenCalledWith(ANALYTICS_EVENTS.SUBSCRIPTION_MANAGED)
    // The event must survive the REAL normalizer, not merely be handed to trackEvent. An
    // unreviewed-property event normalizes to `{}`; null would mean the event is silently dropped.
    const calls = mockTrackEvent.mock.calls as unknown as Array<[string, Record<string, unknown> | undefined]>
    const call = calls.find(c => c[0] === ANALYTICS_EVENTS.SUBSCRIPTION_MANAGED)
    expect(normalizeAnalyticsEventPayload(call![0], call![1])).not.toBeNull()
  })

  it('masks the management link so session replay cannot record its text', () => {
    render(
      <BillingPortalButton
        billingPortalUrl="https://portal.paddle.com/123"
        isLoadingPortal={false}
      />
    )
    expect(screen.getByRole('link', { name: /Manage subscription/i })).toHaveAttribute('data-ph-mask')
  })
})
