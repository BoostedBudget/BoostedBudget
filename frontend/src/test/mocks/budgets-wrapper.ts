// Issue #575: Centralized useBudgets wrapper hook mock
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'

// Default mock values - can be configured per test via mockReturnValue
export const mockUseBudgets = mock(() => ({
  budgets: [
    { id: 'budget-1', name: 'Monthly Budget', categories: [] },
  ],
  loading: false,
  error: null,
  refetch: mock(() => Promise.resolve()),
}))

// Single mock.module() call - called once at preload time
mock.module('@/features/budgets/hooks/useBudgets', () => ({
  useBudgets: (...args: unknown[]) => mockUseBudgets(...args),
}))
