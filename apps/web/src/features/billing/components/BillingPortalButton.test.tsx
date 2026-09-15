import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { BillingPortalButton } from './BillingPortalButton'

describe('BillingPortalButton', () => {
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
})
