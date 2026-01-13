// Issue #575: Centralized subscription API mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'
import * as RealSubscriptionModule from '@/api/generated/endpoints/subscription/subscription'

// === useGetSubscriptionTiers ===
export const mockRefetchSubscriptionTiers = mock(() => Promise.resolve())
export const mockUseGetSubscriptionTiers = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetchSubscriptionTiers,
}))

// Single mock.module() call - called once at preload time
mock.module('@/api/generated/endpoints/subscription/subscription', () => ({
  ...RealSubscriptionModule,
  useGetSubscriptionTiers: mockUseGetSubscriptionTiers,
}))
