// Issue #974: Centralized regions API mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'
import * as RealRegionsModule from '@boostedbudget/api-client/endpoints/regions/regions'

// Two regions by default so "change region" tests can select a second option
const _defaultRegionsData = {
  regions: {
    iad: { code: 'iad', display_name: 'USA (Virginia)', country: 'USA', city: 'Virginia', flag: '🇺🇸' },
    ewr: { code: 'ewr', display_name: 'USA (New Jersey)', country: 'USA', city: 'New Jersey', flag: '🇺🇸' },
  },
  default_region: 'iad',
  server_region: 'iad',
}

export const mockRefetchRegions = mock(() => Promise.resolve())
export const mockUseGetRegions = mock(() => ({
  data: _defaultRegionsData,
  isLoading: false,
  error: null,
  refetch: mockRefetchRegions,
}))

// Single mock.module() call - called once at preload time
mock.module('@boostedbudget/api-client/endpoints/regions/regions', () => ({
  ...RealRegionsModule,
  useGetRegions: mockUseGetRegions,
}))
