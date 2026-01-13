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
 * Default dashboard layout - FALLBACK ONLY
 *
 * Issue #449: This is now a FALLBACK for when the API is unavailable.
 * The authoritative default layout is defined in:
 *   config/default_dashboard_layout.yaml
 *
 * This layout is served via GET /api/v1/dashboard/default-layout
 * and fetched by useDefaultDashboardLayout hook.
 *
 * Keep this in sync with the YAML config file!
 *
 * NOTE: Re-enabled BalanceHistoryWidget after fresh rewrite (Issue #418).
 * Other widgets still pending fixes:
 * - #419: CashFlowSummaryWidget
 * - #425: BalanceOverviewWidget
 * - #426: AccountCardsWidget
 */
// Issue #468: Updated minH values for proper chart/content rendering
// Version 4 triggers auto-reset for existing users with outdated layouts
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
      minH: 4,
    },
    {
      i: 'balance-history-1',
      type: 'balance-history',
      x: 0,
      y: 5,
      w: 12,
      h: 4,
      minW: 4,
      minH: 4,
    },
  ],
  version: 4,
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
