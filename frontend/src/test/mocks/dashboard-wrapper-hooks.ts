// Issue #575: Dashboard wrapper hook mock exports
// These are EXPORTS ONLY - NOT globally registered via mock.module()
//
// The hook implementation tests (useDashboardData.test.tsx, useDashboardLayout.test.tsx,
// useDashboardOnboarding.test.tsx) need to test the REAL hook implementations with
// API-level mocks from dashboard-api.ts.
//
// Component tests that need wrapper-level mocks should import these exports and
// apply mock.module() inline BEFORE importing the component.
//
// Example usage in component tests:
//   import { mockUseDashboardLayout, ... } from '@/test/mocks/dashboard-wrapper-hooks'
//   mock.module('@/features/dashboard/hooks/useDashboardLayout', () => ({
//     useDashboardLayout: () => mockUseDashboardLayout(),
//   }))
//   // NOW import the component

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
