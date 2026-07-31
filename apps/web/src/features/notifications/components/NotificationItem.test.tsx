/**
 * NotificationItem Tests
 * Issue #952: Notification Center — All 4 interaction patterns
 * Issue #972: Confirm-match mark-read and error-path (GAP AA)
 */

import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { screen, fireEvent } from '@testing-library/react'
import React from 'react'
import { MemoryRouter } from 'react-router'
import { NotificationItem } from './NotificationItem'
import type { Notification } from '@boostedbudget/api-client/models'
import { mockUseMarkNotificationAsRead } from '@/test/mocks/notifications-api'
import { mockUseConfirmTransferSuggestionMatch } from '@/test/mocks/transfer-suggestions-api'
import { mockToastError } from '@/test/mocks/sonner'
import { renderWithQueryAndWrapper } from '@/test/utils/queryTestUtils'

const baseNotification: Notification = {
  id: 'notif-1',
  user_id: 'user-1',
  type: 'transaction_sync',
  title: 'Sync Complete',
  message: 'Your transactions have been synced.',
  interaction_pattern: 'informational',
  read: false,
  sent_email: false,
  created_at: new Date(Date.now() - 60000).toISOString(),
  updated_at: new Date().toISOString(),
}

function renderItem(notification: Notification) {
  return renderWithQueryAndWrapper(
    <NotificationItem notification={notification} />,
    MemoryRouter
  )
}

describe('NotificationItem', () => {
  beforeEach(() => {
    mockUseMarkNotificationAsRead.mockClear()
    mockUseMarkNotificationAsRead.mockReturnValue({
      mutate: () => {},
      mutateAsync: () => Promise.resolve({}),
      isPending: false,
      isError: false,
    })
  })

  it('renders title and message', () => {
    renderItem(baseNotification)
    expect(screen.getByText('Sync Complete')).toBeTruthy()
    expect(screen.getByText('Your transactions have been synced.')).toBeTruthy()
  })

  it('shows unread styling when not read', () => {
    renderItem(baseNotification)
    const item = screen.getByTestId('notification-item')
    expect(item.className).toContain('bg-accent')
  })

  it('shows read styling when read', () => {
    renderItem({ ...baseNotification, read: true })
    const item = screen.getByTestId('notification-item')
    expect(item.className).not.toContain('bg-accent')
  })

  it('renders action link for action_required pattern', () => {
    renderItem({
      ...baseNotification,
      interaction_pattern: 'action_required',
      action_url: '/billing',
      action_label: 'Update Payment',
    })
    expect(screen.getByRole('link', { name: /update payment/i })).toBeTruthy()
  })

  it('does not render action link for informational pattern', () => {
    renderItem(baseNotification)
    expect(screen.queryByRole('link')).toBeNull()
  })

  it('renders accept/reject buttons for accept_reject pattern', () => {
    renderItem({
      ...baseNotification,
      type: 'transfer_suggestion',
      interaction_pattern: 'accept_reject',
      transfer_suggestion_id: 'sugg-1',
    })
    expect(screen.getByRole('button', { name: /accept/i })).toBeTruthy()
    expect(screen.getByRole('button', { name: /reject/i })).toBeTruthy()
  })

  it('renders confirm-match UI for transfer_suggestion_match type', () => {
    renderItem({
      ...baseNotification,
      type: 'transfer_suggestion_match',
      interaction_pattern: 'accept_reject',
      transfer_suggestion_id: 'sugg-1',
    })
    expect(screen.getByRole('spinbutton')).toBeTruthy()
    expect(screen.getByRole('button', { name: /confirm/i })).toBeTruthy()
    expect(screen.getByRole('button', { name: /dismiss/i })).toBeTruthy()
  })

  it('shows relative timestamp', () => {
    renderItem(baseNotification)
    expect(screen.getByText(/m ago|just now/i)).toBeTruthy()
  })

  // GAP AA: confirm-match should mark the notification read with the correct id.
  // RED: fails today because the mutate mock is a no-op and no assertion existed.
  // GREEN: wire onConfirm → markReadMutation.mutate({ id }) + add onError toast.
  it('marks notification as read when confirm-match succeeds', () => {
    // Capture the onSuccess callback wired inside NotificationConfirmMatch.
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

    const markReadSpy = mock(() => {})
    mockUseMarkNotificationAsRead.mockReturnValue({
      mutate: markReadSpy,
      mutateAsync: () => Promise.resolve({}),
      isPending: false,
      isError: false,
    })

    const notification: Notification = {
      ...baseNotification,
      id: 'notif-confirm-read',
      type: 'transfer_suggestion_match',
      interaction_pattern: 'accept_reject',
      transfer_suggestion_id: 'sugg-confirm-1',
    }

    renderItem(notification)

    // Enter a valid amount and click Confirm.
    fireEvent.change(screen.getByRole('spinbutton'), { target: { value: '150' } })
    fireEvent.click(screen.getByRole('button', { name: /confirm/i }))

    // onSuccess fires onConfirm → markReadMutation.mutate({ id: 'notif-confirm-read' })
    expect(markReadSpy).toHaveBeenCalledWith({ id: 'notif-confirm-read' })
  })

  // GAP AA: when mark-read fails after confirm, a toast error should surface
  // (bell should not silently stay elevated without feedback).
  // RED: fails today because markReadMutation has no onError handler.
  // GREEN: add onError: () => toast.error(...) to markReadMutation.
  it('shows error toast when mark-read fails after confirm', () => {
    // Set up confirm mutation to capture onSuccess and fire it.
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

    // Set up mark-read to fire onError (simulating a network failure).
    let capturedMarkReadOnError: (() => void) | undefined
    mockUseMarkNotificationAsRead.mockImplementation(
      (options?: { mutation?: { onSuccess?: () => void; onError?: () => void } }) => {
        capturedMarkReadOnError = options?.mutation?.onError
        return {
          mutate: mock((_args: unknown) => { capturedMarkReadOnError?.() }),
          isPending: false,
          isError: false,
        }
      }
    )

    mockToastError.mockClear()

    const notification: Notification = {
      ...baseNotification,
      id: 'notif-mark-err',
      type: 'transfer_suggestion_match',
      interaction_pattern: 'accept_reject',
      transfer_suggestion_id: 'sugg-err-1',
    }

    renderItem(notification)

    fireEvent.change(screen.getByRole('spinbutton'), { target: { value: '100' } })
    fireEvent.click(screen.getByRole('button', { name: /confirm/i }))

    // onConfirm fires → markReadMutation.mutate → onError fires → toast.error
    expect(mockToastError).toHaveBeenCalledTimes(1)
  })
})
