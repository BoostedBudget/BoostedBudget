// Issue #642: Centralized admin API mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'
import * as RealAdminModule from '@boostedbudget/api-client/endpoints/admin/admin'

// === useCheckRole ===
export const mockRefetchCheckRole = mock(() => Promise.resolve())
export const mockUseCheckRole = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetchCheckRole,
}))

// === useListAdminUsers (Issue #644) ===
export const mockUseListAdminUsers = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
}))

// === useGetAdminUserDetail (Issue #644) ===
export const mockUseGetAdminUserDetail = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
}))

// === useGetSystemHealth ===
export const mockRefetchGetSystemHealth = mock(() => Promise.resolve())
export const mockUseGetSystemHealth = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetchGetSystemHealth,
}))

// === useListAdminPaddleWebhooks (Issue #747) ===
export const mockUseListAdminPaddleWebhooks = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
}))

// === useGetAdminPaddleWebhookStats (Issue #747) ===
export const mockUseGetAdminPaddleWebhookStats = mock(() => ({
  data: undefined,
  isLoading: false,
  error: null,
}))

// === useGetAdminPaddleWebhookDetail (Issue #747) ===
export const mockUseGetAdminPaddleWebhookDetail = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
}))

// === useReplayAdminPaddleWebhook (Issue #747) ===
export const mockUseReplayAdminPaddleWebhook = mock(() => ({
  mutate: mock(() => {}),
  isPending: false,
  isSuccess: false,
  isError: false,
}))

// === DLQ hooks (Issue #633) ===
export const mockUseListAdminDeadLetterJobs = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
}))

export const mockUseGetAdminDeadLetterJobStats = mock(() => ({
  data: undefined,
  isLoading: false,
  error: null,
}))

export const mockUseGetAdminDeadLetterJobDetail = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
}))

export const mockUseGetAdminDeadLetterJobArgs = mock(() => ({
  data: undefined,
  isLoading: false,
  error: null,
  refetch: mock(() => Promise.resolve()),
}))

export const mockUseRetryAdminDeadLetterJob = mock(() => ({
  mutate: mock(() => {}),
  isPending: false,
  isSuccess: false,
  isError: false,
}))

export const mockUseAddAdminDeadLetterJobNotes = mock(() => ({
  mutate: mock(() => {}),
  isPending: false,
}))

export const mockUseDeleteAdminDeadLetterJob = mock(() => ({
  mutate: mock(() => {}),
  isPending: false,
}))

export const mockUseBulkDeleteAdminDeadLetterJobs = mock(() => ({
  mutate: mock(() => {}),
  isPending: false,
}))

// Single mock.module() call - called once at preload time
mock.module('@boostedbudget/api-client/endpoints/admin/admin', () => ({
  ...RealAdminModule,
  useCheckRole: mockUseCheckRole,
  useListAdminUsers: mockUseListAdminUsers,
  useGetAdminUserDetail: mockUseGetAdminUserDetail,
  useGetSystemHealth: mockUseGetSystemHealth,
  useListAdminPaddleWebhooks: mockUseListAdminPaddleWebhooks,
  useGetAdminPaddleWebhookStats: mockUseGetAdminPaddleWebhookStats,
  useGetAdminPaddleWebhookDetail: mockUseGetAdminPaddleWebhookDetail,
  useReplayAdminPaddleWebhook: mockUseReplayAdminPaddleWebhook,
  useListAdminDeadLetterJobs: mockUseListAdminDeadLetterJobs,
  useGetAdminDeadLetterJobStats: mockUseGetAdminDeadLetterJobStats,
  useGetAdminDeadLetterJobDetail: mockUseGetAdminDeadLetterJobDetail,
  useGetAdminDeadLetterJobArgs: mockUseGetAdminDeadLetterJobArgs,
  useRetryAdminDeadLetterJob: mockUseRetryAdminDeadLetterJob,
  useAddAdminDeadLetterJobNotes: mockUseAddAdminDeadLetterJobNotes,
  useDeleteAdminDeadLetterJob: mockUseDeleteAdminDeadLetterJob,
  useBulkDeleteAdminDeadLetterJobs: mockUseBulkDeleteAdminDeadLetterJobs,
}))
