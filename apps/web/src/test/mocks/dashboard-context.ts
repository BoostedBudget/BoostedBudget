// Issue #575: Centralized dashboard context mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests
// IMPORTANT: Re-exports all real exports, only overrides useDashboardContext

import { mock } from 'bun:test'
import * as RealDashboardContext from '@/features/dashboard/context/DashboardContext'
import type { DashboardContextValue } from '@/features/dashboard/context/DashboardContext'

// Configurable mock function - tests customize in beforeEach
export const mockSetFilters = mock(() => {})
export const mockSetTimeframe = mock(() => {})
export const mockSetIsEditing = mock(() => {})
// Issue #1370: Household feature — Personal/Household scope toggle
export const mockSetScope = mock(() => {})

// Return type is deliberately widened to `Partial<DashboardContextValue>` (rather
// than inferred as the exact literal below) so that the MANY existing test files
// calling `mockUseDashboardContext.mockReturnValue({...})` with a subset of fields
// (predating the `scope`/`setScope` fields added for Issue #1370) keep compiling
// without every one of them needing updates. Consumers of the REAL hook still see
// the full `DashboardContextValue` type (this widening only affects test literals
// checked against this mock's `mockReturnValue` parameter type).
export const mockUseDashboardContext = mock((): Partial<DashboardContextValue> => ({
  filters: {
    startDate: '2024-01-01',
    endDate: '2024-01-31',
    accountTypes: undefined,
  },
  setFilters: mockSetFilters,
  timeframe: 'this_month' as const,
  setTimeframe: mockSetTimeframe,
  isEditing: false,
  setIsEditing: mockSetIsEditing,
  hasAccounts: false,
  accountsLoading: false,
  scope: 'personal',
  setScope: mockSetScope,
}))

// Single mock.module() calls - called once at preload time
mock.module('@/features/dashboard/context/DashboardContext', () => ({
  ...RealDashboardContext,
  useDashboardContext: mockUseDashboardContext,
}))

// Also mock relative path imports (widget tests use relative paths)
mock.module('../context/DashboardContext', () => ({
  ...RealDashboardContext,
  useDashboardContext: mockUseDashboardContext,
}))
