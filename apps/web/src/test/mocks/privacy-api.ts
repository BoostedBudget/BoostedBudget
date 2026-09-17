// Issue #575: Centralized privacy API mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'
import * as RealPrivacyModule from '@boostedbudget/api-client/endpoints/privacy/privacy'

type MockPrivacySettings = Record<string, boolean>

interface MockPrivacyQueryOptions {
  query?: {
    enabled?: boolean
    queryKey?: readonly unknown[]
  }
}

interface MockPrivacyQueryResult {
  data?: MockPrivacySettings
  error: Error | null
  isFetching?: boolean
  isLoading: boolean
  refetch: () => Promise<unknown>
}

// === useGetPrivacySettings ===
export const mockRefetchPrivacySettings = mock((): Promise<unknown> => Promise.resolve())
export const mockUseGetPrivacySettings = mock((_options?: MockPrivacyQueryOptions): MockPrivacyQueryResult => ({
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
export const mockUpdatePrivacySettingsMutateAsync = mock((_variables?: unknown): Promise<MockPrivacySettings> => Promise.resolve({}))
export const mockUpdatePrivacySettingsMutate = mock(() => {})
export const mockUseUpdatePrivacySettings = mock((): {
  data: MockPrivacySettings | null
  error: Error | null
  isError: boolean
  isPending: boolean
  isSuccess: boolean
  mutate: typeof mockUpdatePrivacySettingsMutate
  mutateAsync: typeof mockUpdatePrivacySettingsMutateAsync
  reset: () => void
} => ({
  mutate: mockUpdatePrivacySettingsMutate,
  mutateAsync: mockUpdatePrivacySettingsMutateAsync,
  isPending: false,
  isError: false,
  isSuccess: false,
  error: null,
  data: null,
  reset: mock(() => {}),
}))

// === getDataExport (Issue #1587, GDPR Article 20) ===
// The IMPERATIVE function, not a query hook: the export is requested on a click, never on mount.
// It lives in this file rather than in its own because mock.module() is keyed by specifier and
// process-global — a second call for this same specifier elsewhere would replace, not merge with,
// the overrides above.
export const mockGetDataExport = mock(() =>
  Promise.resolve({
    export_version: 1,
    generated_at: '2026-08-25T12:00:00Z',
    user_id: 'user-123',
    sections: {},
    excluded_tables: {},
    row_count: 0,
  })
)

// Single mock.module() call - called once at preload time
mock.module('@boostedbudget/api-client/endpoints/privacy/privacy', () => ({
  ...RealPrivacyModule,
  useGetPrivacySettings: mockUseGetPrivacySettings,
  useUpdatePrivacySettings: mockUseUpdatePrivacySettings,
  getDataExport: mockGetDataExport,
}))
