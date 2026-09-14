/**
 * PlaidLinkButton Component Tests
 *
 * Issue #1609 (Item 6): `handleOnExit` used to discard Plaid's `onExit(err, metadata)`
 * arguments entirely, so an error exit (e.g. INSTITUTION_REGISTRATION_REQUIRED) looked
 * identical to the user simply closing Link — no error state, no analytics, no
 * diagnostic info. These tests pin the corrected behaviour: an error exit surfaces a
 * specific message and fires PLAID_LINK_FAILED with the error code, while a clean exit
 * (err === null) stays exactly as silent as before.
 */

import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, waitFor, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { PlaidLinkButton } from './PlaidLinkButton'
import { mockExchangePublicTokenMutateAsync, mockUsePlaidLink } from '@/test/mocks/plaid-api'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { ANALYTICS_EVENTS } from '../../shared/analytics'
import type { PlaidLinkError, PlaidLinkOnExitMetadata } from 'react-plaid-link'

const CLEAN_EXIT_METADATA: PlaidLinkOnExitMetadata = {
  institution: null,
  status: null,
  link_session_id: 'session-clean-1',
  request_id: 'request-clean-1',
}

const ERROR_EXIT_ERROR: PlaidLinkError = {
  error_type: 'INSTITUTION_ERROR',
  error_code: 'INSTITUTION_REGISTRATION_REQUIRED',
  error_message: 'the institution is not registered',
  display_message: '',
}

const ERROR_EXIT_METADATA: PlaidLinkOnExitMetadata = {
  institution: { name: 'Citibank Online', institution_id: 'ins_5' },
  status: null,
  link_session_id: 'session-error-1',
  request_id: 'request-error-1',
}

function renderButton(props: { onExit?: () => void } = {}) {
  const queryClient = new QueryClient()
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <PlaidLinkButton {...props} />
      </MemoryRouter>
    </QueryClientProvider>
  )
}

/** Retrieve the onExit callback the component most recently wired into usePlaidLink. */
function getCapturedOnExit(): (
  err: PlaidLinkError | null,
  metadata: PlaidLinkOnExitMetadata
) => void {
  const lastCall = mockUsePlaidLink.mock.calls.at(-1)
  const onExit = (lastCall?.[0] as { onExit?: unknown } | undefined)?.onExit
  if (typeof onExit !== 'function') {
    throw new Error('usePlaidLink was not called with an onExit option')
  }
  return onExit as (err: PlaidLinkError | null, metadata: PlaidLinkOnExitMetadata) => void
}

function getCapturedOnSuccess(): (
  publicToken: string,
  metadata: { institution: { institution_id: string; name: string } }
) => Promise<void> {
  const lastCall = mockUsePlaidLink.mock.calls.at(-1)
  const onSuccess = (lastCall?.[0] as { onSuccess?: unknown } | undefined)?.onSuccess
  if (typeof onSuccess !== 'function') {
    throw new Error('usePlaidLink was not called with an onSuccess option')
  }
  return onSuccess as (
    publicToken: string,
    metadata: { institution: { institution_id: string; name: string } }
  ) => Promise<void>
}

describe('PlaidLinkButton onExit handling', () => {
  beforeEach(() => {
    mockTrackEvent.mockClear()
    mockUsePlaidLink.mockClear()
    mockExchangePublicTokenMutateAsync.mockClear()
  })

  it('records one canonical account-link completion after the token exchange succeeds', async () => {
    renderButton()
    await waitFor(() => screen.getByText('Connect Bank Account'))

    await act(async () => {
      await getCapturedOnSuccess()('public-token', {
        institution: { institution_id: 'ins_5', name: 'Citibank Online' },
      })
    })

    expect(mockExchangePublicTokenMutateAsync).toHaveBeenCalledTimes(1)
    expect(mockTrackEvent).toHaveBeenCalledWith(
      ANALYTICS_EVENTS.ACCOUNT_LINK_COMPLETED,
      { provider: 'plaid' },
    )
    const calls = mockTrackEvent.mock.calls as unknown as Array<[string, Record<string, unknown>?]>
    const completionEventNames = new Set<string>([
      ANALYTICS_EVENTS.ACCOUNT_LINK_COMPLETED,
      ANALYTICS_EVENTS.PLAID_LINK_SUCCEEDED,
      ANALYTICS_EVENTS.FIRST_ACCOUNT_LINKED,
    ])
    const completionEvents = calls.filter(([event]) => completionEventNames.has(event))
    expect(completionEvents).toHaveLength(1)
  })

  it('surfaces a specific, diagnosable message when Link exits with an error', async () => {
    renderButton()
    await waitFor(() => screen.getByText('Connect Bank Account'))

    const onExit = getCapturedOnExit()
    act(() => {
      onExit(ERROR_EXIT_ERROR, ERROR_EXIT_METADATA)
    })

    await waitFor(() => {
      expect(
        screen.getByText(/INSTITUTION_REGISTRATION_REQUIRED/)
      ).toBeTruthy()
    })
  })

  it('fires PLAID_LINK_FAILED with the Plaid error code when Link exits with an error', async () => {
    renderButton()
    await waitFor(() => screen.getByText('Connect Bank Account'))

    const onExit = getCapturedOnExit()
    act(() => {
      onExit(ERROR_EXIT_ERROR, ERROR_EXIT_METADATA)
    })

    await waitFor(() => {
      expect(mockTrackEvent).toHaveBeenCalledWith(
        ANALYTICS_EVENTS.PLAID_LINK_FAILED,
        expect.objectContaining({ error_code: 'INSTITUTION_REGISTRATION_REQUIRED' })
      )
    })
  })

  it('stays silent (no error state, no failure analytics) on a clean user-initiated exit', async () => {
    renderButton()
    await waitFor(() => screen.getByText('Connect Bank Account'))

    const onExit = getCapturedOnExit()
    act(() => {
      onExit(null, CLEAN_EXIT_METADATA)
    })

    // Give any (incorrect) async state update a chance to land before asserting absence.
    await new Promise(resolve => setTimeout(resolve, 0))

    expect(screen.queryByText(/INSTITUTION_REGISTRATION_REQUIRED/)).toBeNull()
    const calls = mockTrackEvent.mock.calls as unknown as unknown[][]
    const firedFailureEvent = calls.some(call => call[0] === ANALYTICS_EVENTS.PLAID_LINK_FAILED)
    expect(firedFailureEvent).toBe(false)
  })

  it('still calls the onExit prop on both an error exit and a clean exit', async () => {
    const onExitProp = mock(() => {})
    renderButton({ onExit: onExitProp })
    await waitFor(() => screen.getByText('Connect Bank Account'))

    const onExit = getCapturedOnExit()

    act(() => {
      onExit(ERROR_EXIT_ERROR, ERROR_EXIT_METADATA)
    })
    expect(onExitProp).toHaveBeenCalledTimes(1)

    act(() => {
      onExit(null, CLEAN_EXIT_METADATA)
    })
    expect(onExitProp).toHaveBeenCalledTimes(2)
  })
})
