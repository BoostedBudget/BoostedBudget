// Issue #972: Centralized savings impact hook mocks
// These mock the WRAPPER hook (useSavingsImpactData) for component tests.

import { mock } from 'bun:test'

// === useSavingsImpactData mock ===
export const mockUseSavingsImpactData = mock(() => ({
  data: null,
  loading: false,
  error: null,
}))

// Mock both absolute and relative paths
mock.module('@/features/dashboard/hooks/useSavingsImpactData', () => ({
  useSavingsImpactData: (...args: unknown[]) => mockUseSavingsImpactData(...args),
}))

mock.module('../hooks/useSavingsImpactData', () => ({
  useSavingsImpactData: (...args: unknown[]) => mockUseSavingsImpactData(...args),
}))
