/**
 * NotificationAcceptReject Tests
 * Issue #952: Notification Center — Accept/Reject flow for transfer suggestions
 * Issue #972: C8 — analytics events fire with amount + source on onSuccess (test rigor)
 */

import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { screen, fireEvent } from '@testing-library/react'
import React from 'react'
import { NotificationAcceptReject } from './NotificationAcceptReject'
import {
  mockUseAcceptTransferSuggestion,
  mockUseRejectTransferSuggestion,
  mockAcceptTransferSuggestionMutateAsync,
  mockRejectTransferSuggestionMutateAsync,
} from '@/test/mocks/transfer-suggestions-api'
import { mockToastSuccess } from '@/test/mocks/sonner'
import { renderWithQuery } from '@/test/utils/queryTestUtils'
import { mockTrackEvent } from '@/test/mocks/track-event'

mock.module('@/shared/hooks/useTrackEvent', () => ({
  useTrackEvent: () => mockTrackEvent,
}))

describe('NotificationAcceptReject', () => {
  beforeEach(() => {
    mockUseAcceptTransferSuggestion.mockClear()
    mockUseRejectTransferSuggestion.mockClear()
    mockAcceptTransferSuggestionMutateAsync.mockClear()
    mockRejectTransferSuggestionMutateAsync.mockClear()
    mockToastSuccess.mockClear()
    mockTrackEvent.mockClear()

    mockUseAcceptTransferSuggestion.mockReturnValue({
      mutate: mockAcceptTransferSuggestionMutateAsync,
      mutateAsync: mockAcceptTransferSuggestionMutateAsync,
      isPending: false,
    })
    mockUseRejectTransferSuggestion.mockReturnValue({
      mutate: mockRejectTransferSuggestionMutateAsync,
      mutateAsync: mockRejectTransferSuggestionMutateAsync,
      isPending: false,
    })
  })

  it('renders Accept and Reject buttons', () => {
    renderWithQuery(
      <NotificationAcceptReject suggestionId="sugg-1" amount={200} currencyCode="USD" />
    )
    expect(screen.getByRole('button', { name: /accept/i })).toBeTruthy()
    expect(screen.getByRole('button', { name: /reject/i })).toBeTruthy()
  })

  it('calls accept mutation when Accept is clicked', () => {
    renderWithQuery(
      <NotificationAcceptReject suggestionId="sugg-1" amount={200} currencyCode="USD" />
    )
    fireEvent.click(screen.getByRole('button', { name: /accept/i }))
    expect(mockAcceptTransferSuggestionMutateAsync).toHaveBeenCalledTimes(1)
  })

  it('calls reject mutation when Reject is clicked', () => {
    renderWithQuery(
      <NotificationAcceptReject suggestionId="sugg-1" amount={200} currencyCode="USD" />
    )
    fireEvent.click(screen.getByRole('button', { name: /reject/i }))
    expect(mockRejectTransferSuggestionMutateAsync).toHaveBeenCalledTimes(1)
  })

  it('disables both buttons when accept is pending', () => {
    mockUseAcceptTransferSuggestion.mockReturnValue({
      mutate: () => {},
      mutateAsync: () => Promise.resolve({}),
      isPending: true,
      isError: false,
    })
    renderWithQuery(
      <NotificationAcceptReject suggestionId="sugg-1" amount={200} currencyCode="USD" />
    )
    const acceptBtn = screen.getByRole('button', { name: /accept/i })
    const rejectBtn = screen.getByRole('button', { name: /reject/i })
    expect(acceptBtn.getAttribute('disabled')).not.toBeNull()
    expect(rejectBtn.getAttribute('disabled')).not.toBeNull()
  })

  it('disables both buttons when reject is pending', () => {
    mockUseRejectTransferSuggestion.mockReturnValue({
      mutate: () => {},
      mutateAsync: () => Promise.resolve({}),
      isPending: true,
      isError: false,
    })
    renderWithQuery(
      <NotificationAcceptReject suggestionId="sugg-1" amount={200} currencyCode="USD" />
    )
    const acceptBtn = screen.getByRole('button', { name: /accept/i })
    const rejectBtn = screen.getByRole('button', { name: /reject/i })
    expect(acceptBtn.getAttribute('disabled')).not.toBeNull()
    expect(rejectBtn.getAttribute('disabled')).not.toBeNull()
  })

  // C8: analytics events fire with amount + source on onSuccess.
  // Previous tests called the mutate mock but never drove the onSuccess callback,
  // so analytics were never actually tested.
  it('fires TRANSFER_SUGGESTION_ACCEPTED with amount and source on accept success', () => {
    let capturedOnSuccess: ((data: { suggestion?: { amount?: number; currency_code?: string } }) => void) | undefined
    mockUseAcceptTransferSuggestion.mockImplementation(
      (options?: { mutation?: { onSuccess?: (data: unknown) => void } }) => {
        capturedOnSuccess = options?.mutation?.onSuccess as typeof capturedOnSuccess
        return {
          mutate: mock((_args: unknown) => {
            capturedOnSuccess?.({ suggestion: { amount: 200, currency_code: 'USD' } })
          }),
          isPending: false,
        }
      }
    )

    renderWithQuery(
      <NotificationAcceptReject suggestionId="sugg-analytics" amount={200} currencyCode="USD" />
    )
    fireEvent.click(screen.getByRole('button', { name: /accept/i }))

    expect(mockTrackEvent).toHaveBeenCalledWith(
      'transfer_suggestion_accepted',
      expect.objectContaining({ amount: 200, source: 'notification_center' })
    )
  })

  it('fires TRANSFER_SUGGESTION_REJECTED with amount and source on reject success', () => {
    let capturedOnSuccess: (() => void) | undefined
    mockUseRejectTransferSuggestion.mockImplementation(
      (options?: { mutation?: { onSuccess?: () => void } }) => {
        capturedOnSuccess = options?.mutation?.onSuccess
        return {
          mutate: mock((_args: unknown) => { capturedOnSuccess?.() }),
          isPending: false,
        }
      }
    )

    renderWithQuery(
      <NotificationAcceptReject suggestionId="sugg-analytics-reject" amount={150} currencyCode="USD" />
    )
    fireEvent.click(screen.getByRole('button', { name: /reject/i }))

    expect(mockTrackEvent).toHaveBeenCalledWith(
      'transfer_suggestion_rejected',
      expect.objectContaining({ amount: 150, source: 'notification_center' })
    )
  })
})
