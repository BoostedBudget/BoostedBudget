// Issue #575: Centralized dashboard context mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests
// IMPORTANT: Re-exports all real exports, only overrides useDashboardContext

import { mock } from 'bun:test'
import * as RealDashboardContext from '@/features/dashboard/context/DashboardContext'

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
// Re-export ALL real exports, only override useDashboardContext
mock.module('@/features/dashboard/context/DashboardContext', () => ({
  ...RealDashboardContext,
  useDashboardContext: () => mockUseDashboardContext(),
}))

// Also mock relative path imports (widget tests use relative paths)
mock.module('../context/DashboardContext', () => ({
  ...RealDashboardContext,
  useDashboardContext: () => mockUseDashboardContext(),
}))
