/**
 * NotificationBell Tests
 * Issue #952: Notification Center — Bell icon with unread badge
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { screen, fireEvent } from '@testing-library/react'
import React from 'react'
import { NotificationBell } from './NotificationBell'
import { mockUseGetUnreadNotificationCount } from '@/test/mocks/notifications-api'
import { renderWithQuery } from '@/test/utils/queryTestUtils'

describe('NotificationBell', () => {
  beforeEach(() => {
    mockUseGetUnreadNotificationCount.mockClear()
    mockUseGetUnreadNotificationCount.mockReturnValue({
      data: { count: 0 },
      isLoading: false,
      error: null,
    })
  })

  it('renders bell button', () => {
    renderWithQuery(<NotificationBell onOpen={() => {}} />)
    expect(screen.getByRole('button', { name: /notifications/i })).toBeTruthy()
  })

  it('hides badge when unread count is 0', () => {
    renderWithQuery(<NotificationBell onOpen={() => {}} />)
    const badge = screen.queryByTestId('unread-badge')
    expect(badge).toBeNull()
  })

  it('shows badge with count when unread notifications exist', () => {
    mockUseGetUnreadNotificationCount.mockReturnValue({
      data: { count: 5 },
      isLoading: false,
      error: null,
    })
    renderWithQuery(<NotificationBell onOpen={() => {}} />)
    const badge = screen.getByTestId('unread-badge')
    expect(badge.textContent).toBe('5')
  })

  it('calls onOpen when bell is clicked', () => {
    let called = false
    renderWithQuery(<NotificationBell onOpen={() => { called = true }} />)
    fireEvent.click(screen.getByRole('button', { name: /notifications/i }))
    expect(called).toBe(true)
  })

  it('caps badge display at 99 for large counts', () => {
    mockUseGetUnreadNotificationCount.mockReturnValue({
      data: { count: 150 },
      isLoading: false,
      error: null,
    })
    renderWithQuery(<NotificationBell onOpen={() => {}} />)
    const badge = screen.getByTestId('unread-badge')
    expect(badge.textContent).toBe('99+')
  })
})
