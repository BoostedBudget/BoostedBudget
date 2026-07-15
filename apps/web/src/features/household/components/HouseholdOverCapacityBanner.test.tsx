/**
 * HouseholdOverCapacityBanner component tests
 * Issue #1371 Task 10: Household offboarding & lifecycle — owner-facing
 * over-capacity banner.
 *
 * TDD RED: written before implementation. Mirrors
 * SubscriptionStatusBanner.test.tsx's structure — drives the REAL
 * `useHousehold` hook through the centralized household-api mocks, plus
 * the shared session-dismiss and track-event mocks.
 */
import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { HouseholdOverCapacityBanner } from './HouseholdOverCapacityBanner'
import { mockUseGetUserProfile } from '@/test/mocks/profile-api'
import {
  mockUseGetMyHousehold,
  mockUseGetHouseholdEntitlement,
  mockRefetchMyHousehold,
  mockRefetchHouseholdEntitlement,
} from '@/test/mocks/household-api'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { mockUseSessionDismiss, mockDismiss } from '@/test/mocks/session-dismiss'
import type { User } from '@boostedbudget/api-client/models'
import type { Household, HouseholdEntitlementSummary } from '../types'

const baseUser: User = {
  id: 'user-123',
  clerk_user_id: 'user_123',
  email: 'test@example.com',
  email_verified: true,
  primary_region: 'iad',
  subscription_tier: 'household_3',
  subscription_status: 'active',
  is_active: true,
  is_deleted: false,
  had_trial: false,
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-15T10:00:00Z',
  timezone: 'UTC',
  timezone_source: 'auto',
}

function makeUser(overrides: Partial<User> = {}): User {
  return { ...baseUser, ...overrides }
}

const baseHousehold: Household = {
  id: 'household-1',
  name: 'The Smiths',
  owner_user_id: 'user-123',
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-01T00:00:00Z',
}

function makeEntitlement(overrides: Partial<HouseholdEntitlementSummary> = {}): HouseholdEntitlementSummary {
  return {
    is_owner: true,
    seats_active: 5,
    seats_pending: 0,
    source: 'household',
    tier: 'household_3',
    users_included: 3,
    ...overrides,
  }
}

function renderBanner() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
  return render(<HouseholdOverCapacityBanner />, { wrapper: Wrapper })
}

function configureOverCapacityOwner() {
  mockUseGetUserProfile.mockReturnValue({
    data: makeUser(),
    isLoading: false,
    error: null,
    refetch: () => Promise.resolve(),
  })
  mockUseGetMyHousehold.mockReturnValue({
    data: {
      household: { ...baseHousehold, over_capacity_since: '2026-07-01T00:00:00Z' },
      summary: makeEntitlement(),
    },
    isLoading: false,
    error: null,
    refetch: mockRefetchMyHousehold,
  })
  mockUseGetHouseholdEntitlement.mockReturnValue({
    data: makeEntitlement(),
    isLoading: false,
    error: null,
    refetch: mockRefetchHouseholdEntitlement,
  })
}

describe('HouseholdOverCapacityBanner', () => {
  beforeEach(() => {
    mockUseGetUserProfile.mockClear()
    mockUseGetMyHousehold.mockClear()
    mockUseGetHouseholdEntitlement.mockClear()
    mockRefetchMyHousehold.mockClear()
    mockRefetchHouseholdEntitlement.mockClear()
    mockTrackEvent.mockClear()
    mockDismiss.mockClear()
    mockUseSessionDismiss.mockClear()

    mockUseSessionDismiss.mockReturnValue({ isDismissed: false, dismiss: mockDismiss })
    // Sensible not-over-capacity defaults; individual tests override.
    mockUseGetUserProfile.mockReturnValue({
      data: makeUser(),
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })
    mockUseGetMyHousehold.mockReturnValue({
      data: { household: baseHousehold, summary: makeEntitlement({ seats_active: 2 }) },
      isLoading: false,
      error: null,
      refetch: mockRefetchMyHousehold,
    })
    mockUseGetHouseholdEntitlement.mockReturnValue({
      data: makeEntitlement({ seats_active: 2 }),
      isLoading: false,
      error: null,
      refetch: mockRefetchHouseholdEntitlement,
    })
  })

  it('renders null when not over capacity', () => {
    const { container } = renderBanner()
    expect(container.firstChild).toBeNull()
  })

  it('renders null when the user is a member (not owner), even if over capacity', () => {
    mockUseGetMyHousehold.mockReturnValue({
      data: {
        household: { ...baseHousehold, over_capacity_since: '2026-07-01T00:00:00Z' },
        summary: makeEntitlement({ is_owner: false }),
      },
      isLoading: false,
      error: null,
      refetch: mockRefetchMyHousehold,
    })
    mockUseGetHouseholdEntitlement.mockReturnValue({
      data: makeEntitlement({ is_owner: false }),
      isLoading: false,
      error: null,
      refetch: mockRefetchHouseholdEntitlement,
    })
    const { container } = renderBanner()
    expect(container.firstChild).toBeNull()
  })

  it('renders with role="alert" and the correct message + N count when owner and over capacity', () => {
    configureOverCapacityOwner()
    renderBanner()
    expect(screen.getByRole('alert')).toBeDefined()
    // seats_active: 5, users_included: 3 -> overage of 2
    expect(
      screen.getByText(/Your household is over your plan's seat limit\. Remove 2 members or upgrade to avoid interruption\./i)
    ).toBeDefined()
  })

  it('includes a CTA link to the household tab', () => {
    configureOverCapacityOwner()
    renderBanner()
    const cta = screen.getByText(/Manage household/i).closest('a')
    expect(cta).not.toBeNull()
    expect(cta?.getAttribute('href')).toBe('/profile?tab=household')
  })

  it('is dismissible and hides after dismiss', () => {
    configureOverCapacityOwner()
    renderBanner()
    const dismissBtn = screen.getByRole('button', { name: /Dismiss banner/i })
    fireEvent.click(dismissBtn)
    expect(mockDismiss).toHaveBeenCalledTimes(1)
  })

  it('renders null when already dismissed', () => {
    configureOverCapacityOwner()
    mockUseSessionDismiss.mockReturnValue({ isDismissed: true, dismiss: mockDismiss })
    const { container } = renderBanner()
    expect(container.firstChild).toBeNull()
  })

  it('fires HOUSEHOLD_OVER_CAPACITY_BANNER_VIEWED on mount', () => {
    configureOverCapacityOwner()
    renderBanner()
    expect(mockTrackEvent).toHaveBeenCalledWith(
      'household_over_capacity_banner_viewed',
      expect.any(Object)
    )
  })

  it('does not fire HOUSEHOLD_OVER_CAPACITY_BANNER_VIEWED when not applicable', () => {
    renderBanner()
    expect(mockTrackEvent).not.toHaveBeenCalledWith(
      'household_over_capacity_banner_viewed',
      expect.any(Object)
    )
  })

  it('fires HOUSEHOLD_OVER_CAPACITY_BANNER_CTA_CLICKED on CTA click', () => {
    configureOverCapacityOwner()
    renderBanner()
    const cta = screen.getByText(/Manage household/i)
    fireEvent.click(cta)
    expect(mockTrackEvent).toHaveBeenCalledWith(
      'household_over_capacity_banner_cta_clicked',
      expect.any(Object)
    )
  })

  it('fires HOUSEHOLD_OVER_CAPACITY_BANNER_DISMISSED on dismiss', () => {
    configureOverCapacityOwner()
    renderBanner()
    const dismissBtn = screen.getByRole('button', { name: /Dismiss banner/i })
    fireEvent.click(dismissBtn)
    expect(mockTrackEvent).toHaveBeenCalledWith(
      'household_over_capacity_banner_dismissed',
      expect.any(Object)
    )
  })
})
