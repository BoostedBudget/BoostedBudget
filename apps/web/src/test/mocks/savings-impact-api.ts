// Issue #972: Centralized savings impact API mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'

import * as RealSavingsImpactModule from '@boostedbudget/api-client/endpoints/savings-impact/savings-impact'

// === useGetSavingsImpactMe ===
export const mockRefetch = mock(() => Promise.resolve())
export const mockUseGetSavingsImpactMe = mock(() => ({
  data: null,
  isLoading: false,
  error: null,
  refetch: mockRefetch,
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
