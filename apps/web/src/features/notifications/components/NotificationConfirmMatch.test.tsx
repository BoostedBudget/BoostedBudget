/**
 * NotificationConfirmMatch Tests
 * Issue #972: Savings Transfer Automation — confirm fuzzy-matched transfer amount
 */

import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { screen, fireEvent, waitFor } from '@testing-library/react'
import React from 'react'
import {
  mockUseConfirmTransferSuggestionMatch,
  mockConfirmTransferSuggestionMatchMutateAsync,
} from '@/test/mocks/transfer-suggestions-api'
import { mockToastError } from '@/test/mocks/sonner'
import { renderWithQuery } from '@/test/utils/queryTestUtils'
import { mockTrackEvent } from '@/test/mocks/track-event'

mock.module('@/shared/hooks/useTrackEvent', () => ({
  useTrackEvent: () => mockTrackEvent,
}))

const { NotificationConfirmMatch } = await import('./NotificationConfirmMatch')

describe('NotificationConfirmMatch', () => {
  beforeEach(() => {
    mockUseConfirmTransferSuggestionMatch.mockClear()
    mockConfirmTransferSuggestionMatchMutateAsync.mockClear()
    mockToastError.mockClear()
    mockTrackEvent.mockClear()

    mockUseConfirmTransferSuggestionMatch.mockReturnValue({
      mutate: mockConfirmTransferSuggestionMatchMutateAsync,
      mutateAsync: mockConfirmTransferSuggestionMatchMutateAsync,
      isPending: false,
      isError: false,
    })
  })

  it('renders actual amount input and Confirm button', () => {
    renderWithQuery(
      <NotificationConfirmMatch suggestionId="sugg-1" onDismiss={() => {}} />
    )
    expect(screen.getByRole('spinbutton')).toBeTruthy()
    expect(screen.getByRole('button', { name: /confirm/i })).toBeTruthy()
  })

  it('renders a Dismiss button', () => {
    renderWithQuery(
      <NotificationConfirmMatch suggestionId="sugg-1" onDismiss={() => {}} />
    )
    expect(screen.getByRole('button', { name: /dismiss/i })).toBeTruthy()
  })

  it('shows validation error when amount is 0', async () => {
    renderWithQuery(
      <NotificationConfirmMatch suggestionId="sugg-1" onDismiss={() => {}} />
    )
    const input = screen.getByRole('spinbutton')
    fireEvent.change(input, { target: { value: '0' } })
    fireEvent.click(screen.getByRole('button', { name: /confirm/i }))
    await waitFor(() => {
      expect(screen.getByText(/amount must be greater/i)).toBeInTheDocument()
    })
    expect(mockConfirmTransferSuggestionMatchMutateAsync).not.toHaveBeenCalled()
  })

  it('shows validation error when amount is empty', async () => {
    renderWithQuery(
      <NotificationConfirmMatch suggestionId="sugg-1" onDismiss={() => {}} />
    )
    fireEvent.click(screen.getByRole('button', { name: /confirm/i }))
    await waitFor(() => {
      expect(screen.getByText(/amount must be greater/i)).toBeInTheDocument()
    })
    expect(mockConfirmTransferSuggestionMatchMutateAsync).not.toHaveBeenCalled()
  })

  it('calls confirm mutation with actual_amount when valid amount is entered', async () => {
    renderWithQuery(
      <NotificationConfirmMatch suggestionId="sugg-1" onDismiss={() => {}} />
    )
    const input = screen.getByRole('spinbutton')
    fireEvent.change(input, { target: { value: '250.50' } })
    fireEvent.click(screen.getByRole('button', { name: /confirm/i }))
    await waitFor(() => {
      expect(mockConfirmTransferSuggestionMatchMutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          id: 'sugg-1',
          data: expect.objectContaining({ actual_amount: 250.50 }),
        })
      )
    })
  })

  it('calls onDismiss when Dismiss is clicked', () => {
    let dismissed = false
    renderWithQuery(
      <NotificationConfirmMatch suggestionId="sugg-1" onDismiss={() => { dismissed = true }} />
    )
    fireEvent.click(screen.getByRole('button', { name: /dismiss/i }))
    expect(dismissed).toBe(true)
  })

  it('applies data-ph-mask to the amount input', () => {
    renderWithQuery(
      <NotificationConfirmMatch suggestionId="sugg-1" onDismiss={() => {}} />
    )
    const input = screen.getByRole('spinbutton')
    expect(input.hasAttribute('data-ph-mask')).toBe(true)
  })

  // C7: drive mutation.onError — assert toast fires and analytics NOT tracked.
  // Previous version only rendered with isError=true without driving onError,
  // never proving the error callback path actually executes.
  it('fires error toast on mutation failure and does NOT track analytics', () => {
    let capturedOnError: (() => void) | undefined

    mockUseConfirmTransferSuggestionMatch.mockImplementation(
      (options?: { mutation?: { onSuccess?: () => void; onError?: () => void } }) => {
        capturedOnError = options?.mutation?.onError
        return {
          mutate: mock((_args: unknown) => { capturedOnError?.() }),
          isPending: false,
          isError: false,
        }
      }
    )

    renderWithQuery(
      <NotificationConfirmMatch suggestionId="sugg-err" onDismiss={() => {}} />
    )

    // Enter a valid amount and click Confirm → mutate fires → drives onError.
    fireEvent.change(screen.getByRole('spinbutton'), { target: { value: '50' } })
    fireEvent.click(screen.getByRole('button', { name: /confirm/i }))

    // The onError path must fire the error toast (NotificationConfirmMatch.tsx:42-44).
    // sonner is globally mocked via preload in bunfig.toml; mockToastError captures calls.
    expect(mockToastError).toHaveBeenCalledTimes(1)
    expect(mockToastError).toHaveBeenCalledWith('Failed to confirm transfer. Please try again.')

    // The onError path must NOT fire the analytics event.
    expect(mockTrackEvent).not.toHaveBeenCalled()

    // The component must remain stable after error (not unmounted/crashed).
    expect(screen.getByRole('spinbutton')).toBeTruthy()
  })

  it('calls onConfirm when confirm mutation succeeds', async () => {
    let capturedOnSuccess: (() => void) | undefined
    mockUseConfirmTransferSuggestionMatch.mockImplementation((options?: { mutation?: { onSuccess?: () => void } }) => {
      capturedOnSuccess = options?.mutation?.onSuccess
      return {
        mutate: mock((_args: unknown) => { capturedOnSuccess?.() }),
        isPending: false,
        isError: false,
      }
    })

    const onConfirmSpy = mock(() => {})
    renderWithQuery(
      <NotificationConfirmMatch suggestionId="sugg-1" onDismiss={() => {}} onConfirm={onConfirmSpy} />
    )

    fireEvent.change(screen.getByRole('spinbutton'), { target: { value: '100' } })
    fireEvent.click(screen.getByRole('button', { name: /confirm/i }))

    expect(onConfirmSpy).toHaveBeenCalledTimes(1)
  })

  it('fires TRANSFER_SUGGESTION_VALIDATED on successful confirm', () => {
    // Wire the confirm mutation so that calling mutate() synchronously invokes
    // the component's onSuccess callback — the only way to exercise the trackEvent call.
    // The previous version of this test only asserted the event did NOT fire on mount
    // (false/weak coverage that never drove onSuccess).
    let capturedOnSuccess: (() => void) | undefined
    mockUseConfirmTransferSuggestionMatch.mockImplementation(
      (options?: { mutation?: { onSuccess?: () => void } }) => {
        capturedOnSuccess = options?.mutation?.onSuccess
        return {
          mutate: mock((_args: unknown) => { capturedOnSuccess?.() }),
          isPending: false,
          isError: false,
        }
      }
    )

    renderWithQuery(
      <NotificationConfirmMatch suggestionId="sugg-analytics" onDismiss={() => {}} />
    )

    // Enter a valid amount and click Confirm → mutate fires → onSuccess → trackEvent.
    fireEvent.change(screen.getByRole('spinbutton'), { target: { value: '75' } })
    fireEvent.click(screen.getByRole('button', { name: /confirm/i }))

    expect(mockTrackEvent).toHaveBeenCalledWith('transfer_suggestion_validated')
  })
})
