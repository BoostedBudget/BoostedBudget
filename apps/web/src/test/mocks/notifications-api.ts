// Issue #952: Centralized notifications API mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'

import * as RealNotificationsModule from '@boostedbudget/api-client/endpoints/notifications/notifications'

// === Query key helpers (pass through real implementations) ===
export const getGetNotificationsQueryKey = RealNotificationsModule.getGetNotificationsQueryKey
export const getGetUnreadNotificationCountQueryKey = RealNotificationsModule.getGetUnreadNotificationCountQueryKey

// === useGetNotifications ===
export const mockUseGetNotifications = mock(() => ({
  data: null,
  isLoading: false,
  error: null,
}))

// === useGetUnreadNotificationCount ===
export const mockUseGetUnreadNotificationCount = mock(() => ({
  data: { count: 0 },
  isLoading: false,
  error: null,
}))

// === useMarkNotificationAsRead ===
export const mockMarkNotificationAsReadMutateAsync = mock(() => Promise.resolve({}))
export const mockUseMarkNotificationAsRead = mock(() => ({
  mutate: mock(() => {}),
  mutateAsync: mockMarkNotificationAsReadMutateAsync,
  isPending: false,
  isError: false,
}))

// === useMarkAllNotificationsAsRead ===
export const mockMarkAllAsReadMutateAsync = mock(() => Promise.resolve({}))
export const mockUseMarkAllNotificationsAsRead = mock(() => ({
  mutate: mock(() => {}),
  mutateAsync: mockMarkAllAsReadMutateAsync,
  isPending: false,
  isError: false,
}))

mock.module('@boostedbudget/api-client/endpoints/notifications/notifications', () => ({
  ...RealNotificationsModule,
  useGetNotifications: mockUseGetNotifications,
  useGetUnreadNotificationCount: mockUseGetUnreadNotificationCount,
  useMarkNotificationAsRead: mockUseMarkNotificationAsRead,
  useMarkAllNotificationsAsRead: mockUseMarkAllNotificationsAsRead,
}))
