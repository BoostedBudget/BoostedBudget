/**
 * GridStack Conversion Utilities
 * Issue #441: Migrate dashboard from react-grid-layout to GridStack.js
 *
 * Converts between our WidgetConfig format and GridStack's widget format.
 * Key differences:
 * - WidgetConfig uses 'i' for id, GridStack uses 'id'
 * - WidgetConfig uses 'static' for lock, GridStack uses 'noMove'/'noResize'
 */

import type { GridStackWidget, GridStackNode, GridStackOptions } from 'gridstack';
import type { WidgetConfig, WidgetType } from './index';

/**
 * Issue #476: Validate GridStack widget dimensions
 *
 * Checks if a GridStack widget has valid dimensions for persistence.
 * Returns false for intermediate resize states with invalid dimensions (w=0, h=0, undefined).
 * This prevents 400 errors when GridStack fires 'change' events during resize drag.
 *
 * @param gs - GridStack widget to validate
 * @returns true if widget has valid id and dimensions (w >= 1, h >= 1)
 */
export function isValidGridStackWidget(gs: GridStackWidget): boolean {
  return (
    gs.id !== undefined &&
    gs.w !== undefined &&
    gs.w >= 1 &&
    gs.h !== undefined &&
    gs.h >= 1
  );
}

/**
 * Convert our WidgetConfig to GridStack widget format.
 *
 * @param config - Our internal widget configuration
 * @returns GridStack-compatible widget object
 */
export function toGridStackWidget(config: WidgetConfig): GridStackNode {
  const widget: GridStackNode = {
    id: config.i,
    x: config.x,
    y: config.y,
    w: config.w,
    h: config.h,
  };

  // Add optional min/max constraints if defined
  if (config.minW !== undefined) widget.minW = config.minW;
  if (config.minH !== undefined) widget.minH = config.minH;
  if (config.maxW !== undefined) widget.maxW = config.maxW;
  if (config.maxH !== undefined) widget.maxH = config.maxH;

  // Convert 'static' to noMove/noResize
  if (config.static === true) {
    widget.noMove = true;
    widget.noResize = true;
  }

  return widget;
}

/**
 * Convert GridStack widget back to our WidgetConfig format.
 *
 * Issue #476: Returns null for invalid intermediate resize states
 * (missing id, w=0, h=0, undefined dimensions) to prevent 400 errors
 * when GridStack fires 'change' events during resize drag operations.
 *
 * @param gs - GridStack widget object
 * @param type - Widget type (GridStack doesn't store this)
 * @returns Our internal widget configuration, or null if widget has invalid dimensions
 */
export function toWidgetConfig(
  gs: GridStackWidget,
  type: WidgetType
): WidgetConfig | null {
  // Issue #476: Reject invalid intermediate states (e.g., during resize drag)
  // This prevents 400 errors from backend validation
  if (!isValidGridStackWidget(gs)) {
    return null;
  }

  const config: WidgetConfig = {
    i: gs.id!, // Safe: validated by isValidGridStackWidget
    type,
    x: gs.x ?? 0,
    y: gs.y ?? 0,
    w: gs.w!, // Safe: validated >= 1 by isValidGridStackWidget
    h: gs.h!, // Safe: validated >= 1 by isValidGridStackWidget
  };

  // Add optional min/max constraints if defined
  if (gs.minW !== undefined) config.minW = gs.minW;
  if (gs.minH !== undefined) config.minH = gs.minH;
  if (gs.maxW !== undefined) config.maxW = gs.maxW;
  if (gs.maxH !== undefined) config.maxH = gs.maxH;

  // Convert noMove/noResize to 'static'
  if (gs.noMove === true || gs.noResize === true) {
    config.static = true;
  }

  return config;
}

/**
 * Get GridStack initialization options based on edit mode.
 *
 * @param isEditing - Whether dashboard is in edit mode
 * @returns GridStack initialization options
 */
export function toGridStackOptions(isEditing: boolean): GridStackOptions {
  return {
    // Grid configuration
    column: 12,
    cellHeight: 80,
    margin: 16,
    // Issue #478: Changed to true to preserve inline widget x-positions
    float: true,

    // Edit mode controls
    disableDrag: !isEditing,
    disableResize: !isEditing,

    // Drag handle - reuses existing cursor-move class
    handleClass: 'cursor-move',

    // Enable drop from widget catalog
    acceptWidgets: true,
  };
}
