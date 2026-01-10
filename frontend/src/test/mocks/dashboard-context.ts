// Issue #575: Centralized dashboard context mock to prevent mock.module() pollution
// Simplified version that doesn't use React directly in preload

import { mock } from 'bun:test'

// Configurable mock function - tests customize in beforeEach
export const mockSetFilters = mock(() => {})
export const mockSetTimeframe = mock(() => {})
export const mockSetIsEditing = mock(() => {})

export const mockUseDashboardContext = mock(() => ({
  filters: {
    startDate: '2024-01-01',
    endDate: '2024-01-31',
    accountTypes: undefined,
  },
  setFilters: mockSetFilters,
  timeframe: 'month' as const,
  setTimeframe: mockSetTimeframe,
  isEditing: false,
  setIsEditing: mockSetIsEditing,
}))

// Single mock.module() call - called once at preload time
mock.module('@/features/dashboard/context/DashboardContext', () => ({
  useDashboardContext: () => mockUseDashboardContext(),
}))

// Also mock relative path imports (widget tests use relative paths)
mock.module('../context/DashboardContext', () => ({
  useDashboardContext: () => mockUseDashboardContext(),
}))
