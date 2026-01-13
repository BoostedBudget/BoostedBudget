// Issue #575: Centralized dashboard hooks mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests
//
// IMPORTANT: This file does NOT mock any hook implementations because:
// - Hook implementation tests need REAL hooks with mocked APIs
// - Component tests should use API-level mocks for more realistic behavior
//
// Previously this file mocked useDashboardData, useCashFlowSummary, useDefaultDashboardLayout etc.
// but that conflicted with hook implementation tests. Now we only export utility constants and
// functions that tests can use.

import { mock } from 'bun:test'

// Default refetch function
export const mockRefetch = mock(() => {})

// Default layout (matches config/default_dashboard_layout.yaml version 4)
export const MOCK_DEFAULT_LAYOUT = {
  widgets: [
    { i: 'cash-flow-chart-1', type: 'cash-flow-chart', x: 0, y: 0, w: 12, h: 5, minW: 4, minH: 4 },
    { i: 'balance-history-1', type: 'balance-history', x: 0, y: 5, w: 12, h: 4, minW: 4, minH: 4 },
  ],
  version: 4,
}

// NOTE: We intentionally do NOT mock any hooks here because:
// - useDefaultDashboardLayout is tested in useDefaultDashboardLayout.test.tsx with API-level mocks
// - useDashboardData is tested in useDashboardData.test.tsx with API-level mocks
// - useCashFlowSummary/useCashFlowHistory are tested in useCashFlowData.test.tsx with API-level mocks
// - useDashboardLayout is tested in useDashboardLayout.test.tsx with API-level mocks
//
// Component tests that need dashboard data should use API-level mocks from:
// - dashboard-api.ts (for useGetDashboardPreferences, useGetBalanceSummary, etc.)
// - analytics-api.ts (for useGetCashFlowSummary, useGetCashFlowHistory)
