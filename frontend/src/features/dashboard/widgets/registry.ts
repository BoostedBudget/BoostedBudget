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
 *
 * NOTE: Temporarily reduced to only cash-flow-chart widget while fixing
 * rendering issues in other widgets. See issues:
 * - #417: CashFlowChartWidget (in progress)
 * - #418: BalanceHistoryWidget
 * - #419: CashFlowSummaryWidget
 * - #425: BalanceOverviewWidget
 * - #426: AccountCardsWidget
 */
export const DEFAULT_LAYOUT: DashboardLayout = {
  widgets: [
    {
      i: 'cash-flow-chart-1',
      type: 'cash-flow-chart',
      x: 0,
      y: 0,
      w: 12,
      h: 5,
      minW: 4,
      minH: 3,
    },
  ],
  version: 2,
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
