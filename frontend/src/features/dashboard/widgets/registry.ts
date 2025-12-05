/**
 * Widget Registry
 * Issue #354: Customizable Dashboard Widget System
 *
 * Simple map-based registry for widget definitions.
 * Widgets register themselves on import.
 */

import type { WidgetDefinition, WidgetType, DashboardLayout, WidgetConfig } from '../types';

// Widget registry storage
const widgetRegistry = new Map<WidgetType, WidgetDefinition>();

/**
 * Register a widget definition
 */
export function registerWidget(definition: WidgetDefinition): void {
  widgetRegistry.set(definition.type, definition);
}

/**
 * Get a widget definition by type
 */
export function getWidget(type: WidgetType): WidgetDefinition | undefined {
  return widgetRegistry.get(type);
}

/**
 * Get all registered widgets
 */
export function getAllWidgets(): WidgetDefinition[] {
  return Array.from(widgetRegistry.values());
}

/**
 * Get widgets by category
 */
export function getWidgetsByCategory(category: WidgetDefinition['category']): WidgetDefinition[] {
  return getAllWidgets().filter((w) => w.category === category);
}

/**
 * Check if a widget type is registered
 */
export function isWidgetRegistered(type: WidgetType): boolean {
  return widgetRegistry.has(type);
}

/**
 * Default dashboard layout
 * Used for new users or when resetting to defaults
 */
export const DEFAULT_LAYOUT: DashboardLayout = {
  widgets: [
    {
      i: 'balance-overview-1',
      type: 'balance-overview',
      x: 0,
      y: 0,
      w: 4,
      h: 3,
      minW: 3,
      minH: 2,
    },
    {
      i: 'cash-flow-summary-1',
      type: 'cash-flow-summary',
      x: 4,
      y: 0,
      w: 8,
      h: 3,
      minW: 6,
      minH: 2,
    },
    {
      i: 'balance-history-1',
      type: 'balance-history',
      x: 0,
      y: 3,
      w: 6,
      h: 4,
      minW: 4,
      minH: 3,
    },
    {
      i: 'cash-flow-chart-1',
      type: 'cash-flow-chart',
      x: 6,
      y: 3,
      w: 6,
      h: 4,
      minW: 4,
      minH: 3,
    },
    {
      i: 'account-cards-1',
      type: 'account-cards',
      x: 0,
      y: 7,
      w: 12,
      h: 3,
      minW: 6,
      minH: 2,
    },
  ],
  version: 1,
};

/**
 * Generate unique widget ID
 */
export function generateWidgetId(type: WidgetType): string {
  return `${type}-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`;
}

/**
 * Create a new widget config with default settings from the definition
 */
export function createWidgetConfig(type: WidgetType, x: number = 0, y: number = 0): WidgetConfig | null {
  const definition = getWidget(type);
  if (!definition) return null;

  return {
    i: generateWidgetId(type),
    type,
    x,
    y,
    w: definition.defaultSize.w,
    h: definition.defaultSize.h,
    minW: definition.minSize.w,
    minH: definition.minSize.h,
    maxW: definition.maxSize?.w,
    maxH: definition.maxSize?.h,
  };
}
