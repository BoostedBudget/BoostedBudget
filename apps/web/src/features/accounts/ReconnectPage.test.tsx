/**
 * ReconnectPage tests (Issue #1609).
 *
 * These pin the fix for the "Reconnect Bank" button leading to a broken page: the notification's
 * action_url is item_id-only (no link_token), and this page mints its own update-mode Plaid Link
 * token, opens Link, and on success calls the REPAIR endpoint — never the normal exchange-token
 * flow (which would create a duplicate linked_items row).
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, waitFor, act } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ReconnectPage } from './ReconnectPage'
import {
  mockUsePlaidLink,
  mockPlaidLinkOpen,
  mockCreateReconnectLinkTokenMutateAsync,
  mockUseCreateReconnectLinkToken,
  mockRepairLinkedItemMutateAsync,
  mockUseRepairLinkedItem,
} from '@/test/mocks/plaid-api'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { mockToastSuccess, mockToastError } from '@/test/mocks/sonner'
import { ANALYTICS_EVENTS } from '../../shared/analytics'
import type { PlaidLinkError, PlaidLinkOnExitMetadata } from 'react-plaid-link'

function renderPage(initialEntry: string) {
  const queryClient = new QueryClient()
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initialEntry]}>
        <Routes>
          <Route path="/accounts/reconnect" element={<ReconnectPage />} />
          <Route path="/accounts" element={<div>Accounts Page Marker</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  )
}

/** Retrieve the onSuccess/onExit callbacks the component most recently wired into usePlaidLink. */
function getCapturedPlaidLinkOptions(): {
  onSuccess: (publicToken: string, metadata: unknown) => void
  onExit: (err: PlaidLinkError | null, metadata: PlaidLinkOnExitMetadata) => void
} {
  const lastCall = mockUsePlaidLink.mock.calls.at(-1)
  const options = lastCall?.[0] as
    | { onSuccess?: unknown; onExit?: unknown }
    | undefined
  if (typeof options?.onSuccess !== 'function' || typeof options?.onExit !== 'function') {
    throw new Error('usePlaidLink was not called with onSuccess/onExit options')
  }
  return {
    onSuccess: options.onSuccess as (publicToken: string, metadata: unknown) => void,
    onExit: options.onExit as (err: PlaidLinkError | null, metadata: PlaidLinkOnExitMetadata) => void,
  }
}

const EXIT_ERROR: PlaidLinkError = {
  error_type: 'INSTITUTION_ERROR',
  error_code: 'INSTITUTION_NOT_RESPONDING',
  error_message: 'the institution is not responding',
  display_message: '',
}

const EXIT_METADATA: PlaidLinkOnExitMetadata = {
  institution: { name: 'Test Bank', institution_id: 'ins_1' },
  status: null,
  link_session_id: 'session-1',
  request_id: 'request-1',
}

describe('ReconnectPage', () => {
  beforeEach(() => {
    mockTrackEvent.mockClear()
    mockUsePlaidLink.mockClear()
    mockPlaidLinkOpen.mockClear()
    mockToastSuccess.mockClear()
    mockToastError.mockClear()
    mockUseCreateReconnectLinkToken.mockClear()
    mockCreateReconnectLinkTokenMutateAsync.mockClear()
    mockCreateReconnectLinkTokenMutateAsync.mockImplementation(() =>
      Promise.resolve({ link_token: 'test-reconnect-link-token' })
    )
    mockUseRepairLinkedItem.mockClear()
    mockRepairLinkedItemMutateAsync.mockClear()
    mockRepairLinkedItemMutateAsync.mockImplementation(() => Promise.resolve({ success: true }))
  })

  it('mints a reconnect link token scoped to the item_id from the query string', async () => {
    renderPage('/accounts/reconnect?item_id=plaid-item-789')

    await waitFor(() => {
      expect(mockCreateReconnectLinkTokenMutateAsync).toHaveBeenCalledWith({
        data: { item_id: 'plaid-item-789' },
      })
    })
  })

  it('shows an error and never mints a token when item_id is missing from the query string', async () => {
    renderPage('/accounts/reconnect')

    await waitFor(() => {
      expect(screen.getByText(/missing its account reference/)).toBeTruthy()
    })
    expect(mockCreateReconnectLinkTokenMutateAsync).not.toHaveBeenCalled()
  })

  it('opens Plaid Link once a token is minted and fires ACCOUNT_RECONNECT_LINK_STARTED', async () => {
    renderPage('/accounts/reconnect?item_id=plaid-item-789')

    await waitFor(() => {
      expect(mockUsePlaidLink).toHaveBeenCalledWith(
        expect.objectContaining({ token: 'test-reconnect-link-token' })
      )
    })
    await waitFor(() => {
      expect(mockTrackEvent).toHaveBeenCalledWith(ANALYTICS_EVENTS.ACCOUNT_RECONNECT_LINK_STARTED)
    })
    // #1609 round 2, should-fix 2: the assertions above only prove the token was minted and an
    // analytics event fired — NOT that Plaid Link was actually opened. Before this fix the test
    // passed even with the component's `open()` call deleted entirely, because `open` was never
    // captured (mockUsePlaidLink used to return a fresh, uncapturable mock() per render). Assert
    // the actual open() invocation.
    await waitFor(() => {
      expect(mockPlaidLinkOpen).toHaveBeenCalled()
    })
  })

  it('calls repairLinkedItem (never exchangePublicToken) on a successful Link flow, then navigates to /accounts', async () => {
    renderPage('/accounts/reconnect?item_id=plaid-item-789')
    await waitFor(() => expect(mockUsePlaidLink).toHaveBeenCalled())

    const { onSuccess } = getCapturedPlaidLinkOptions()
    await act(async () => {
      onSuccess('public-token-should-be-ignored', {})
    })

    await waitFor(() => {
      expect(mockRepairLinkedItemMutateAsync).toHaveBeenCalledWith({
        data: { item_id: 'plaid-item-789' },
      })
    })
    await waitFor(() => {
      expect(mockTrackEvent).toHaveBeenCalledWith(ANALYTICS_EVENTS.ACCOUNT_RECONNECT_SUCCEEDED)
    })
    await waitFor(() => {
      expect(screen.getByText('Accounts Page Marker')).toBeTruthy()
    })
  })

  it('shows an error and fires ACCOUNT_RECONNECT_FAILED when the repair call fails after a successful Link', async () => {
    mockRepairLinkedItemMutateAsync.mockImplementation(() =>
      Promise.reject(new Error('repair failed'))
    )
    renderPage('/accounts/reconnect?item_id=plaid-item-789')
    await waitFor(() => expect(mockUsePlaidLink).toHaveBeenCalled())

    const { onSuccess } = getCapturedPlaidLinkOptions()
    await act(async () => {
      onSuccess('public-token-should-be-ignored', {})
    })

    await waitFor(() => {
      expect(mockTrackEvent).toHaveBeenCalledWith(
        ANALYTICS_EVENTS.ACCOUNT_RECONNECT_FAILED,
        { stage: 'repair' }
      )
    })
    await waitFor(() => {
      expect(screen.getByText(/repair failed/)).toBeTruthy()
    })
  })

  it('surfaces a diagnosable message and fires ACCOUNT_RECONNECT_FAILED when Link exits with an error', async () => {
    renderPage('/accounts/reconnect?item_id=plaid-item-789')
    await waitFor(() => expect(mockUsePlaidLink).toHaveBeenCalled())

    const { onExit } = getCapturedPlaidLinkOptions()
    act(() => {
      onExit(EXIT_ERROR, EXIT_METADATA)
    })

    await waitFor(() => {
      expect(screen.getByText(/INSTITUTION_NOT_RESPONDING/)).toBeTruthy()
    })
    expect(mockTrackEvent).toHaveBeenCalledWith(ANALYTICS_EVENTS.ACCOUNT_RECONNECT_FAILED, {
      stage: 'link',
      error_code: 'INSTITUTION_NOT_RESPONDING',
      error_type: 'INSTITUTION_ERROR',
    })
    expect(mockRepairLinkedItemMutateAsync).not.toHaveBeenCalled()
  })

  it('shows the mint failure detail, fires ACCOUNT_RECONNECT_FAILED, and never opens Plaid Link when minting the token fails', async () => {
    mockCreateReconnectLinkTokenMutateAsync.mockImplementation(() =>
      Promise.reject({
        response: { data: { detail: 'This connection is not in a state that can be reconnected' } },
      })
    )
    renderPage('/accounts/reconnect?item_id=plaid-item-789')

    await waitFor(() => {
      expect(
        screen.getByText('This connection is not in a state that can be reconnected')
      ).toBeTruthy()
    })
    // #1609 round 2, should-fix 3: a mint failure used to show an error without ever emitting
    // analytics — ACCOUNT_RECONNECT_FAILED fired only for a Link exit or a repair failure, so a
    // mint failure (the FIRST thing that can go wrong on this page) was invisible to analytics.
    expect(mockTrackEvent).toHaveBeenCalledWith(ANALYTICS_EVENTS.ACCOUNT_RECONNECT_FAILED, {
      stage: 'mint',
    })
    expect(mockRepairLinkedItemMutateAsync).not.toHaveBeenCalled()
  })
})
