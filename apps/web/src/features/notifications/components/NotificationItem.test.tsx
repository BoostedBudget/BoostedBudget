/**
 * NotificationItem Tests
 * Issue #952: Notification Center — All 4 interaction patterns
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { screen } from '@testing-library/react'
import React from 'react'
import { MemoryRouter } from 'react-router-dom'
import { NotificationItem } from './NotificationItem'
import type { Notification } from '@boostedbudget/api-client/models'
import { mockUseMarkNotificationAsRead } from '@/test/mocks/notifications-api'
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

  it('shows relative timestamp', () => {
    renderItem(baseNotification)
    expect(screen.getByText(/m ago|just now/i)).toBeTruthy()
  })
})
