/**
 * SubscriptionManagementPage Tests
 * Issue #843: Polished loading/error states for billing page
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { SubscriptionManagementPage } from './SubscriptionManagementPage'
import {
  mockUseGetCurrentSubscription,
  mockUseGetSubscriptionTiers,
  mockUseUpdatePaddleSubscription,
  mockUseCancelPaddleSubscription,
  mockUseGetBillingPortalUrl,
  mockUpdateSubscriptionMutateAsync,
  mockCancelSubscriptionMutateAsync,
} from '@/test/mocks/subscription-api'
import { mockSubscriptionTiersResponse } from '@/test/fixtures/subscriptionFixtures'

const defaultMutationState = {
  isPending: false,
  isSuccess: false,
  isError: false,
  error: null,
  reset: () => {},
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/billing/manage']}>
      <Routes>
        <Route path="/billing/manage" element={<SubscriptionManagementPage />} />
        <Route path="/pricing" element={<div data-testid="pricing-page">Pricing Page</div>} />
      </Routes>
    </MemoryRouter>
  )
}

describe('SubscriptionManagementPage', () => {
  beforeEach(() => {
    // Default: loading state
    mockUseGetCurrentSubscription.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve({ data: undefined, error: null }),
    })
    mockUseGetSubscriptionTiers.mockReturnValue({
      data: { tiers: mockSubscriptionTiersResponse.tiers },
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })
    mockUseUpdatePaddleSubscription.mockReturnValue({
      mutateAsync: mockUpdateSubscriptionMutateAsync,
      ...defaultMutationState,
    })
    mockUseCancelPaddleSubscription.mockReturnValue({
      mutateAsync: mockCancelSubscriptionMutateAsync,
      ...defaultMutationState,
    })
    mockUseGetBillingPortalUrl.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })
    mockUpdateSubscriptionMutateAsync.mockClear()
    mockCancelSubscriptionMutateAsync.mockClear()
  })

  describe('Loading state', () => {
    it('renders skeleton with role="status" when subscription is loading', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: undefined,
        isLoading: true,
        error: null,
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      renderPage()

      expect(screen.getByRole('status')).toBeDefined()
    })

    it('renders skeleton when tiers are loading', () => {
      mockUseGetSubscriptionTiers.mockReturnValue({
        data: undefined,
        isLoading: true,
        error: null,
        refetch: () => Promise.resolve(),
      })

      renderPage()

      expect(screen.getByRole('status')).toBeDefined()
    })

    it('does not render error state when loading', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: undefined,
        isLoading: true,
        error: null,
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      renderPage()

      expect(screen.queryByRole('alert')).toBeNull()
    })
  })

  describe('Error state (non-404)', () => {
    it('renders error alert for non-404 errors', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: { status: 500, message: 'Server error' },
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      renderPage()

      expect(screen.getByRole('alert')).toBeDefined()
    })

    it('shows "Unable to load subscription" heading for errors', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: { status: 500, message: 'Server error' },
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      renderPage()

      expect(screen.getByText('Unable to load subscription')).toBeDefined()
    })

    it('does not expose raw HTTP status codes', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: { status: 500, message: 'Internal Server Error' },
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      const { container } = renderPage()

      expect(container.textContent).not.toContain('500')
      expect(container.textContent).not.toContain('Internal Server Error')
    })

    it('renders Try Again button on error', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: { status: 503, message: 'Service unavailable' },
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      renderPage()

      expect(screen.getByRole('button', { name: /try again/i })).toBeDefined()
    })
  })

  describe('404 / No subscription state', () => {
    it('renders no-subscription empty state when 404', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: { status: 404, message: 'Not Found' },
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      renderPage()

      expect(screen.getByText('No Active Subscription')).toBeDefined()
    })

    it('does not render error alert for 404', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: { status: 404, message: 'Not Found' },
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      renderPage()

      expect(screen.queryByRole('alert')).toBeNull()
    })

    it('renders "View Plans" CTA for 404', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: { status: 404, message: 'Not Found' },
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      renderPage()

      expect(screen.getByRole('button', { name: /view plans/i })).toBeDefined()
    })

    it('does not expose raw "404" text for 404 error', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: { status: 404, message: 'Not Found' },
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      const { container } = renderPage()

      expect(container.textContent).not.toContain('404')
    })

    it('renders empty state when subscription data is null/undefined', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: null,
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      renderPage()

      expect(screen.getByText('No Active Subscription')).toBeDefined()
    })
  })

  describe('Active subscription', () => {
    it('renders subscription details for active status', () => {
      mockUseGetCurrentSubscription.mockReturnValue({
        data: {
          status: 'active',
          tier: 'premium',
          amount: '8.99',
          currency: 'USD',
          billing_interval: 'month',
          next_billing_date: '2026-04-09',
        },
        isLoading: false,
        error: null,
        refetch: () => Promise.resolve({ data: undefined, error: null }),
      })

      renderPage()

      expect(screen.queryByRole('status')).toBeNull()
      expect(screen.queryByRole('alert')).toBeNull()
      expect(screen.getByText('Manage Subscription')).toBeDefined()
    })
  })
})
