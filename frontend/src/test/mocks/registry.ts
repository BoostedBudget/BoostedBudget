// Issue #575: Centralized registry mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests
// All widget test files should use these exported mocks instead of inline mock.module()

import { mock } from 'bun:test'
import type { WidgetDefinition, DashboardLayout, WidgetType, WidgetConfig } from '@/features/dashboard/types'

// Configurable mock functions - tests customize these in beforeEach
export const mockGetWidget = mock(() => undefined as WidgetDefinition | undefined)
export const mockGetAllWidgets = mock(() => [] as WidgetDefinition[])
export const mockRegisterWidget = mock(() => {})
export const mockGetWidgetsByCategory = mock(() => [] as WidgetDefinition[])
export const mockIsWidgetRegistered = mock(() => false)
export const mockGenerateWidgetId = mock((type: WidgetType) => `${type}-mock-id`)
export const mockCreateWidgetConfig = mock(() => null as WidgetConfig | null)

// Default layout matching registry.ts (version must match config/default_dashboard_layout.yaml)
const MOCK_DEFAULT_LAYOUT: DashboardLayout = {
  widgets: [
    { i: 'cash-flow-chart-1', type: 'cash-flow-chart', x: 0, y: 0, w: 12, h: 5, minW: 4, minH: 4 },
    { i: 'balance-history-1', type: 'balance-history', x: 0, y: 5, w: 12, h: 4, minW: 4, minH: 4 },
  ],
  version: 4,
}

// Single mock.module() call - called once at preload time
// All tests share this mock, but can customize behavior via mockImplementation
mock.module('@/features/dashboard/widgets/registry', () => ({
  getWidget: (...args: unknown[]) => mockGetWidget(...args),
  getAllWidgets: () => mockGetAllWidgets(),
  registerWidget: (...args: unknown[]) => mockRegisterWidget(...args),
  getWidgetsByCategory: (...args: unknown[]) => mockGetWidgetsByCategory(...args),
  isWidgetRegistered: (...args: unknown[]) => mockIsWidgetRegistered(...args),
  generateWidgetId: (...args: unknown[]) => mockGenerateWidgetId(...(args as [WidgetType])),
  createWidgetConfig: (...args: unknown[]) => mockCreateWidgetConfig(...args),
  DEFAULT_LAYOUT: MOCK_DEFAULT_LAYOUT,
}))

// Note: We intentionally do NOT mock './registry' relative path
// This allows registry.test.ts to test the REAL implementation
// Only the alias path '@/features/dashboard/widgets/registry' is mocked
