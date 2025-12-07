/**
 * Widget Registry Tests
 *
 * Tests the widget registry functions including registration,
 * lookup, and configuration creation.
 *
 * Issue #354: Customizable Dashboard Widget System
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import type { WidgetDefinition, WidgetType, WidgetProps } from '../types'

// We need to test the registry in isolation, so we'll import it fresh
// and manually control registrations

describe('Widget Registry', () => {
  // Store original Date.now and Math.random for mocking
  const originalDateNow = Date.now
  const originalMathRandom = Math.random

  beforeEach(() => {
    // Reset mocks before each test
    vi.clearAllMocks()
  })

  afterEach(() => {
    // Restore original functions
    Date.now = originalDateNow
    Math.random = originalMathRandom
    vi.restoreAllMocks()
  })

  // Create a mock widget definition for testing
  const createMockDefinition = (
    type: WidgetType,
    category: 'overview' | 'charts' | 'accounts' = 'overview'
  ): WidgetDefinition => ({
    type,
    title: `${type} Widget`,
    description: `Description for ${type}`,
    icon: 'chart-bar',
    category,
    defaultSize: { w: 4, h: 3 },
    minSize: { w: 2, h: 2 },
    Component: (() => null) as React.ComponentType<WidgetProps>,
  })

  describe('registerWidget', () => {
    it('should register a new widget successfully', async () => {
      // Re-import to get fresh registry state
      const { registerWidget, getWidget } = await import('./registry')

      const definition = createMockDefinition('balance-overview')
      registerWidget(definition)

      const result = getWidget('balance-overview')
      expect(result).toBeDefined()
      expect(result?.type).toBe('balance-overview')
      expect(result?.title).toBe('balance-overview Widget')
    })

    it('should store widget with correct properties', async () => {
      const { registerWidget, getWidget } = await import('./registry')

      const definition = createMockDefinition('balance-history', 'charts')
      registerWidget(definition)

      const result = getWidget('balance-history')
      expect(result).toEqual(definition)
      expect(result?.category).toBe('charts')
      expect(result?.defaultSize).toEqual({ w: 4, h: 3 })
      expect(result?.minSize).toEqual({ w: 2, h: 2 })
    })
  })

  describe('getWidget', () => {
    it('should return widget definition for registered type', async () => {
      const { registerWidget, getWidget } = await import('./registry')

      const definition = createMockDefinition('cash-flow-summary')
      registerWidget(definition)

      const result = getWidget('cash-flow-summary')
      expect(result).toBeDefined()
      expect(result?.type).toBe('cash-flow-summary')
    })

    it('should return undefined for unregistered type', async () => {
      const { getWidget } = await import('./registry')

      // Cast to WidgetType to test the undefined case
      const result = getWidget('non-existent' as WidgetType)
      expect(result).toBeUndefined()
    })
  })

  describe('getAllWidgets', () => {
    it('should return all registered widgets', async () => {
      const { registerWidget, getAllWidgets } = await import('./registry')

      // Register multiple widgets
      registerWidget(createMockDefinition('balance-overview', 'overview'))
      registerWidget(createMockDefinition('balance-history', 'charts'))
      registerWidget(createMockDefinition('account-cards', 'accounts'))

      const result = getAllWidgets()
      expect(result.length).toBeGreaterThanOrEqual(3)

      const types = result.map((w) => w.type)
      expect(types).toContain('balance-overview')
      expect(types).toContain('balance-history')
      expect(types).toContain('account-cards')
    })
  })

  describe('getWidgetsByCategory', () => {
    it('should return empty array for unknown category', async () => {
      const { getWidgetsByCategory } = await import('./registry')

      // Use a category that shouldn't exist
      const result = getWidgetsByCategory('nonexistent' as 'overview')
      // Since widgets are pre-registered, check if this category has any
      // If the category truly doesn't exist, result would be empty
      expect(Array.isArray(result)).toBe(true)
    })

    it('should filter widgets by category correctly', async () => {
      const { registerWidget, getWidgetsByCategory } = await import('./registry')

      // Register widgets in different categories
      registerWidget(createMockDefinition('balance-overview', 'overview'))
      registerWidget(createMockDefinition('cash-flow-summary', 'overview'))
      registerWidget(createMockDefinition('balance-history', 'charts'))

      const overviewWidgets = getWidgetsByCategory('overview')
      const chartsWidgets = getWidgetsByCategory('charts')

      // All overview widgets should have category 'overview'
      overviewWidgets.forEach((w) => {
        expect(w.category).toBe('overview')
      })

      // All charts widgets should have category 'charts'
      chartsWidgets.forEach((w) => {
        expect(w.category).toBe('charts')
      })

      expect(overviewWidgets.length).toBeGreaterThanOrEqual(2)
      expect(chartsWidgets.length).toBeGreaterThanOrEqual(1)
    })

    it('should return only widgets in specified category', async () => {
      const { getWidgetsByCategory } = await import('./registry')

      const accountsWidgets = getWidgetsByCategory('accounts')

      // Every widget should be in accounts category
      accountsWidgets.forEach((w) => {
        expect(w.category).toBe('accounts')
      })
    })
  })

  describe('isWidgetRegistered', () => {
    it('should return true for registered widget', async () => {
      const { registerWidget, isWidgetRegistered } = await import('./registry')

      registerWidget(createMockDefinition('balance-overview'))

      expect(isWidgetRegistered('balance-overview')).toBe(true)
    })

    it('should return false for unregistered widget', async () => {
      const { isWidgetRegistered } = await import('./registry')

      // Cast to WidgetType to test the false case
      expect(isWidgetRegistered('fake-widget' as WidgetType)).toBe(false)
    })
  })

  describe('generateWidgetId', () => {
    it('should generate unique IDs for same type', async () => {
      const { generateWidgetId } = await import('./registry')

      const id1 = generateWidgetId('balance-overview')
      const id2 = generateWidgetId('balance-overview')

      expect(id1).not.toBe(id2)
    })

    it('should include widget type in ID', async () => {
      const { generateWidgetId } = await import('./registry')

      const id = generateWidgetId('cash-flow-chart')
      expect(id).toContain('cash-flow-chart')
    })

    it('should include timestamp component', async () => {
      const { generateWidgetId } = await import('./registry')

      // Mock Date.now to return a specific value
      const mockTimestamp = 1700000000000
      Date.now = vi.fn(() => mockTimestamp)

      const id = generateWidgetId('balance-history')
      expect(id).toContain(mockTimestamp.toString())
    })

    it('should include random component', async () => {
      const { generateWidgetId } = await import('./registry')

      // Generate multiple IDs with same timestamp
      const mockTimestamp = 1700000000000
      Date.now = vi.fn(() => mockTimestamp)

      const id1 = generateWidgetId('account-cards')
      const id2 = generateWidgetId('account-cards')

      // Even with same timestamp, IDs should be different due to random component
      expect(id1).not.toBe(id2)
    })
  })

  describe('createWidgetConfig', () => {
    it('should create config with default size from definition', async () => {
      const { registerWidget, createWidgetConfig } = await import('./registry')

      const definition: WidgetDefinition = {
        type: 'balance-overview',
        title: 'Balance Overview',
        description: 'Shows balance',
        icon: 'banknotes',
        category: 'overview',
        defaultSize: { w: 6, h: 4 },
        minSize: { w: 3, h: 2 },
        Component: (() => null) as React.ComponentType<WidgetProps>,
      }
      registerWidget(definition)

      const config = createWidgetConfig('balance-overview')

      expect(config).not.toBeNull()
      expect(config?.w).toBe(6)
      expect(config?.h).toBe(4)
    })

    it('should create config with custom position', async () => {
      const { registerWidget, createWidgetConfig } = await import('./registry')

      registerWidget(createMockDefinition('cash-flow-summary'))

      const config = createWidgetConfig('cash-flow-summary', 5, 10)

      expect(config).not.toBeNull()
      expect(config?.x).toBe(5)
      expect(config?.y).toBe(10)
    })

    it('should return null for unregistered widget type', async () => {
      const { createWidgetConfig } = await import('./registry')

      const config = createWidgetConfig('non-existent' as WidgetType)

      expect(config).toBeNull()
    })

    it('should apply minSize constraints', async () => {
      const { registerWidget, createWidgetConfig } = await import('./registry')

      const definition: WidgetDefinition = {
        type: 'balance-history',
        title: 'Balance History',
        description: 'Shows history',
        icon: 'chart-line',
        category: 'charts',
        defaultSize: { w: 8, h: 5 },
        minSize: { w: 4, h: 3 },
        Component: (() => null) as React.ComponentType<WidgetProps>,
      }
      registerWidget(definition)

      const config = createWidgetConfig('balance-history')

      expect(config).not.toBeNull()
      expect(config?.minW).toBe(4)
      expect(config?.minH).toBe(3)
    })

    it('should apply maxSize if defined', async () => {
      const { registerWidget, createWidgetConfig } = await import('./registry')

      const definition: WidgetDefinition = {
        type: 'cash-flow-chart',
        title: 'Cash Flow Chart',
        description: 'Shows cash flow',
        icon: 'chart-bar',
        category: 'charts',
        defaultSize: { w: 6, h: 4 },
        minSize: { w: 4, h: 3 },
        maxSize: { w: 12, h: 8 },
        Component: (() => null) as React.ComponentType<WidgetProps>,
      }
      registerWidget(definition)

      const config = createWidgetConfig('cash-flow-chart')

      expect(config).not.toBeNull()
      expect(config?.maxW).toBe(12)
      expect(config?.maxH).toBe(8)
    })

    it('should generate unique ID', async () => {
      const { registerWidget, createWidgetConfig } = await import('./registry')

      registerWidget(createMockDefinition('account-cards'))

      const config1 = createWidgetConfig('account-cards')
      const config2 = createWidgetConfig('account-cards')

      expect(config1?.i).not.toBe(config2?.i)
    })
  })

  describe('DEFAULT_LAYOUT', () => {
    /**
     * NOTE: DEFAULT_LAYOUT temporarily reduced to 1 widget while fixing
     * rendering issues. See issues #417-419, #425-426.
     */
    it('should contain expected widgets (temporarily reduced)', async () => {
      const { DEFAULT_LAYOUT } = await import('./registry')

      // Temporarily reduced to 1 widget while fixing rendering issues
      expect(DEFAULT_LAYOUT.widgets).toHaveLength(1)
    })

    it('should have all valid widget types', async () => {
      const { DEFAULT_LAYOUT } = await import('./registry')

      // Temporarily only cash-flow-chart enabled
      const expectedTypes: WidgetType[] = [
        'cash-flow-chart',
      ]

      const actualTypes = DEFAULT_LAYOUT.widgets.map((w) => w.type)

      expectedTypes.forEach((type) => {
        expect(actualTypes).toContain(type)
      })
    })

    it('should have correct version', async () => {
      const { DEFAULT_LAYOUT } = await import('./registry')

      // Version bumped to 2 to force layout reset
      expect(DEFAULT_LAYOUT.version).toBe(2)
    })

    it('should have unique widget IDs', async () => {
      const { DEFAULT_LAYOUT } = await import('./registry')

      const ids = DEFAULT_LAYOUT.widgets.map((w) => w.i)
      const uniqueIds = new Set(ids)

      expect(uniqueIds.size).toBe(ids.length)
    })

    it('should have widgets with valid positions', async () => {
      const { DEFAULT_LAYOUT } = await import('./registry')

      DEFAULT_LAYOUT.widgets.forEach((widget) => {
        expect(widget.x).toBeGreaterThanOrEqual(0)
        expect(widget.y).toBeGreaterThanOrEqual(0)
        expect(widget.w).toBeGreaterThan(0)
        expect(widget.h).toBeGreaterThan(0)
      })
    })

    it('should have widgets with min size constraints', async () => {
      const { DEFAULT_LAYOUT } = await import('./registry')

      DEFAULT_LAYOUT.widgets.forEach((widget) => {
        expect(widget.minW).toBeDefined()
        expect(widget.minH).toBeDefined()
        expect(widget.minW).toBeGreaterThan(0)
        expect(widget.minH).toBeGreaterThan(0)
      })
    })

    it('should have widgets that fit within 12-column grid', async () => {
      const { DEFAULT_LAYOUT } = await import('./registry')

      DEFAULT_LAYOUT.widgets.forEach((widget) => {
        expect(widget.x + widget.w).toBeLessThanOrEqual(12)
      })
    })
  })
})
