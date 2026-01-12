// Issue #575: Centralized dashboard wrapper hook mocks
// These mock the WRAPPER hooks (useDashboardData, useDashboardLayout, useDashboardOnboarding)
// for component tests that need to test in isolation from the actual hook implementation.
//
// NOTE: Hook implementation tests (e.g., useDashboardData.test.tsx) use API-level mocks
// from dashboard-api.ts instead, so they test real hook behavior.

import { mock } from 'bun:test'
import type { WidgetType } from '@/features/dashboard/types'

// === useDashboardData mock ===
export const mockDashboardDataRefetch = mock(() => {})
export const mockUseDashboardData = mock(() => ({
  summary: null,
  history: null,
  accountTypes: null,
  loading: false,
  error: null,
  refetch: mockDashboardDataRefetch,
}))

// Mock both absolute and relative paths
mock.module('@/features/dashboard/hooks/useDashboardData', () => ({
  useDashboardData: (...args: unknown[]) => mockUseDashboardData(...args),
}))

mock.module('../hooks/useDashboardData', () => ({
  useDashboardData: (...args: unknown[]) => mockUseDashboardData(...args),
}))

// === useDashboardLayout mock ===
export const mockUpdateLayout = mock(() => {})
export const mockAddWidget = mock(() => {})
export const mockRemoveWidget = mock(() => {})
export const mockResetToDefault = mock(() => {})
export const mockSaveNow = mock(() => {})

export const DEFAULT_LAYOUT_MOCK = {
  widgets: [
    {
      i: 'balance-overview-1',
      type: 'balance-overview' as WidgetType,
      x: 0,
      y: 0,
      w: 4,
      h: 2,
    },
    {
      i: 'cash-flow-summary-1',
      type: 'cash-flow-summary' as WidgetType,
      x: 4,
      y: 0,
      w: 8,
      h: 2,
    },
    {
      i: 'balance-history-1',
      type: 'balance-history' as WidgetType,
      x: 0,
      y: 2,
      w: 6,
      h: 4,
    },
    {
      i: 'cash-flow-chart-1',
      type: 'cash-flow-chart' as WidgetType,
      x: 6,
      y: 2,
      w: 6,
      h: 4,
    },
    { i: 'account-cards-1', type: 'account-cards' as WidgetType, x: 0, y: 6, w: 12, h: 3 },
  ],
  version: 4,
}

export const mockUseDashboardLayout = mock(() => ({
  layout: DEFAULT_LAYOUT_MOCK,
  loading: false,
  error: null,
  saving: false,
  updateLayout: mockUpdateLayout,
  addWidget: mockAddWidget,
  removeWidget: mockRemoveWidget,
  resetToDefault: mockResetToDefault,
  saveNow: mockSaveNow,
}))

mock.module('@/features/dashboard/hooks/useDashboardLayout', () => ({
  useDashboardLayout: (...args: unknown[]) => mockUseDashboardLayout(...args),
}))

mock.module('../hooks/useDashboardLayout', () => ({
  useDashboardLayout: (...args: unknown[]) => mockUseDashboardLayout(...args),
}))

// === useDashboardOnboarding mock ===
export const mockDismissHint = mock(() => {})
export const mockUseDashboardOnboarding = mock(() => ({
  showHint: false,
  dismissHint: mockDismissHint,
}))

mock.module('@/features/dashboard/hooks/useDashboardOnboarding', () => ({
  useDashboardOnboarding: (...args: unknown[]) => mockUseDashboardOnboarding(...args),
}))

mock.module('../hooks/useDashboardOnboarding', () => ({
  useDashboardOnboarding: (...args: unknown[]) => mockUseDashboardOnboarding(...args),
}))
