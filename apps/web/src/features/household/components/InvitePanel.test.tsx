/**
 * InvitePanel component tests
 * Issue #1370: Household feature — InvitePanel (email-bound invite link
 * generation + revoke/regenerate)
 * TDD RED: written before implementation.
 *
 * Uses the REAL `useHouseholdInvites` hook (composed here exactly like
 * `useHouseholdInvites.test.tsx` does) driven through the centralized
 * api-client mocks in `@/test/mocks/household-api` (preloaded via
 * bunfig.toml). This deliberately avoids a local `mock.module()` of
 * `useHouseholdInvites` itself — that hook has its own dedicated
 * real-implementation test file running in the same bun:test process, and
 * bun's `mock.module()` is process-global, so re-mocking the hook here would
 * clobber it there (see project memory: mock.module app hooks in broad test
 * files never — mock at the api-client level instead).
 *
 * The real `useCopyToClipboard` hook is also used unmocked; clipboard access
 * is stubbed per-test via `navigator.clipboard`, mirroring
 * `useCopyToClipboard.test.tsx`.
 */
import { describe, it, expect, beforeEach, afterEach, mock, spyOn } from 'bun:test'
import { render, screen, fireEvent, waitFor, within, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import type { HouseholdInvite } from '../types'
import {
  mockUseListHouseholdInvites,
  mockUseCreateHouseholdInvite,
  mockUseRevokeHouseholdInvite,
  mockRefetchHouseholdInvites,
  mockCreateHouseholdInviteMutateAsync,
  mockRevokeHouseholdInviteMutateAsync,
} from '@/test/mocks/household-api'
import { InvitePanel } from './InvitePanel'

function renderPanel() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
  return render(<InvitePanel />, { wrapper: Wrapper })
}

function makeInvite(overrides: Partial<HouseholdInvite> = {}): HouseholdInvite {
  return {
    id: 'invite-1',
    household_id: 'household-1',
    invitee_email: 'friend@example.com',
    inviter_user_id: 'user-1',
    status: 'pending',
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
    expires_at: '2025-01-08T00:00:00Z',
    ...overrides,
  }
}

/** Sets the invites list returned by the (mocked) list query for a test. */
function setInvites(invites: HouseholdInvite[]) {
  mockUseListHouseholdInvites.mockReturnValue({
    data: invites,
    isLoading: false,
    error: null,
    refetch: mockRefetchHouseholdInvites,
  })
}

describe('InvitePanel', () => {
  const originalClipboard = navigator.clipboard
  let mockWriteText: ReturnType<typeof mock>

  beforeEach(() => {
    mockUseListHouseholdInvites.mockClear()
    mockUseCreateHouseholdInvite.mockClear()
    mockUseRevokeHouseholdInvite.mockClear()
    mockRefetchHouseholdInvites.mockClear()
    mockCreateHouseholdInviteMutateAsync.mockClear()
    mockRevokeHouseholdInviteMutateAsync.mockClear()

    setInvites([])
    mockUseCreateHouseholdInvite.mockReturnValue({
      mutateAsync: mockCreateHouseholdInviteMutateAsync,
      isPending: false,
    })
    mockUseRevokeHouseholdInvite.mockReturnValue({
      mutateAsync: mockRevokeHouseholdInviteMutateAsync,
      isPending: false,
    })

    mockWriteText = mock(() => Promise.resolve(undefined))
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText: mockWriteText },
      writable: true,
      configurable: true,
    })
  })

  afterEach(() => {
    Object.defineProperty(navigator, 'clipboard', {
      value: originalClipboard,
      writable: true,
      configurable: true,
    })
  })

  describe('Generate section', () => {
    it('calls createInvite with the entered email and displays the returned link + copy button', async () => {
      mockCreateHouseholdInviteMutateAsync.mockResolvedValueOnce({
        invite: makeInvite(),
        invite_link: 'https://app.boostedbudget.com/invite/token-abc',
      })

      // Regression guard for the UTC-pinning fix in `formatExpiresAt`: spy on
      // `toLocaleDateString` (calling through to the real implementation, so
      // the rendered text below is unaffected) and assert it was invoked with
      // `timeZone: 'UTC'`. Asserting on the *rendered string* alone
      // ("Jan 8, 2025") would NOT catch a regression where someone removes
      // the `timeZone: 'UTC'` option: on a UTC CI runner (the typical
      // merge-gating environment), `expires_at`'s UTC-midnight instant
      // formats identically whether or not the option is present. Asserting
      // directly on the call options makes this test fail on ANY host
      // timezone if the option is ever dropped.
      const toLocaleDateStringSpy = spyOn(Date.prototype, 'toLocaleDateString')

      try {
        renderPanel()

        fireEvent.change(screen.getByLabelText(/email/i), { target: { value: 'new@example.com' } })
        fireEvent.click(screen.getByRole('button', { name: /generate link/i }))

        await waitFor(() => {
          expect(mockCreateHouseholdInviteMutateAsync).toHaveBeenCalledWith({
            data: { email: 'new@example.com' },
          })
        })

        expect(await screen.findByText(/token-abc/)).toBeInTheDocument()
        expect(screen.getByRole('button', { name: /^copy$/i })).toBeInTheDocument()
        // expires_at is formatted human-readably, not the raw ISO string
        expect(screen.queryByText('2025-01-08T00:00:00Z')).not.toBeInTheDocument()
        expect(screen.getByText(/Jan 8, 2025/)).toBeInTheDocument()

        expect(toLocaleDateStringSpy).toHaveBeenCalled()
        const [, options] = toLocaleDateStringSpy.mock.calls[0] as [unknown, Intl.DateTimeFormatOptions]
        expect(options).toMatchObject({ timeZone: 'UTC' })
      } finally {
        toLocaleDateStringSpy.mockRestore()
      }
    })

    it('shows "Copied!" on the copy button after copying the link', async () => {
      mockCreateHouseholdInviteMutateAsync.mockResolvedValueOnce({
        invite: makeInvite(),
        invite_link: 'https://app.boostedbudget.com/invite/token-abc',
      })

      renderPanel()
      fireEvent.change(screen.getByLabelText(/email/i), { target: { value: 'new@example.com' } })
      fireEvent.click(screen.getByRole('button', { name: /generate link/i }))

      const copyButton = await screen.findByRole('button', { name: /^copy$/i })
      fireEvent.click(copyButton)

      await waitFor(() => {
        expect(mockWriteText).toHaveBeenCalledWith('https://app.boostedbudget.com/invite/token-abc')
      })
      expect(await screen.findByRole('button', { name: /copied!/i })).toBeInTheDocument()
    })

    it('does not call createInvite when the email field is empty', () => {
      renderPanel()
      const button = screen.getByRole('button', { name: /generate link/i })
      expect(button).toBeDisabled()
      fireEvent.click(button)
      expect(mockCreateHouseholdInviteMutateAsync).not.toHaveBeenCalled()
    })

    it('disables Generate link while the create call is in flight (no double submission)', async () => {
      let resolveCreate: (value: { invite: HouseholdInvite; invite_link: string }) => void = () => {}
      const pending = new Promise<{ invite: HouseholdInvite; invite_link: string }>((resolve) => {
        resolveCreate = resolve
      })
      mockCreateHouseholdInviteMutateAsync.mockImplementationOnce(() => pending)

      renderPanel()
      fireEvent.change(screen.getByLabelText(/email/i), { target: { value: 'slow@example.com' } })
      const button = screen.getByRole('button', { name: /generate link/i })
      fireEvent.click(button)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /generating/i })).toBeDisabled()
      })

      // A second click while in flight must not fire a second create call.
      fireEvent.click(screen.getByRole('button', { name: /generating/i }))
      expect(mockCreateHouseholdInviteMutateAsync).toHaveBeenCalledTimes(1)

      resolveCreate({
        invite: makeInvite(),
        invite_link: 'https://app.boostedbudget.com/invite/token-abc',
      })

      // isGenerating resets once the call resolves — the button label reverts
      // (it may still be disabled because the email field was cleared on
      // success, which is expected: disabled-due-to-empty is a different
      // state than stuck-disabled-due-to-in-flight).
      await waitFor(() => {
        expect(screen.getByRole('button', { name: /^generate link$/i })).toBeInTheDocument()
      })
    })

    it('clears a previously displayed link when a new Generate attempt fails', async () => {
      mockCreateHouseholdInviteMutateAsync.mockResolvedValueOnce({
        invite: makeInvite(),
        invite_link: 'https://app.boostedbudget.com/invite/token-abc',
      })

      renderPanel()
      fireEvent.change(screen.getByLabelText(/email/i), { target: { value: 'first@example.com' } })
      fireEvent.click(screen.getByRole('button', { name: /generate link/i }))
      expect(await screen.findByText(/token-abc/)).toBeInTheDocument()

      mockCreateHouseholdInviteMutateAsync.mockRejectedValueOnce({
        response: { status: 409, data: { detail: 'A pending invite already exists for this email.' } },
      })
      fireEvent.change(screen.getByLabelText(/email/i), { target: { value: 'second@example.com' } })
      fireEvent.click(screen.getByRole('button', { name: /generate link/i }))

      expect(
        await screen.findByText('A pending invite already exists for this email.')
      ).toBeInTheDocument()
      // The stale link from the first (successful) attempt must not linger
      // next to the new error.
      expect(screen.queryByText(/token-abc/)).not.toBeInTheDocument()
    })

    it('disables Generate link while a row action (revoke/regenerate) is in flight', async () => {
      let resolveRevoke: () => void = () => {}
      const pending = new Promise<void>((resolve) => {
        resolveRevoke = resolve
      })
      mockRevokeHouseholdInviteMutateAsync.mockImplementationOnce(() => pending)
      setInvites([makeInvite({ id: 'invite-42', invitee_email: 'target@example.com' })])

      renderPanel()
      fireEvent.click(screen.getByRole('button', { name: /^revoke$/i }))

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /generate link/i })).toBeDisabled()
      })

      resolveRevoke()
      await waitFor(() => {
        expect(screen.getByRole('button', { name: /^revoke$/i })).not.toBeDisabled()
      })
    })
  })

  describe('Error states', () => {
    it('surfaces a friendly error for a duplicate pending invite (409)', async () => {
      mockCreateHouseholdInviteMutateAsync.mockRejectedValueOnce({
        response: {
          status: 409,
          data: { detail: 'A pending invite already exists for this email.' },
        },
      })

      renderPanel()
      fireEvent.change(screen.getByLabelText(/email/i), { target: { value: 'dup@example.com' } })
      fireEvent.click(screen.getByRole('button', { name: /generate link/i }))

      expect(
        await screen.findByText('A pending invite already exists for this email.')
      ).toBeInTheDocument()
    })

    it('surfaces a friendly error for seats-full (409)', async () => {
      mockCreateHouseholdInviteMutateAsync.mockRejectedValueOnce({
        response: {
          status: 409,
          data: { detail: 'No seats available on this plan.' },
        },
      })

      renderPanel()
      fireEvent.change(screen.getByLabelText(/email/i), { target: { value: 'noseat@example.com' } })
      fireEvent.click(screen.getByRole('button', { name: /generate link/i }))

      expect(await screen.findByText('No seats available on this plan.')).toBeInTheDocument()
    })

    it('renders nothing extra when there is no action error', () => {
      renderPanel()
      expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    })
  })

  describe('Pending invites list', () => {
    it('renders only pending invites with Revoke and Regenerate actions', () => {
      setInvites([
        makeInvite({ id: 'invite-1', invitee_email: 'pending@example.com', status: 'pending' }),
        makeInvite({ id: 'invite-2', invitee_email: 'accepted@example.com', status: 'accepted' }),
        makeInvite({ id: 'invite-3', invitee_email: 'expired@example.com', status: 'expired' }),
      ])

      renderPanel()

      expect(screen.getByText('pending@example.com')).toBeInTheDocument()
      expect(screen.queryByText('accepted@example.com')).not.toBeInTheDocument()
      expect(screen.queryByText('expired@example.com')).not.toBeInTheDocument()

      expect(screen.getByRole('button', { name: /^revoke$/i })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /^regenerate$/i })).toBeInTheDocument()
    })

    it('calls revokeInvite with the correct invite id', async () => {
      setInvites([makeInvite({ id: 'invite-42', invitee_email: 'target@example.com' })])
      mockRevokeHouseholdInviteMutateAsync.mockResolvedValueOnce(undefined)

      renderPanel()

      fireEvent.click(screen.getByRole('button', { name: /^revoke$/i }))

      await waitFor(() => {
        expect(mockRevokeHouseholdInviteMutateAsync).toHaveBeenCalledWith({ id: 'invite-42' })
      })
    })

    it('calls regenerateInvite (revoke-then-create via the hook) and displays the new link', async () => {
      setInvites([makeInvite({ id: 'invite-42', invitee_email: 'target@example.com' })])
      mockRevokeHouseholdInviteMutateAsync.mockResolvedValueOnce(undefined)
      mockCreateHouseholdInviteMutateAsync.mockResolvedValueOnce({
        invite: makeInvite({ id: 'invite-99' }),
        invite_link: 'https://app.boostedbudget.com/invite/token-def',
      })

      renderPanel()

      fireEvent.click(screen.getByRole('button', { name: /^regenerate$/i }))

      await waitFor(() => {
        expect(mockRevokeHouseholdInviteMutateAsync).toHaveBeenCalledWith({ id: 'invite-42' })
        expect(mockCreateHouseholdInviteMutateAsync).toHaveBeenCalledWith({
          data: { email: 'target@example.com' },
        })
      })

      expect(await screen.findByText(/token-def/)).toBeInTheDocument()
    })

    it('disables Revoke and Regenerate for a row while its action is in flight', async () => {
      let resolveRevoke: () => void = () => {}
      const pending = new Promise<void>((resolve) => {
        resolveRevoke = resolve
      })
      mockRevokeHouseholdInviteMutateAsync.mockImplementationOnce(() => pending)

      setInvites([makeInvite({ id: 'invite-42', invitee_email: 'target@example.com' })])

      renderPanel()

      const row = screen.getByText('target@example.com').closest('li') as HTMLElement
      fireEvent.click(within(row).getByRole('button', { name: /^revoke$/i }))

      // Button label switches to "Revoking…"/"Regenerate" mid-flight, so match
      // on the stem rather than the exact idle-state label.
      await waitFor(() => {
        expect(within(row).getByRole('button', { name: /revok/i })).toBeDisabled()
        expect(within(row).getByRole('button', { name: /regenerat/i })).toBeDisabled()
      })

      resolveRevoke()
      await waitFor(() => {
        expect(mockRevokeHouseholdInviteMutateAsync).toHaveBeenCalledWith({ id: 'invite-42' })
      })
    })

    it('shows an empty state when there are no pending invites', () => {
      renderPanel()
      expect(screen.getByText(/no pending invites/i)).toBeInTheDocument()
    })

    it('shows a loading state instead of the empty state while invites are loading', () => {
      mockUseListHouseholdInvites.mockReturnValue({
        data: undefined,
        isLoading: true,
        error: null,
        refetch: mockRefetchHouseholdInvites,
      })

      renderPanel()

      expect(screen.getByText(/loading invites/i)).toBeInTheDocument()
      expect(screen.queryByText(/no pending invites/i)).not.toBeInTheDocument()
    })

    it('clears a previously displayed link when a Regenerate attempt fails', async () => {
      setInvites([makeInvite({ id: 'invite-42', invitee_email: 'target@example.com' })])
      mockCreateHouseholdInviteMutateAsync.mockResolvedValueOnce({
        invite: makeInvite({ id: 'invite-1' }),
        invite_link: 'https://app.boostedbudget.com/invite/token-abc',
      })

      renderPanel()

      // Generate an unrelated link first so there is something stale to clear.
      fireEvent.change(screen.getByLabelText(/email/i), { target: { value: 'other@example.com' } })
      fireEvent.click(screen.getByRole('button', { name: /generate link/i }))
      expect(await screen.findByText(/token-abc/)).toBeInTheDocument()

      // Regenerate fails at the revoke step — create must never fire, and the
      // stale link must not linger.
      mockRevokeHouseholdInviteMutateAsync.mockRejectedValueOnce(new Error('revoke failed'))
      fireEvent.click(screen.getByRole('button', { name: /^regenerate$/i }))

      await waitFor(() => {
        expect(screen.queryByText(/token-abc/)).not.toBeInTheDocument()
      })
      expect(mockCreateHouseholdInviteMutateAsync).toHaveBeenCalledTimes(1)
    })

    it('disables row actions while Generate is in flight', async () => {
      let resolveCreate: (value: { invite: HouseholdInvite; invite_link: string }) => void = () => {}
      const pending = new Promise<{ invite: HouseholdInvite; invite_link: string }>((resolve) => {
        resolveCreate = resolve
      })
      mockCreateHouseholdInviteMutateAsync.mockImplementationOnce(() => pending)
      setInvites([makeInvite({ id: 'invite-42', invitee_email: 'target@example.com' })])

      renderPanel()
      fireEvent.change(screen.getByLabelText(/email/i), { target: { value: 'new@example.com' } })
      fireEvent.click(screen.getByRole('button', { name: /generate link/i }))

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /^revoke$/i })).toBeDisabled()
        expect(screen.getByRole('button', { name: /^regenerate$/i })).toBeDisabled()
      })

      // Resolve (and await) the in-flight create mutation inside `act`. This
      // test asserts on the SAME "Revoke" element both before and after the
      // resolve — that specific same-element-twice pattern triggers a
      // happy-dom MutationObserver/testing-library `waitFor` interaction
      // where the DOM update from a state change made outside of `act` is
      // only ever observed once the surrounding `waitFor`'s own timeout
      // fires (a multi-second stall), even though the underlying React state
      // updates correctly and near-instantly (verified via instrumentation
      // during debugging). Wrapping the resolve + drain in `act` forces
      // React to flush the resulting re-render synchronously, so the
      // following `waitFor`'s very first synchronous check already sees the
      // updated DOM and never depends on that flaky observer path.
      await act(async () => {
        resolveCreate({
          invite: makeInvite(),
          invite_link: 'https://app.boostedbudget.com/invite/token-xyz',
        })
        await pending
      })

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /^revoke$/i })).not.toBeDisabled()
      })
    })

    it('associates row action buttons with the invite email via aria-describedby, never aria-label (PII must not land in an ARIA attribute)', () => {
      setInvites([makeInvite({ id: 'invite-42', invitee_email: 'target@example.com' })])

      renderPanel()

      const emailNode = screen.getByText('target@example.com')
      const revokeButton = screen.getByRole('button', { name: /^revoke$/i })
      const regenerateButton = screen.getByRole('button', { name: /^regenerate$/i })

      expect(revokeButton).toHaveAttribute('aria-describedby', emailNode.id)
      expect(regenerateButton).toHaveAttribute('aria-describedby', emailNode.id)
      expect(revokeButton).not.toHaveAttribute('aria-label')
      expect(regenerateButton).not.toHaveAttribute('aria-label')
    })
  })
})
