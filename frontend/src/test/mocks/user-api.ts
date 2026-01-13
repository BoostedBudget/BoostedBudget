// Issue #575: Centralized user API mock (deletion endpoints) to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'
import * as RealUserModule from '@/api/generated/endpoints/user/user'

// === useRequestDeletion ===
export const mockRequestDeletionMutateAsync = mock(() =>
  Promise.resolve({
    message: 'Account deletion scheduled',
    scheduled_at: '2025-11-26T00:00:00Z',
    cancellation_token: 'test-token',
  })
)
export const mockUseRequestDeletion = mock(() => ({
  mutateAsync: mockRequestDeletionMutateAsync,
  isPending: false,
  isError: false,
  error: null,
}))

// === cancelDeletion (raw function, not a hook) ===
export const mockCancelDeletion = mock(() =>
  Promise.resolve({
    message: 'Account deletion canceled successfully',
  })
)

// === useCancelDeletion (hook wrapper, if needed) ===
export const mockRefetchCancelDeletion = mock(() => Promise.resolve())
export const mockUseCancelDeletion = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetchCancelDeletion,
}))

// Single mock.module() call - called once at preload time
mock.module('@/api/generated/endpoints/user/user', () => ({
  ...RealUserModule,
  useRequestDeletion: mockUseRequestDeletion,
  cancelDeletion: mockCancelDeletion,
  useCancelDeletion: mockUseCancelDeletion,
}))
