/**
 * NotificationAcceptReject Tests
 * Issue #952: Notification Center — Accept/Reject flow for transfer suggestions
 */

import { describe, it, expect, beforeEach } from 'bun:test'
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

describe('NotificationAcceptReject', () => {
  beforeEach(() => {
    mockUseAcceptTransferSuggestion.mockClear()
    mockUseRejectTransferSuggestion.mockClear()
    mockAcceptTransferSuggestionMutateAsync.mockClear()
    mockRejectTransferSuggestionMutateAsync.mockClear()
    mockToastSuccess.mockClear()

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
})
