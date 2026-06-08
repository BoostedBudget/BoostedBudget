// Issue #972: Centralized savings impact API mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'

import * as RealSavingsImpactModule from '@boostedbudget/api-client/endpoints/savings-impact/savings-impact'

// === useGetSavingsImpactMe ===
export const mockUseGetSavingsImpactMe = mock(() => ({
  data: null,
  isLoading: false,
  error: null,
}))

// === useGetSavingsImpactAggregate ===
export const mockUseGetSavingsImpactAggregate = mock(() => ({
  data: null,
  isLoading: false,
  error: null,
}))

mock.module('@boostedbudget/api-client/endpoints/savings-impact/savings-impact', () => ({
  ...RealSavingsImpactModule,
  useGetSavingsImpactMe: mockUseGetSavingsImpactMe,
  useGetSavingsImpactAggregate: mockUseGetSavingsImpactAggregate,
}))
