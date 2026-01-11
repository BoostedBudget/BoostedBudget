// Issue #575: Centralized privacy API mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'
import * as RealPrivacyModule from '@/api/generated/endpoints/privacy/privacy'

// === useGetPrivacySettings ===
export const mockRefetchPrivacySettings = mock(() => Promise.resolve())
export const mockUseGetPrivacySettings = mock(() => ({
  data: {
    analytics_enabled: true,
    data_sharing_enabled: false,
    ad_personalization_enabled: true,
  },
  isLoading: false,
  error: null,
  refetch: mockRefetchPrivacySettings,
}))

// === useUpdatePrivacySettings ===
export const mockUpdatePrivacySettingsMutateAsync = mock(() => Promise.resolve())
export const mockUpdatePrivacySettingsMutate = mock(() => {})
export const mockUseUpdatePrivacySettings = mock(() => ({
  mutate: mockUpdatePrivacySettingsMutate,
  mutateAsync: mockUpdatePrivacySettingsMutateAsync,
  isPending: false,
  isError: false,
  isSuccess: false,
  error: null,
  data: null,
  reset: mock(() => {}),
}))

// Single mock.module() call - called once at preload time
mock.module('@/api/generated/endpoints/privacy/privacy', () => ({
  ...RealPrivacyModule,
  useGetPrivacySettings: (...args: unknown[]) => mockUseGetPrivacySettings(...args),
  useUpdatePrivacySettings: (...args: unknown[]) => mockUseUpdatePrivacySettings(...args),
}))
