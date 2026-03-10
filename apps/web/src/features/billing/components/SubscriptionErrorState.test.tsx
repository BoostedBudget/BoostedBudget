import { describe, it, expect, mock } from 'bun:test'
import React from 'react'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { SubscriptionErrorState } from './SubscriptionErrorState'

describe('SubscriptionErrorState', () => {
  it('renders "Unable to load subscription" heading', () => {
    render(<SubscriptionErrorState message="Something went wrong" onRetry={() => {}} />)
    expect(screen.getByText('Unable to load subscription')).toBeDefined()
  })

  it('renders the provided error message', () => {
    render(<SubscriptionErrorState message="Please check your connection." onRetry={() => {}} />)
    expect(screen.getByText('Please check your connection.')).toBeDefined()
  })

  it('does not expose raw HTTP status codes in the message', () => {
    render(<SubscriptionErrorState message="Subscription service error. Please try again later." onRetry={() => {}} />)
    const { container } = render(
      <SubscriptionErrorState message="Subscription service error. Please try again later." onRetry={() => {}} />
    )
    expect(container.textContent).not.toContain('500')
    expect(container.textContent).not.toContain('404')
    expect(container.textContent).not.toContain('503')
  })

  it('renders a Try Again button', () => {
    render(<SubscriptionErrorState message="Error" onRetry={() => {}} />)
    expect(screen.getByRole('button', { name: /try again/i })).toBeDefined()
  })

  it('calls onRetry when Try Again button is clicked', async () => {
    const onRetry = mock(() => {})
    render(<SubscriptionErrorState message="Error" onRetry={onRetry} />)
    await userEvent.click(screen.getByRole('button', { name: /try again/i }))
    expect(onRetry).toHaveBeenCalledTimes(1)
  })

  it('has role="alert" for screen readers', () => {
    render(<SubscriptionErrorState message="Error" onRetry={() => {}} />)
    expect(screen.getByRole('alert')).toBeDefined()
  })

  it('renders RefreshCw icon with aria-hidden', () => {
    const { container } = render(<SubscriptionErrorState message="Error" onRetry={() => {}} />)
    const hiddenIcons = container.querySelectorAll('[aria-hidden="true"]')
    expect(hiddenIcons.length).toBeGreaterThan(0)
  })
})
