// Issue #575: Centralized profile API mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'
import * as RealProfileModule from '@/api/generated/endpoints/profile/profile'

// === useGetUserProfile ===
export const mockRefetchUserProfile = mock(() => Promise.resolve())
export const mockUseGetUserProfile = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetchUserProfile,
}))

// === useUpdateUserProfile ===
export const mockUpdateUserProfileMutateAsync = mock(() => Promise.resolve())
export const mockUpdateUserProfileMutate = mock(() => {})
export let mockUpdateUserProfileIsPending = false
export const setMockUpdateUserProfileIsPending = (value: boolean) => {
  mockUpdateUserProfileIsPending = value
}
export const mockUseUpdateUserProfile = mock(() => ({
  mutate: mockUpdateUserProfileMutate,
  mutateAsync: mockUpdateUserProfileMutateAsync,
  get isPending() {
    return mockUpdateUserProfileIsPending
  },
  isError: false,
  isSuccess: false,
  error: null,
  data: null,
  reset: mock(() => {}),
}))

// Single mock.module() call - called once at preload time
mock.module('@/api/generated/endpoints/profile/profile', () => ({
  ...RealProfileModule,
  useGetUserProfile: (...args: unknown[]) => mockUseGetUserProfile(...args),
  useUpdateUserProfile: (...args: unknown[]) => mockUseUpdateUserProfile(...args),
}))
