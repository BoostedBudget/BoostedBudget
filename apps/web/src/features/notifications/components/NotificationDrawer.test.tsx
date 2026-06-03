/**
 * NotificationDrawer Tests
 * Issue #952: Notification Center — Sheet-based notification panel
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { screen, fireEvent } from '@testing-library/react'
import React from 'react'
import { MemoryRouter } from 'react-router-dom'
import { NotificationDrawer } from './NotificationDrawer'
import {
  mockUseGetNotifications,
  mockUseMarkAllNotificationsAsRead,
  mockUseMarkNotificationAsRead,
  mockUseGetUnreadNotificationCount,
} from '@/test/mocks/notifications-api'
import type { Notification } from '@boostedbudget/api-client/models'
import { renderWithQueryAndWrapper } from '@/test/utils/queryTestUtils'

const mockNotification: Notification = {
  id: 'notif-1',
  user_id: 'user-1',
  type: 'transaction_sync',
  title: 'Sync Complete',
  message: 'Your transactions have been synced.',
  interaction_pattern: 'informational',
  read: false,
  sent_email: false,
  created_at: new Date().toISOString(),
  updated_at: new Date().toISOString(),
}

function renderDrawer(props = { open: true, onClose: () => {} }) {
  return renderWithQueryAndWrapper(<NotificationDrawer {...props} />, MemoryRouter)
}

describe('NotificationDrawer', () => {
  beforeEach(() => {
    mockUseGetNotifications.mockClear()
    mockUseMarkAllNotificationsAsRead.mockClear()
    mockUseMarkNotificationAsRead.mockClear()
    mockUseGetUnreadNotificationCount.mockClear()

    mockUseGetNotifications.mockReturnValue({
      data: [],
      isLoading: false,
      error: null,
    })
    mockUseMarkAllNotificationsAsRead.mockReturnValue({
      mutate: () => {},
      mutateAsync: () => Promise.resolve({}),
      isPending: false,
    })
    mockUseMarkNotificationAsRead.mockReturnValue({
      mutate: () => {},
      mutateAsync: () => Promise.resolve({}),
      isPending: false,
    })
    mockUseGetUnreadNotificationCount.mockReturnValue({
      data: { count: 0 },
      isLoading: false,
      error: null,
    })
  })

  it('renders "Notifications" title', () => {
    renderDrawer()
    expect(screen.getByText('Notifications')).toBeTruthy()
  })

  it('renders empty state when no notifications', () => {
    renderDrawer()
    expect(screen.getByText(/no notifications yet/i)).toBeTruthy()
  })

  it('renders notification list when notifications exist', () => {
    mockUseGetNotifications.mockReturnValue({
      data: [mockNotification],
      isLoading: false,
      error: null,
    })
    renderDrawer()
    expect(screen.getByText('Sync Complete')).toBeTruthy()
  })

  it('shows "Mark all as read" button when unread notifications exist', () => {
    mockUseGetNotifications.mockReturnValue({
      data: [mockNotification],
      isLoading: false,
      error: null,
    })
    mockUseGetUnreadNotificationCount.mockReturnValue({
      data: { count: 1 },
      isLoading: false,
      error: null,
    })
    renderDrawer()
    expect(screen.getByRole('button', { name: /mark all as read/i })).toBeTruthy()
  })

  it('hides "Mark all as read" button when all notifications are read', () => {
    mockUseGetNotifications.mockReturnValue({
      data: [{ ...mockNotification, read: true }],
      isLoading: false,
      error: null,
    })
    mockUseGetUnreadNotificationCount.mockReturnValue({
      data: { count: 0 },
      isLoading: false,
      error: null,
    })
    renderDrawer()
    expect(screen.queryByRole('button', { name: /mark all as read/i })).toBeNull()
  })

  it('calls markAllAsRead when button is clicked', () => {
    let markAllCalled = false
    mockUseMarkAllNotificationsAsRead.mockReturnValue({
      mutate: () => { markAllCalled = true },
      mutateAsync: () => Promise.resolve({}),
      isPending: false,
    })
    mockUseGetNotifications.mockReturnValue({
      data: [mockNotification],
      isLoading: false,
      error: null,
    })
    mockUseGetUnreadNotificationCount.mockReturnValue({
      data: { count: 1 },
      isLoading: false,
      error: null,
    })
    renderDrawer()
    fireEvent.click(screen.getByRole('button', { name: /mark all as read/i }))
    expect(markAllCalled).toBe(true)
  })
})
