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
 * @param gs - GridStack widget object
 * @param type - Widget type (GridStack doesn't store this)
 * @returns Our internal widget configuration
 * @throws Error if widget has no id
 */
export function toWidgetConfig(
  gs: GridStackWidget,
  type: WidgetType
): WidgetConfig {
  if (!gs.id) {
    throw new Error('GridStack widget must have an id');
  }

  const config: WidgetConfig = {
    i: gs.id,
    type,
    x: gs.x ?? 0,
    y: gs.y ?? 0,
    w: gs.w ?? 0,
    h: gs.h ?? 0,
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
    float: false,

    // Edit mode controls
    disableDrag: !isEditing,
    disableResize: !isEditing,

    // Drag handle - reuses existing cursor-move class
    handleClass: 'cursor-move',

    // Enable drop from widget catalog
    acceptWidgets: true,
  };
}
