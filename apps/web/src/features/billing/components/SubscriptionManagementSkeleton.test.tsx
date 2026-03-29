import { describe, it, expect } from 'bun:test'
import React from 'react'
import { render, screen } from '@testing-library/react'
import { SubscriptionManagementSkeleton } from './SubscriptionManagementSkeleton'

describe('SubscriptionManagementSkeleton', () => {
  it('renders with role="status" for screen readers', () => {
    render(<SubscriptionManagementSkeleton />)
    expect(screen.getByRole('status')).toBeDefined()
  })

  it('renders with loading aria-label', () => {
    render(<SubscriptionManagementSkeleton />)
    const status = screen.getByRole('status')
    expect(status.getAttribute('aria-label')).toBe('Loading subscription details')
  })

  it('renders with aria-busy="true"', () => {
    render(<SubscriptionManagementSkeleton />)
    const status = screen.getByRole('status')
    expect(status.getAttribute('aria-busy')).toBe('true')
  })

  it('renders skeleton pulse blocks', () => {
    const { container } = render(<SubscriptionManagementSkeleton />)
    const pulseElements = container.querySelectorAll('.animate-pulse')
    expect(pulseElements.length).toBeGreaterThan(3)
  })

  it('renders subscription card skeleton section', () => {
    const { container } = render(<SubscriptionManagementSkeleton />)
    const subscriptionCard = container.querySelector('[data-testid="skeleton-subscription-card"]')
    expect(subscriptionCard).not.toBeNull()
  })

  it('renders billing details skeleton section', () => {
    const { container } = render(<SubscriptionManagementSkeleton />)
    const billingCard = container.querySelector('[data-testid="skeleton-billing-card"]')
    expect(billingCard).not.toBeNull()
  })
})
