/**
 * AcceptInvitePage Component Tests
 *
 * Issue #1370: Household feature — standalone invite-accept page (Task 6)
 * TDD RED: written before implementation.
 *
 * Routes covered: `/households/invites/:token` (real link, per
 * household_service.go's invite_link generation) and `/household/invite/:token`
 * (alias, per the original issue text) — both are exercised in App.test.tsx.
 * This file drives the page itself via the REAL `useAcceptInvite` hook,
 * mocked at the api-client layer (`@/test/mocks/household-api`) — same
 * approach used throughout this feature's other composing tests
 * (HouseholdTab.test.tsx) — plus the centralized Clerk mock
 * (`@/test/mocks/clerk`) and accounts mock (`@/test/mocks/accounts-api`) for
 * the nested ConsentPreview.
 */
import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { GetInvitePreviewResponseBody, AcceptInviteResponseBody, Account } from '@boostedbudget/api-client/models'
import {
  mockUseGetHouseholdInvitePreview,
  mockUseAcceptHouseholdInvite,
  mockUseDeclineHouseholdInvite,
  mockAcceptHouseholdInviteMutateAsync,
  mockDeclineHouseholdInviteMutateAsync,
} from '@/test/mocks/household-api'
import { mockUseAuth, mockUseClerk, mockUseUser, createMockClerkAuth, createMockClerk, createMockClerkUser } from '@/test/mocks/clerk'
import { mockUseGetAccounts } from '@/test/mocks/accounts-api'
import { mockNavigate } from '@/test/mocks/react-router'
import { AcceptInvitePage } from './AcceptInvitePage'

const mockPreview: GetInvitePreviewResponseBody = {
  household_name: 'The Smiths',
  invitee_email: 'invitee@example.com',
  expires_at: '2025-02-01T00:00:00Z',
}

const mockAcceptResponse: AcceptInviteResponseBody = {
  household_id: 'household-1',
  member: {
    id: 'member-1',
    household_id: 'household-1',
    user_id: 'user-2',
    role: 'member',
    status: 'active',
    joined_at: '2025-01-15T00:00:00Z',
    created_at: '2025-01-15T00:00:00Z',
    updated_at: '2025-01-15T00:00:00Z',
  },
}

const mockAccount: Account = {
  id: 'account-1',
  user_account_id: 'ua-1',
  user_id: 'user-2',
  linked_item_id: 'item-1',
  plaid_account_id: 'plaid-1',
  name: 'Checking',
  official_name: 'Primary Checking',
  type: 'depository',
  subtype: 'checking',
  mask: '1234',
  current_balance: 1000,
  currency_code: 'USD',
  institution_name: 'Test Bank',
  is_hidden: false,
  status: 'active',
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-01T00:00:00Z',
}

function renderPage(token = 'tok-123', routePath: '/households/invites/:token' | '/household/invite/:token' = '/households/invites/:token') {
  const basePath = routePath === '/households/invites/:token' ? '/households/invites' : '/household/invite'
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[`${basePath}/${token}`]}>
        <Routes>
          <Route path={routePath} element={<AcceptInvitePage />} />
          <Route path="/dashboard" element={<div>Dashboard Page</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  )
}

describe('AcceptInvitePage', () => {
  beforeEach(() => {
    mockUseGetHouseholdInvitePreview.mockClear()
    mockUseAcceptHouseholdInvite.mockClear()
    mockUseDeclineHouseholdInvite.mockClear()
    mockAcceptHouseholdInviteMutateAsync.mockClear()
    mockDeclineHouseholdInviteMutateAsync.mockClear()
    mockUseAuth.mockClear()
    mockUseClerk.mockClear()
    mockUseUser.mockClear()
    mockUseGetAccounts.mockClear()
    mockNavigate.mockClear()

    mockUseGetHouseholdInvitePreview.mockReturnValue({
      data: mockPreview,
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve({}),
    })
    mockUseAcceptHouseholdInvite.mockReturnValue({ mutateAsync: mockAcceptHouseholdInviteMutateAsync, isPending: false })
    mockUseDeclineHouseholdInvite.mockReturnValue({ mutateAsync: mockDeclineHouseholdInviteMutateAsync, isPending: false })
    mockUseAuth.mockReturnValue(createMockClerkAuth({ isSignedIn: true, isLoaded: true }))
    mockUseClerk.mockReturnValue(createMockClerk())
    mockUseUser.mockReturnValue(createMockClerkUser({ isSignedIn: true, isLoaded: true }))
    mockUseGetAccounts.mockReturnValue({ data: { accounts: [mockAccount] }, isLoading: false, error: null, refetch: () => Promise.resolve() })
  })

  it('shows a loading state while the invite preview is fetching', () => {
    mockUseGetHouseholdInvitePreview.mockReturnValue({ data: undefined, isLoading: true, error: null, refetch: () => Promise.resolve({}) })

    renderPage()

    expect(screen.getByText(/loading/i)).toBeInTheDocument()
    expect(screen.queryByText('The Smiths')).not.toBeInTheDocument()
  })

  it('shows an invalid-invite state on a 404 (not_found)', () => {
    mockUseGetHouseholdInvitePreview.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: { response: { status: 404 } },
      refetch: () => Promise.resolve({}),
    })

    renderPage()

    expect(screen.getByText(/invalid or has expired/i)).toBeInTheDocument()
  })

  it('shows a seats-full state on a 409 from the initial preview', () => {
    mockUseGetHouseholdInvitePreview.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: { response: { status: 409 } },
      refetch: () => Promise.resolve({}),
    })

    renderPage()

    expect(screen.getByText(/now full/i)).toBeInTheDocument()
  })

  it('shows a generic error state (not a broken "Join undefined" form) on an unmapped preview error', () => {
    mockUseGetHouseholdInvitePreview.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error('network down'),
      refetch: () => Promise.resolve({}),
    })

    renderPage()

    expect(screen.getByText(/something went wrong/i)).toBeInTheDocument()
    expect(screen.queryByText(/join undefined/i)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /join household/i })).not.toBeInTheDocument()
  })

  it('shows a sign-in prompt with the invite preview when signed out', () => {
    mockUseAuth.mockReturnValue(createMockClerkAuth({ isSignedIn: false, isLoaded: true }))
    const openSignIn = () => {}
    mockUseClerk.mockReturnValue(createMockClerk({ openSignIn }))

    renderPage()

    expect(screen.getByText(/The Smiths/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /sign in to continue/i })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /join household/i })).not.toBeInTheDocument()
  })

  it('calls openSignIn with the current URL as forceRedirectUrl when signed out', () => {
    mockUseAuth.mockReturnValue(createMockClerkAuth({ isSignedIn: false, isLoaded: true }))
    const openSignInCalls: unknown[] = []
    mockUseClerk.mockReturnValue(createMockClerk({ openSignIn: (opts: unknown) => { openSignInCalls.push(opts) } }))

    renderPage()
    fireEvent.click(screen.getByRole('button', { name: /sign in to continue/i }))

    expect(openSignInCalls).toEqual([{ forceRedirectUrl: window.location.href }])
  })

  it('shows an email-mismatch card after a 403 accept failure, hiding Join/Decline', async () => {
    mockAcceptHouseholdInviteMutateAsync.mockRejectedValue({ response: { status: 403 } })

    renderPage()
    fireEvent.click(screen.getByRole('button', { name: /join household/i }))

    await waitFor(() => {
      expect(screen.getByText(/invitee@example\.com/)).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /join household/i })).not.toBeInTheDocument()
    })
  })

  it('renders ConsentPreview and Join/Decline buttons on the happy path', () => {
    renderPage()

    expect(screen.getByText(/The Smiths/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /join household/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /decline/i })).toBeInTheDocument()
    expect(screen.getByText('Primary Checking')).toBeInTheDocument()
  })

  it('shows an inline (not full-page) error on an unmapped Join failure, keeping the form usable for a retry', async () => {
    mockAcceptHouseholdInviteMutateAsync.mockRejectedValue(new Error('network down'))
    renderPage()

    fireEvent.click(screen.getByRole('button', { name: /join household/i }))

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/something went wrong/i)
    })
    // The form must stay usable — this is NOT the same full-page state as an
    // unmapped preview-fetch error.
    expect(screen.getByText(/The Smiths/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /join household/i })).toBeInTheDocument()
  })

  it('disables Join and Decline while a Join mutation is in flight, preventing a double-submit', async () => {
    let resolveAccept: (value: typeof mockAcceptResponse) => void = () => {}
    mockAcceptHouseholdInviteMutateAsync.mockImplementation(
      () => new Promise((resolve) => { resolveAccept = resolve })
    )
    renderPage()

    const joinButton = screen.getByRole('button', { name: /join household/i })
    const declineButton = screen.getByRole('button', { name: /decline/i })
    fireEvent.click(joinButton)

    await waitFor(() => {
      expect(joinButton).toBeDisabled()
      expect(declineButton).toBeDisabled()
    })

    resolveAccept(mockAcceptResponse)
    // Let the mutation's resolution (and the resulting state updates) settle
    // within this test, so React doesn't warn about an unwrapped `act()`
    // update happening after the test has already finished.
    await waitFor(() => {
      expect(screen.getByText(/you've joined/i)).toBeInTheDocument()
    })
  })

  it('renders identically via the /household/invite/:token alias route, extracting the same token', async () => {
    mockAcceptHouseholdInviteMutateAsync.mockResolvedValue(mockAcceptResponse)
    renderPage('tok-alias-1', '/household/invite/:token')

    expect(screen.getByText(/The Smiths/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /join household/i }))

    await waitFor(() => {
      expect(mockAcceptHouseholdInviteMutateAsync).toHaveBeenCalledWith({
        token: 'tok-alias-1',
        data: { consent: true, hidden_account_ids: [] },
      })
    })
  })

  it('Join calls accept with the collected hidden-ids array (empty when nothing toggled off)', async () => {
    mockAcceptHouseholdInviteMutateAsync.mockResolvedValue(mockAcceptResponse)
    renderPage('tok-123')

    fireEvent.click(screen.getByRole('button', { name: /join household/i }))

    await waitFor(() => {
      expect(mockAcceptHouseholdInviteMutateAsync).toHaveBeenCalledWith({
        token: 'tok-123',
        data: { consent: true, hidden_account_ids: [] },
      })
    })
  })

  it('Join collects toggled-off account ids into hidden_account_ids', async () => {
    mockAcceptHouseholdInviteMutateAsync.mockResolvedValue(mockAcceptResponse)
    renderPage('tok-123')

    const [accountSwitch] = screen.getAllByRole('switch')
    fireEvent.click(accountSwitch)
    fireEvent.click(screen.getByRole('button', { name: /join household/i }))

    await waitFor(() => {
      expect(mockAcceptHouseholdInviteMutateAsync).toHaveBeenCalledWith({
        token: 'tok-123',
        data: { consent: true, hidden_account_ids: ['ua-1'] },
      })
    })
  })

  it('shows a seats-full state on a 409 accept failure', async () => {
    mockAcceptHouseholdInviteMutateAsync.mockRejectedValue({ response: { status: 409 } })
    renderPage()

    fireEvent.click(screen.getByRole('button', { name: /join household/i }))

    await waitFor(() => {
      expect(screen.getByText(/now full/i)).toBeInTheDocument()
    })
  })

  it('shows a success state and eventually redirects to /dashboard after a successful accept', async () => {
    mockAcceptHouseholdInviteMutateAsync.mockResolvedValue(mockAcceptResponse)
    renderPage()

    fireEvent.click(screen.getByRole('button', { name: /join household/i }))

    await waitFor(() => {
      expect(screen.getByText(/you've joined/i)).toBeInTheDocument()
    })

    await waitFor(
      () => {
        expect(mockNavigate).toHaveBeenCalledWith('/dashboard')
      },
      { timeout: 3000 }
    )
  })

  it('Decline calls decline() and shows a declined confirmation', async () => {
    mockDeclineHouseholdInviteMutateAsync.mockResolvedValue(undefined)
    renderPage('tok-123')

    fireEvent.click(screen.getByRole('button', { name: /decline/i }))

    await waitFor(() => {
      expect(mockDeclineHouseholdInviteMutateAsync).toHaveBeenCalledWith({ token: 'tok-123' })
      expect(screen.getByText(/invite declined/i)).toBeInTheDocument()
    })
  })
})
