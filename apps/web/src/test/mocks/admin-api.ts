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

// Single mock.module() call - called once at preload time
mock.module('@boostedbudget/api-client/endpoints/admin/admin', () => ({
  ...RealAdminModule,
  useCheckRole: mockUseCheckRole,
}))
