/**
 * HouseholdTab component tests
 * Issue #1370: Household feature — composing tab (Task 4b)
 * TDD RED: written before implementation.
 *
 * Composes the REAL `useHousehold` hook plus real child components
 * (CreateHouseholdForm, HouseholdCard, MembersList, InvitePanel,
 * LeaveHouseholdDialog, DisbandHouseholdDialog), all driven through the
 * centralized api-client mocks in `@/test/mocks/household-api` and
 * `@/test/mocks/profile-api` (both preloaded via bunfig.toml) — same
 * approach used throughout this feature's other composing tests
 * (HouseholdCard.test.tsx, LeaveHouseholdDialog.test.tsx).
 */
import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import type { Household, HouseholdEntitlementSummary } from './types'
import type { GetMyHouseholdResponseBody, User } from '@boostedbudget/api-client/models'
import {
  mockUseGetMyHousehold,
  mockUseGetHouseholdEntitlement,
  mockRefetchMyHousehold,
  mockRefetchHouseholdEntitlement,
  mockUseListHouseholdMembers,
  mockRefetchHouseholdMembers,
  mockUseRemoveHouseholdMember,
  mockRemoveHouseholdMemberMutateAsync,
  mockUseCreateHousehold,
  mockUseUpdateHouseholdName,
  mockUseDeleteHousehold,
  mockUseListHouseholdInvites,
  mockRefetchHouseholdInvites,
  mockUseCreateHouseholdInvite,
  mockUseRevokeHouseholdInvite,
} from '@/test/mocks/household-api'
import { mockUseGetUserProfile, mockRefetchUserProfile } from '@/test/mocks/profile-api'
import { HouseholdTab } from './HouseholdTab'

const mockHousehold: Household = {
  id: 'household-1',
  name: 'The Smiths',
  owner_user_id: 'owner-1',
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-01T00:00:00Z',
}

const mockUser: User = {
  id: 'owner-1',
  clerk_user_id: 'user_1',
  email: 'test@example.com',
  email_verified: true,
  primary_region: 'iad',
  subscription_tier: 'household_3',
  subscription_status: 'active',
  is_active: true,
  is_deleted: false,
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-01T00:00:00Z',
  last_login_at: '2025-01-01T00:00:00Z',
}

function makeEntitlement(overrides: Partial<HouseholdEntitlementSummary> = {}): HouseholdEntitlementSummary {
  return {
    is_owner: true,
    seats_active: 2,
    seats_pending: 0,
    source: 'household',
    tier: 'household_3',
    users_included: 3,
    ...overrides,
  }
}

function setNoHousehold() {
  mockUseGetMyHousehold.mockReturnValue({
    data: undefined,
    isLoading: false,
    error: { response: { status: 404 } },
    refetch: mockRefetchMyHousehold,
  })
  mockUseGetHouseholdEntitlement.mockReturnValue({
    data: makeEntitlement({ is_owner: false, seats_active: 0, users_included: 1, tier: 'premium' }),
    isLoading: false,
    error: null,
    refetch: mockRefetchHouseholdEntitlement,
  })
}

function setHousehold(entitlement: HouseholdEntitlementSummary) {
  const responseBody: GetMyHouseholdResponseBody = { household: mockHousehold, summary: entitlement }
  mockUseGetMyHousehold.mockReturnValue({
    data: responseBody,
    isLoading: false,
    error: null,
    refetch: mockRefetchMyHousehold,
  })
  mockUseGetHouseholdEntitlement.mockReturnValue({
    data: entitlement,
    isLoading: false,
    error: null,
    refetch: mockRefetchHouseholdEntitlement,
  })
}

function setLoading() {
  mockUseGetMyHousehold.mockReturnValue({
    data: undefined,
    isLoading: true,
    error: null,
    refetch: mockRefetchMyHousehold,
  })
  mockUseGetHouseholdEntitlement.mockReturnValue({
    data: undefined,
    isLoading: true,
    error: null,
    refetch: mockRefetchHouseholdEntitlement,
  })
}

function renderTab() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
  return render(<HouseholdTab />, { wrapper: Wrapper })
}

describe('HouseholdTab', () => {
  beforeEach(() => {
    mockUseGetMyHousehold.mockClear()
    mockUseGetHouseholdEntitlement.mockClear()
    mockRefetchMyHousehold.mockClear()
    mockRefetchHouseholdEntitlement.mockClear()
    mockUseListHouseholdMembers.mockClear()
    mockRefetchHouseholdMembers.mockClear()
    mockUseRemoveHouseholdMember.mockClear()
    mockRemoveHouseholdMemberMutateAsync.mockClear()
    mockUseCreateHousehold.mockClear()
    mockUseUpdateHouseholdName.mockClear()
    mockUseDeleteHousehold.mockClear()
    mockUseListHouseholdInvites.mockClear()
    mockRefetchHouseholdInvites.mockClear()
    mockUseCreateHouseholdInvite.mockClear()
    mockUseRevokeHouseholdInvite.mockClear()
    mockUseGetUserProfile.mockClear()
    mockRefetchUserProfile.mockClear()

    mockUseListHouseholdMembers.mockReturnValue({
      data: [
        { user_id: 'owner-1', display_name: 'Alex Owner', role: 'owner', status: 'active', joined_at: '2025-01-01T00:00:00Z' },
        { user_id: 'member-1', display_name: 'Jamie Member', role: 'member', status: 'active', joined_at: '2025-02-01T00:00:00Z' },
      ],
      isLoading: false,
      error: null,
      refetch: mockRefetchHouseholdMembers,
    })
    mockUseRemoveHouseholdMember.mockReturnValue({
      mutateAsync: mockRemoveHouseholdMemberMutateAsync,
      isPending: false,
    })
    mockUseCreateHousehold.mockReturnValue({ mutateAsync: () => Promise.resolve(undefined), isPending: false })
    mockUseUpdateHouseholdName.mockReturnValue({ mutateAsync: () => Promise.resolve(undefined), isPending: false })
    mockUseDeleteHousehold.mockReturnValue({ mutateAsync: () => Promise.resolve(undefined), isPending: false })
    mockUseListHouseholdInvites.mockReturnValue({
      data: [],
      isLoading: false,
      error: null,
      refetch: mockRefetchHouseholdInvites,
    })
    mockUseCreateHouseholdInvite.mockReturnValue({ mutateAsync: () => Promise.resolve(undefined), isPending: false })
    mockUseRevokeHouseholdInvite.mockReturnValue({ mutateAsync: () => Promise.resolve(undefined), isPending: false })
    mockUseGetUserProfile.mockReturnValue({
      data: mockUser,
      isLoading: false,
      error: null,
      refetch: mockRefetchUserProfile,
    })
  })

  it('shows a loading state while useHousehold is loading', () => {
    setLoading()
    renderTab()

    expect(screen.getByText(/loading/i)).toBeInTheDocument()
    expect(screen.queryByText('The Smiths')).not.toBeInTheDocument()
  })

  it('shows an error state with Retry (not CreateHouseholdForm or a misrendered household) when useHousehold errors', async () => {
    mockUseGetMyHousehold.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error('Network error'),
      refetch: mockRefetchMyHousehold,
    })
    mockUseGetHouseholdEntitlement.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
      refetch: mockRefetchHouseholdEntitlement,
    })
    renderTab()

    expect(screen.getByText('Network error')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: /create a household/i })).not.toBeInTheDocument()
    expect(screen.queryByText('The Smiths')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /retry/i }))
    await waitFor(() => {
      expect(mockRefetchMyHousehold).toHaveBeenCalled()
    })
  })

  it('shows CreateHouseholdForm when the caller is not in a household', () => {
    setNoHousehold()
    renderTab()

    expect(screen.getByRole('heading', { name: /create a household/i })).toBeInTheDocument()
    expect(screen.queryByText('The Smiths')).not.toBeInTheDocument()
  })

  it('shows HouseholdCard and MembersList when in a household', () => {
    setHousehold(makeEntitlement({ is_owner: false }))
    renderTab()

    expect(screen.getByText('The Smiths')).toBeInTheDocument()
    expect(screen.getByText('Alex Owner')).toBeInTheDocument()
    expect(screen.getByText('Jamie Member')).toBeInTheDocument()
  })

  it('shows InvitePanel for the owner when seats are available', () => {
    setHousehold(makeEntitlement({ is_owner: true, seats_active: 2, users_included: 3 }))
    renderTab()

    expect(screen.getByRole('heading', { name: /invite a household member/i })).toBeInTheDocument()
  })

  it('hides InvitePanel for the owner when seats are full', () => {
    setHousehold(makeEntitlement({ is_owner: true, seats_active: 3, users_included: 3 }))
    renderTab()

    expect(screen.queryByRole('heading', { name: /invite a household member/i })).not.toBeInTheDocument()
  })

  it('hides InvitePanel entirely for a non-owner, even with seats available', () => {
    setHousehold(makeEntitlement({ is_owner: false, seats_active: 1, users_included: 3 }))
    renderTab()

    expect(screen.queryByRole('heading', { name: /invite a household member/i })).not.toBeInTheDocument()
  })

  it('hides InvitePanel for the owner when seats are over-limit, without breaking the rest of the UI', () => {
    setHousehold(makeEntitlement({ is_owner: true, seats_active: 4, users_included: 3 }))
    renderTab()

    expect(screen.queryByRole('heading', { name: /invite a household member/i })).not.toBeInTheDocument()
    expect(screen.getByText('The Smiths')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /disband household/i })).toBeInTheDocument()
  })

  it('shows a Disband household button that opens DisbandHouseholdDialog for the owner', () => {
    setHousehold(makeEntitlement({ is_owner: true, seats_active: 1, users_included: 3 }))
    renderTab()

    expect(screen.queryByRole('button', { name: /leave household/i })).not.toBeInTheDocument()
    const disbandButton = screen.getByRole('button', { name: /disband household/i })
    fireEvent.click(disbandButton)

    expect(screen.getByRole('heading', { name: /disband household\?/i })).toBeInTheDocument()
  })

  it('shows a Leave household button that opens LeaveHouseholdDialog for a non-owner', () => {
    setHousehold(makeEntitlement({ is_owner: false }))
    renderTab()

    expect(screen.queryByRole('button', { name: /disband household/i })).not.toBeInTheDocument()
    const leaveButton = screen.getByRole('button', { name: /leave household/i })
    fireEvent.click(leaveButton)

    expect(screen.getByRole('heading', { name: /leave household\?/i })).toBeInTheDocument()
  })
})
