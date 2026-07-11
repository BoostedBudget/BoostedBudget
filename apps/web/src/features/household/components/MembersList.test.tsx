/**
 * MembersList component tests
 * Issue #1370: Household feature — members roster (Task 4b)
 * TDD RED: written before implementation.
 *
 * Uses the REAL `useHouseholdMembers` and `useHousehold` hooks, driven
 * through the centralized api-client mocks in `@/test/mocks/household-api`
 * (preloaded via bunfig.toml) — same approach as HouseholdCard.test.tsx /
 * LeaveHouseholdDialog.test.tsx.
 *
 * Date assertions deliberately avoid pinning an exact calendar day: joined_at
 * is formatted in the viewer's LOCAL timezone (unlike InvitePanel's
 * `expires_at`, which is a UTC-midnight cutoff — see MembersList.tsx), so a
 * UTC midnight fixture can render as the previous day in a negative-offset
 * host timezone (e.g. this repo's CI/dev boxes). Asserting a date-shaped
 * string (rather than a specific day) keeps the test host-timezone-agnostic.
 */
import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import type { Household, HouseholdEntitlementSummary, HouseholdMemberView } from '../types'
import type { GetMyHouseholdResponseBody } from '@boostedbudget/api-client/models'
import {
  mockUseListHouseholdMembers,
  mockUseRemoveHouseholdMember,
  mockRefetchHouseholdMembers,
  mockRemoveHouseholdMemberMutateAsync,
  mockUseGetMyHousehold,
  mockUseGetHouseholdEntitlement,
  mockRefetchMyHousehold,
  mockRefetchHouseholdEntitlement,
} from '@/test/mocks/household-api'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import { MembersList } from './MembersList'

const mockHousehold: Household = {
  id: 'household-1',
  name: 'The Smiths',
  owner_user_id: 'owner-1',
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-01T00:00:00Z',
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

function setEntitlement(entitlement: HouseholdEntitlementSummary) {
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

const mockMembers: HouseholdMemberView[] = [
  {
    user_id: 'owner-1',
    display_name: 'Alex Owner',
    role: 'owner',
    status: 'active',
    joined_at: '2025-01-01T00:00:00Z',
  },
  {
    user_id: 'member-1',
    display_name: 'Jamie Member',
    role: 'member',
    status: 'active',
    joined_at: '2025-02-15T00:00:00Z',
  },
]

function setMembers(members: HouseholdMemberView[]) {
  mockUseListHouseholdMembers.mockReturnValue({
    data: members,
    isLoading: false,
    error: null,
    refetch: mockRefetchHouseholdMembers,
  })
}

function renderList() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
  return render(<MembersList />, { wrapper: Wrapper })
}

describe('MembersList', () => {
  beforeEach(() => {
    mockUseListHouseholdMembers.mockClear()
    mockUseRemoveHouseholdMember.mockClear()
    mockRefetchHouseholdMembers.mockClear()
    mockRemoveHouseholdMemberMutateAsync.mockClear()
    mockUseGetMyHousehold.mockClear()
    mockUseGetHouseholdEntitlement.mockClear()
    mockRefetchMyHousehold.mockClear()
    mockRefetchHouseholdEntitlement.mockClear()
    mockTrackEvent.mockClear()

    setMembers(mockMembers)
    setEntitlement(makeEntitlement())
    mockUseRemoveHouseholdMember.mockReturnValue({
      mutateAsync: mockRemoveHouseholdMemberMutateAsync,
      isPending: false,
    })
  })

  it('renders all members with name and role', () => {
    renderList()

    expect(screen.getByText('Alex Owner')).toBeInTheDocument()
    expect(screen.getByText('Jamie Member')).toBeInTheDocument()
    expect(screen.getByText('Owner')).toBeInTheDocument()
    expect(screen.getByText('Member')).toBeInTheDocument()
  })

  it('renders joined dates in human-readable form, not the raw ISO string', () => {
    renderList()

    const dateMatches = screen.getAllByText(/^[A-Z][a-z]{2} \d{1,2}, \d{4}$/)
    expect(dateMatches).toHaveLength(mockMembers.length)
    expect(screen.queryByText('2025-01-01T00:00:00Z')).not.toBeInTheDocument()
  })

  it('renders avatar fallback initials derived from display_name', () => {
    renderList()

    expect(screen.getByText('AO')).toBeInTheDocument()
    expect(screen.getByText('JM')).toBeInTheDocument()
  })

  it('falls back to a placeholder identity when display_name is absent', () => {
    setMembers([
      { user_id: 'member-2', role: 'member', status: 'active', joined_at: '2025-03-01T00:00:00Z' },
    ])
    setEntitlement(makeEntitlement({ is_owner: false }))
    renderList()

    expect(screen.getByText('Household member')).toBeInTheDocument()
    expect(screen.getByText('?')).toBeInTheDocument()
  })

  it('shows a Remove action for the owner on non-self rows only', () => {
    renderList()

    const removeButtons = screen.getAllByRole('button', { name: /^remove$/i })
    expect(removeButtons).toHaveLength(1)
  })

  it('shows no Remove action at all for a non-owner viewer', () => {
    setEntitlement(makeEntitlement({ is_owner: false }))
    renderList()

    expect(screen.queryByRole('button', { name: /^remove$/i })).not.toBeInTheDocument()
  })

  it('confirming Remove calls removeMember with the correct user_id', async () => {
    mockRemoveHouseholdMemberMutateAsync.mockResolvedValueOnce(undefined)
    renderList()

    fireEvent.click(screen.getByRole('button', { name: /^remove$/i }))
    fireEvent.click(screen.getByRole('button', { name: /remove member/i }))

    await waitFor(() => {
      expect(mockRemoveHouseholdMemberMutateAsync).toHaveBeenCalledWith({ userId: 'member-1' })
    })
  })

  it('cancelling the confirmation dialog does not call removeMember', () => {
    renderList()

    fireEvent.click(screen.getByRole('button', { name: /^remove$/i }))
    fireEvent.click(screen.getByRole('button', { name: /^cancel$/i }))

    expect(mockRemoveHouseholdMemberMutateAsync).not.toHaveBeenCalled()
  })

  it('shows an error and keeps the dialog open when removal fails', async () => {
    mockRemoveHouseholdMemberMutateAsync.mockRejectedValueOnce(new Error('boom'))
    renderList()

    fireEvent.click(screen.getByRole('button', { name: /^remove$/i }))
    fireEvent.click(screen.getByRole('button', { name: /remove member/i }))

    expect(await screen.findByText(/could not remove this member/i)).toBeInTheDocument()
  })

  it('applies data-ph-mask to the identity column cells', () => {
    const { container } = renderList()
    const maskedCells = container.querySelectorAll('td[data-ph-mask]')
    expect(maskedCells).toHaveLength(mockMembers.length)
  })

  it('masks the member name inside the removal confirmation dialog description', () => {
    renderList()

    fireEvent.click(screen.getByRole('button', { name: /^remove$/i }))

    // The dialog renders via a Radix portal into `document.body`, outside
    // the `render()`-scoped container, so query the document directly.
    const maskedDialogText = document.body.querySelector('[role="alertdialog"] span[data-ph-mask]')
    expect(maskedDialogText).not.toBeNull()
    expect(maskedDialogText).toHaveTextContent('Jamie Member')
  })

  it('associates the Remove button with the member name via aria-describedby, never aria-label', () => {
    renderList()

    const removeButton = screen.getByRole('button', { name: /^remove$/i })
    const nameNode = screen.getByText('Jamie Member')

    expect(removeButton).toHaveAttribute('aria-describedby', nameNode.id)
    expect(removeButton).not.toHaveAttribute('aria-label')
  })

  describe('Analytics (#1370 Task 10)', () => {
    it('fires HOUSEHOLD_MEMBER_REMOVED after a successful removal', async () => {
      mockRemoveHouseholdMemberMutateAsync.mockResolvedValueOnce(undefined)
      renderList()

      fireEvent.click(screen.getByRole('button', { name: /^remove$/i }))
      fireEvent.click(screen.getByRole('button', { name: /remove member/i }))

      await waitFor(() => {
        expect(mockTrackEvent).toHaveBeenCalledWith(ANALYTICS_EVENTS.HOUSEHOLD_MEMBER_REMOVED)
      })
    })

    it('does NOT fire HOUSEHOLD_MEMBER_REMOVED when removal fails', async () => {
      mockRemoveHouseholdMemberMutateAsync.mockRejectedValueOnce(new Error('boom'))
      renderList()

      fireEvent.click(screen.getByRole('button', { name: /^remove$/i }))
      fireEvent.click(screen.getByRole('button', { name: /remove member/i }))

      await screen.findByText(/could not remove this member/i)
      expect(mockTrackEvent).not.toHaveBeenCalledWith(ANALYTICS_EVENTS.HOUSEHOLD_MEMBER_REMOVED)
    })
  })
})
