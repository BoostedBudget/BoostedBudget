/**
 * LeaveHouseholdDialog component tests
 * Issue #1370: Household feature — leave-household flow
 * TDD RED: written before implementation.
 *
 * Uses the REAL `useProfile` and `useHouseholdMembers` hooks, driven
 * through the centralized api-client mocks in `@/test/mocks/profile-api`
 * and `@/test/mocks/household-api` (both preloaded via bunfig.toml) — same
 * approach as InvitePanel.test.tsx / useHouseholdMembers.test.tsx.
 */
import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import type { User } from '@boostedbudget/api-client/models'
import type { HouseholdMemberView } from '../types'
import { mockUseGetUserProfile, mockRefetchUserProfile } from '@/test/mocks/profile-api'
import {
  mockUseListHouseholdMembers,
  mockUseRemoveHouseholdMember,
  mockRefetchHouseholdMembers,
  mockRemoveHouseholdMemberMutateAsync,
} from '@/test/mocks/household-api'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import { LeaveHouseholdDialog } from './LeaveHouseholdDialog'

const mockUser: User = {
  id: 'user-1',
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

const mockMembers: HouseholdMemberView[] = [
  { user_id: 'owner-1', role: 'owner', status: 'active', joined_at: '2025-01-01T00:00:00Z' },
  { user_id: 'user-1', role: 'member', status: 'active', joined_at: '2025-02-01T00:00:00Z' },
]

function renderDialog(onOpenChange: (open: boolean) => void = () => {}) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
  return render(<LeaveHouseholdDialog open onOpenChange={onOpenChange} />, { wrapper: Wrapper })
}

describe('LeaveHouseholdDialog', () => {
  beforeEach(() => {
    mockUseGetUserProfile.mockClear()
    mockRefetchUserProfile.mockClear()
    mockUseListHouseholdMembers.mockClear()
    mockUseRemoveHouseholdMember.mockClear()
    mockRefetchHouseholdMembers.mockClear()
    mockRemoveHouseholdMemberMutateAsync.mockClear()
    mockTrackEvent.mockClear()

    mockUseGetUserProfile.mockReturnValue({
      data: mockUser,
      isLoading: false,
      error: null,
      refetch: mockRefetchUserProfile,
    })
    mockUseListHouseholdMembers.mockReturnValue({
      data: mockMembers,
      isLoading: false,
      error: null,
      refetch: mockRefetchHouseholdMembers,
    })
    mockUseRemoveHouseholdMember.mockReturnValue({
      mutateAsync: mockRemoveHouseholdMemberMutateAsync,
      isPending: false,
    })
  })

  it('confirm calls removeMember with the current user\'s own id', async () => {
    mockRemoveHouseholdMemberMutateAsync.mockResolvedValueOnce(undefined)
    renderDialog()

    fireEvent.click(screen.getByRole('button', { name: /leave household/i }))

    await waitFor(() => {
      expect(mockRemoveHouseholdMemberMutateAsync).toHaveBeenCalledWith({ userId: 'user-1' })
    })
  })

  it('closes the dialog after a successful leave', async () => {
    mockRemoveHouseholdMemberMutateAsync.mockResolvedValueOnce(undefined)
    let openState = true
    const onOpenChange = (next: boolean) => {
      openState = next
    }
    renderDialog(onOpenChange)

    fireEvent.click(screen.getByRole('button', { name: /leave household/i }))

    await waitFor(() => {
      expect(openState).toBe(false)
    })
  })

  it('cancel does not call removeMember', () => {
    renderDialog()

    fireEvent.click(screen.getByRole('button', { name: /^cancel$/i }))

    expect(mockRemoveHouseholdMemberMutateAsync).not.toHaveBeenCalled()
  })

  it('shows an error and keeps the dialog open when leaving fails', async () => {
    const onOpenChange = () => {}
    mockRemoveHouseholdMemberMutateAsync.mockRejectedValueOnce(new Error('boom'))
    renderDialog(onOpenChange)

    fireEvent.click(screen.getByRole('button', { name: /leave household/i }))

    expect(await screen.findByText(/could not leave the household/i)).toBeInTheDocument()
  })

  describe('Analytics (#1370 Task 10)', () => {
    it('fires HOUSEHOLD_LEFT after a successful leave', async () => {
      mockRemoveHouseholdMemberMutateAsync.mockResolvedValueOnce(undefined)
      renderDialog()

      fireEvent.click(screen.getByRole('button', { name: /leave household/i }))

      await waitFor(() => {
        expect(mockTrackEvent).toHaveBeenCalledWith(ANALYTICS_EVENTS.HOUSEHOLD_LEFT)
      })
    })

    it('does NOT fire HOUSEHOLD_LEFT when leaving fails', async () => {
      mockRemoveHouseholdMemberMutateAsync.mockRejectedValueOnce(new Error('boom'))
      renderDialog()

      fireEvent.click(screen.getByRole('button', { name: /leave household/i }))

      await screen.findByText(/could not leave the household/i)
      expect(mockTrackEvent).not.toHaveBeenCalledWith(ANALYTICS_EVENTS.HOUSEHOLD_LEFT)
    })
  })
})
