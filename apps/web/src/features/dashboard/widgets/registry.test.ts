/**
 * Widget Registry Tests
 *
 * Tests the widget registry functions including registration,
 * lookup, and configuration creation.
 *
 * Issue #354: Customizable Dashboard Widget System
 * Issue #575: Migrated to bun:test with isolated config (bunfig.real.toml)
 *
 * NOTE: This test runs with bunfig.real.toml to test the REAL registry,
 * not the centralized mock. Run with: bun test --config bunfig.real.toml
 */

import { describe, it, expect, mock, beforeEach, afterEach } from 'bun:test'
import type { WidgetDefinition, WidgetType, WidgetProps } from '../types'
import {
  registerWidget,
  getWidget,
  getAllWidgets,
  getWidgetsByCategory,
  isWidgetRegistered,
  generateWidgetId,
  createWidgetConfig,
  DEFAULT_LAYOUT,
} from './registry'

describe('Widget Registry', () => {
  const originalDateNow = Date.now
  const originalMathRandom = Math.random

  beforeEach(() => {
    Date.now = originalDateNow
    Math.random = originalMathRandom
  })

  afterEach(() => {
    Date.now = originalDateNow
    Math.random = originalMathRandom
  })

  // Use unique widget types per test to avoid conflicts with shared state
  const createTestDefinition = (
    suffix: string,
    category: 'overview' | 'charts' | 'accounts' = 'overview'
  ): WidgetDefinition => ({
    type: `test-${suffix}-${Date.now()}` as WidgetType,
    title: `Test Widget ${suffix}`,
    description: `Description for ${suffix}`,
    icon: 'chart-bar',
    category,
    defaultSize: { w: 4, h: 3 },
    minSize: { w: 2, h: 2 },
    Component: (() => null) as React.ComponentType<WidgetProps>,
  })

  describe('registerWidget', () => {
    it('should register a new widget successfully', () => {
      const definition = createTestDefinition('reg-new')
      registerWidget(definition)

      const result = getWidget(definition.type)
      expect(result).toBeDefined()
      expect(result?.type).toBe(definition.type)
    })

    it('should store widget with correct properties', () => {
      const definition = createTestDefinition('reg-props', 'charts')
      registerWidget(definition)

      const result = getWidget(definition.type)
      expect(result).toEqual(definition)
      expect(result?.category).toBe('charts')
      expect(result?.defaultSize).toEqual({ w: 4, h: 3 })
      expect(result?.minSize).toEqual({ w: 2, h: 2 })
    })
  })

  describe('getWidget', () => {
    it('should return widget definition for registered type', () => {
      const definition = createTestDefinition('get-exists')
      registerWidget(definition)

      const result = getWidget(definition.type)
      expect(result).toBeDefined()
      expect(result?.type).toBe(definition.type)
    })

    it('should return undefined for unregistered type', () => {
      const result = getWidget('non-existent-widget-xyz' as WidgetType)
      expect(result).toBeUndefined()
    })
  })

  describe('getAllWidgets', () => {
    it('should return array of widgets including registered ones', () => {
      const definition = createTestDefinition('getall')
      registerWidget(definition)

      const result = getAllWidgets()
      expect(result.length).toBeGreaterThanOrEqual(1)

      const found = result.find((w) => w.type === definition.type)
      expect(found).toBeDefined()
    })
  })

  describe('getWidgetsByCategory', () => {
    it('should filter widgets by category correctly', () => {
      const overviewDef = createTestDefinition('cat-overview', 'overview')
      const chartsDef = createTestDefinition('cat-charts', 'charts')

      registerWidget(overviewDef)
      registerWidget(chartsDef)

      const overviewWidgets = getWidgetsByCategory('overview')
      const chartsWidgets = getWidgetsByCategory('charts')

      // All returned widgets should be in their respective categories
      overviewWidgets.forEach((w) => {
        expect(w.category).toBe('overview')
      })
      chartsWidgets.forEach((w) => {
        expect(w.category).toBe('charts')
      })

      // Our registered widgets should be found
      expect(overviewWidgets.find((w) => w.type === overviewDef.type)).toBeDefined()
      expect(chartsWidgets.find((w) => w.type === chartsDef.type)).toBeDefined()
    })

    it('should return array (possibly empty) for any category', () => {
      const result = getWidgetsByCategory('accounts')
      expect(Array.isArray(result)).toBe(true)
    })
  })

  describe('isWidgetRegistered', () => {
    it('should return true for registered widget', () => {
      const definition = createTestDefinition('isreg-true')
      registerWidget(definition)

      expect(isWidgetRegistered(definition.type)).toBe(true)
    })

    it('should return false for unregistered widget', () => {
      expect(isWidgetRegistered('fake-widget-not-exists' as WidgetType)).toBe(false)
    })
  })

  describe('generateWidgetId', () => {
    it('should generate unique IDs for same type', () => {
      const id1 = generateWidgetId('balance-overview')
      const id2 = generateWidgetId('balance-overview')

      expect(id1).not.toBe(id2)
    })

    it('should include widget type in ID', () => {
      const id = generateWidgetId('cash-flow-chart')
      expect(id).toContain('cash-flow-chart')
    })

    it('should include timestamp component', () => {
      const mockTimestamp = 1700000000000
      Date.now = mock(() => mockTimestamp)

      const id = generateWidgetId('balance-history')
      expect(id).toContain(mockTimestamp.toString())
    })

    it('should include random component for uniqueness', () => {
      const mockTimestamp = 1700000000000
      Date.now = mock(() => mockTimestamp)

      const id1 = generateWidgetId('account-cards')
      const id2 = generateWidgetId('account-cards')

      // Even with same timestamp, IDs should differ due to random component
      expect(id1).not.toBe(id2)
    })
  })

  describe('createWidgetConfig', () => {
    it('should create config with default size from definition', () => {
      const definition: WidgetDefinition = {
        type: `config-size-${Date.now()}` as WidgetType,
        title: 'Config Size Test',
        description: 'Tests config creation',
        icon: 'banknotes',
        category: 'overview',
        defaultSize: { w: 6, h: 4 },
        minSize: { w: 3, h: 2 },
        Component: (() => null) as React.ComponentType<WidgetProps>,
      }
      registerWidget(definition)

      const config = createWidgetConfig(definition.type)

      expect(config).not.toBeNull()
      expect(config?.w).toBe(6)
      expect(config?.h).toBe(4)
    })

    it('should create config with custom position', () => {
      const definition = createTestDefinition('config-pos')
      registerWidget(definition)

      const config = createWidgetConfig(definition.type, 5, 10)

      expect(config).not.toBeNull()
      expect(config?.x).toBe(5)
      expect(config?.y).toBe(10)
    })

    it('should return null for unregistered widget type', () => {
      const config = createWidgetConfig('non-existent-config-test' as WidgetType)
      expect(config).toBeNull()
    })

    it('should apply minSize constraints', () => {
      const definition: WidgetDefinition = {
        type: `config-minsize-${Date.now()}` as WidgetType,
        title: 'Min Size Test',
        description: 'Tests min size',
        icon: 'chart-line',
        category: 'charts',
        defaultSize: { w: 8, h: 5 },
        minSize: { w: 4, h: 3 },
        Component: (() => null) as React.ComponentType<WidgetProps>,
      }
      registerWidget(definition)

      const config = createWidgetConfig(definition.type)

      expect(config).not.toBeNull()
      expect(config?.minW).toBe(4)
      expect(config?.minH).toBe(3)
    })

    it('should apply maxSize if defined', () => {
      const definition: WidgetDefinition = {
        type: `config-maxsize-${Date.now()}` as WidgetType,
        title: 'Max Size Test',
        description: 'Tests max size',
        icon: 'chart-bar',
        category: 'charts',
        defaultSize: { w: 6, h: 4 },
        minSize: { w: 4, h: 3 },
        maxSize: { w: 12, h: 8 },
        Component: (() => null) as React.ComponentType<WidgetProps>,
      }
      registerWidget(definition)

      const config = createWidgetConfig(definition.type)

      expect(config).not.toBeNull()
      expect(config?.maxW).toBe(12)
      expect(config?.maxH).toBe(8)
    })

    it('should generate unique ID', () => {
      const definition = createTestDefinition('config-uniqueid')
      registerWidget(definition)

      const config1 = createWidgetConfig(definition.type)
      const config2 = createWidgetConfig(definition.type)

      expect(config1?.i).not.toBe(config2?.i)
    })
  })

  describe('DEFAULT_LAYOUT', () => {
    it('should contain expected widgets', () => {
      // Cash flow chart + balance history widget
      expect(DEFAULT_LAYOUT.widgets).toHaveLength(2)
    })

    it('should have all valid widget types', () => {
      const expectedTypes: WidgetType[] = ['cash-flow-chart', 'balance-history']
      const actualTypes = DEFAULT_LAYOUT.widgets.map((w) => w.type)

      expectedTypes.forEach((type) => {
        expect(actualTypes).toContain(type)
      })
    })

    it('should have correct version', () => {
      // Version bumped to 4 for Issue #468 (increased minH values)
      expect(DEFAULT_LAYOUT.version).toBe(4)
    })

    it('should have unique widget IDs', () => {
      const ids = DEFAULT_LAYOUT.widgets.map((w) => w.i)
      const uniqueIds = new Set(ids)

      expect(uniqueIds.size).toBe(ids.length)
    })

    it('should have widgets with valid positions', () => {
      DEFAULT_LAYOUT.widgets.forEach((widget) => {
        expect(widget.x).toBeGreaterThanOrEqual(0)
        expect(widget.y).toBeGreaterThanOrEqual(0)
        expect(widget.w).toBeGreaterThan(0)
        expect(widget.h).toBeGreaterThan(0)
      })
    })

    it('should have widgets with min size constraints', () => {
      DEFAULT_LAYOUT.widgets.forEach((widget) => {
        expect(widget.minW).toBeDefined()
        expect(widget.minH).toBeDefined()
        expect(widget.minW).toBeGreaterThan(0)
        expect(widget.minH).toBeGreaterThan(0)
      })
    })

    it('should have widgets that fit within 12-column grid', () => {
      DEFAULT_LAYOUT.widgets.forEach((widget) => {
        expect(widget.x + widget.w).toBeLessThanOrEqual(12)
      })
    })
  })
})
