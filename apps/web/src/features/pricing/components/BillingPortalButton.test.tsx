import { describe, it, expect, beforeEach, mock } from 'bun:test'
import React from 'react'
import { render, screen, fireEvent } from '@testing-library/react'
import { BillingPortalButton } from './BillingPortalButton'

const mockWindowOpen = mock(() => null)

beforeEach(() => {
  mockWindowOpen.mockClear()
  Object.defineProperty(window, 'open', { value: mockWindowOpen, writable: true })
})

describe('BillingPortalButton', () => {
  it('renders the label prop as button text', () => {
    render(
      <BillingPortalButton
        billingPortalUrl="https://portal.paddle.com/123"
        isLoadingPortal={false}
        label="Resubscribe"
      />
    )
    expect(screen.getByRole('button', { name: /Resubscribe/i })).toBeDefined()
  })

  it('renders a different label when provided', () => {
    render(
      <BillingPortalButton
        billingPortalUrl="https://portal.paddle.com/123"
        isLoadingPortal={false}
        label="Update Payment Method"
      />
    )
    expect(screen.getByRole('button', { name: /Update Payment Method/i })).toBeDefined()
  })

  it('opens billing portal in new tab on click', () => {
    render(
      <BillingPortalButton
        billingPortalUrl="https://portal.paddle.com/123"
        isLoadingPortal={false}
        label="Resubscribe"
      />
    )
    fireEvent.click(screen.getByRole('button', { name: /Resubscribe/i }))
    expect(mockWindowOpen).toHaveBeenCalledWith(
      'https://portal.paddle.com/123',
      '_blank',
      'noopener,noreferrer'
    )
  })

  it('shows loading state and is disabled when portal is loading', () => {
    render(
      <BillingPortalButton
        billingPortalUrl={undefined}
        isLoadingPortal={true}
        label="Resubscribe"
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
        label="Resubscribe"
      />
    )
    expect(screen.getByRole('button')).toBeDisabled()
  })

  it('does not open portal when URL is absent', () => {
    render(
      <BillingPortalButton
        billingPortalUrl={undefined}
        isLoadingPortal={false}
        label="Resubscribe"
      />
    )
    fireEvent.click(screen.getByRole('button'))
    expect(mockWindowOpen).not.toHaveBeenCalled()
  })
})
