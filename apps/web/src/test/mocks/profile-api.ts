// Issue #575: Centralized profile API mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'
import * as RealProfileModule from '@boostedbudget/api-client/endpoints/profile/profile'

// === useGetUserProfile ===
export const mockRefetchUserProfile = mock(() => Promise.resolve())
export const mockUseGetUserProfile = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetchUserProfile,
}))

// === useUpdateUserProfile ===
export const mockUpdateUserProfileMutate = mock(() => {})
export const mockUpdateUserProfileMutateAsync = mock(() => Promise.resolve())
let _isPending = false
export const setMockUpdateUserProfileIsPending = (value: boolean) => {
  _isPending = value
}
export const mockUseUpdateUserProfile = mock(() => ({
  mutate: mockUpdateUserProfileMutate,
  mutateAsync: mockUpdateUserProfileMutateAsync,
  isPending: _isPending,
  isError: false,
  isSuccess: false,
  error: null,
  data: null,
  reset: mock(() => {}),
}))

// Single mock.module() call - called once at preload time
mock.module('@boostedbudget/api-client/endpoints/profile/profile', () => ({
  ...RealProfileModule,
  useGetUserProfile: mockUseGetUserProfile,
  useUpdateUserProfile: mockUseUpdateUserProfile,
}))
