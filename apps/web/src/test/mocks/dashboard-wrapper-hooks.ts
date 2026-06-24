// Issue #575 / #1323: Dashboard wrapper hook mock exports AND module registrations
//
// This file is preloaded via bunfig.toml. It:
// 1. Creates named mock function exports (used in beforeEach/mockReturnValue chains)
// 2. Registers mock.module() for each wrapper hook so component tests don't need
//    inline mock.module() calls (which are process-global and pollute subsequent tests)
//
// Hook implementation tests (useDashboardData.test.tsx, useDashboardLayout.test.tsx,
// useDashboardOnboarding.test.ts, useGettingStarted.test.ts) need REAL implementations
// and run via test-real-implementations.sh (from /tmp, bypassing bunfig.toml preloads).

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

// === useDashboardOnboarding mock ===
export const mockDismissHint = mock(() => {})
export const mockUseDashboardOnboarding = mock(() => ({
  showHint: false,
  dismissHint: mockDismissHint,
}))

// === useGettingStarted mock ===
export const mockDismissGettingStarted = mock(() => {})
export const mockUseGettingStarted = mock(() => ({
  steps: [],
  completedCount: 0,
  totalSteps: 5,
  allComplete: false,
  shouldShow: false,
  isLoading: false,
  isCelebrating: false,
  dismiss: mockDismissGettingStarted,
}))

// === Centralized mock.module() registrations (Issue #1323) ===
// Registered here at preload time so component tests don't need inline mock.module()
// calls. Inline mock.module() is process-global and pollutes subsequent test files.
//
// Hook implementation tests that need real implementations run via
// test-real-implementations.sh (from /tmp, bypassing bunfig.toml preloads).

mock.module('@/features/dashboard/hooks/useDashboardData', () => ({
  useDashboardData: (...args: unknown[]) => mockUseDashboardData(...args),
}))

mock.module('@/features/dashboard/hooks/useDashboardLayout', () => ({
  useDashboardLayout: (...args: unknown[]) => mockUseDashboardLayout(...args),
}))

mock.module('@/features/dashboard/hooks/useDashboardOnboarding', () => ({
  useDashboardOnboarding: (...args: unknown[]) => mockUseDashboardOnboarding(...args),
}))

mock.module('@/features/dashboard/hooks/useGettingStarted', () => ({
  useGettingStarted: (...args: unknown[]) => mockUseGettingStarted(...args),
}))
