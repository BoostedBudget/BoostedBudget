/**
 * GridStack Conversion Utilities Tests
 * Issue #441: Migrate dashboard from react-grid-layout to GridStack.js
 *
 * TDD RED phase: Write failing tests for conversion utilities
 */

import { describe, it, expect } from 'vitest';
import {
  toGridStackWidget,
  toWidgetConfig,
  toGridStackOptions,
  isValidGridStackWidget,
} from './gridstack-utils';
import type { WidgetConfig } from './index';
import type { GridStackWidget } from 'gridstack';

describe('GridStack Conversion Utilities - Issue #441', () => {
  /**
   * Issue #476: Validate GridStack widget dimensions to prevent 400 errors
   * during resize operations where intermediate states may have invalid dimensions
   */
  describe('isValidGridStackWidget - Issue #476', () => {
    it('should return true for widget with valid dimensions', () => {
      const gs: GridStackWidget = {
        id: 'widget-1',
        x: 0,
        y: 0,
        w: 4,
        h: 3,
      };

      expect(isValidGridStackWidget(gs)).toBe(true);
    });

    it('should return false for widget with w=0', () => {
      const gs: GridStackWidget = {
        id: 'widget-1',
        x: 0,
        y: 0,
        w: 0,
        h: 3,
      };

      expect(isValidGridStackWidget(gs)).toBe(false);
    });

    it('should return false for widget with h=0', () => {
      const gs: GridStackWidget = {
        id: 'widget-1',
        x: 0,
        y: 0,
        w: 4,
        h: 0,
      };

      expect(isValidGridStackWidget(gs)).toBe(false);
    });

    it('should return false for widget with undefined w', () => {
      const gs: GridStackWidget = {
        id: 'widget-1',
        x: 0,
        y: 0,
        h: 3,
      };

      expect(isValidGridStackWidget(gs)).toBe(false);
    });

    it('should return false for widget with undefined h', () => {
      const gs: GridStackWidget = {
        id: 'widget-1',
        x: 0,
        y: 0,
        w: 4,
      };

      expect(isValidGridStackWidget(gs)).toBe(false);
    });

    it('should return false for widget with undefined id', () => {
      const gs: GridStackWidget = {
        x: 0,
        y: 0,
        w: 4,
        h: 3,
      };

      expect(isValidGridStackWidget(gs)).toBe(false);
    });

    it('should return true for widget with minimum valid dimensions (w=1, h=1)', () => {
      const gs: GridStackWidget = {
        id: 'widget-1',
        x: 0,
        y: 0,
        w: 1,
        h: 1,
      };

      expect(isValidGridStackWidget(gs)).toBe(true);
    });

    it('should return false for widget with negative dimensions', () => {
      const gs: GridStackWidget = {
        id: 'widget-1',
        x: 0,
        y: 0,
        w: -1,
        h: 3,
      };

      expect(isValidGridStackWidget(gs)).toBe(false);
    });
  });

  describe('toGridStackWidget', () => {
    it('should convert i to id', () => {
      const config: WidgetConfig = {
        i: 'widget-1',
        type: 'balance-overview',
        x: 0,
        y: 0,
        w: 4,
        h: 3,
      };

      const result = toGridStackWidget(config);

      expect(result.id).toBe('widget-1');
      expect(result).not.toHaveProperty('i');
    });

    it('should preserve position and size properties', () => {
      const config: WidgetConfig = {
        i: 'widget-1',
        type: 'cash-flow-chart',
        x: 2,
        y: 3,
        w: 8,
        h: 5,
      };

      const result = toGridStackWidget(config);

      expect(result.x).toBe(2);
      expect(result.y).toBe(3);
      expect(result.w).toBe(8);
      expect(result.h).toBe(5);
    });

    it('should convert min/max constraints', () => {
      const config: WidgetConfig = {
        i: 'widget-1',
        type: 'balance-overview',
        x: 0,
        y: 0,
        w: 4,
        h: 3,
        minW: 3,
        minH: 2,
        maxW: 12,
        maxH: 6,
      };

      const result = toGridStackWidget(config);

      expect(result.minW).toBe(3);
      expect(result.minH).toBe(2);
      expect(result.maxW).toBe(12);
      expect(result.maxH).toBe(6);
    });

    it('should convert static to noMove and noResize', () => {
      const config: WidgetConfig = {
        i: 'widget-1',
        type: 'balance-overview',
        x: 0,
        y: 0,
        w: 4,
        h: 3,
        static: true,
      };

      const result = toGridStackWidget(config);

      expect(result.noMove).toBe(true);
      expect(result.noResize).toBe(true);
    });

    it('should not set noMove/noResize when static is false or undefined', () => {
      const config: WidgetConfig = {
        i: 'widget-1',
        type: 'balance-overview',
        x: 0,
        y: 0,
        w: 4,
        h: 3,
        static: false,
      };

      const result = toGridStackWidget(config);

      expect(result.noMove).toBeUndefined();
      expect(result.noResize).toBeUndefined();
    });

    it('should handle undefined optional fields', () => {
      const config: WidgetConfig = {
        i: 'widget-1',
        type: 'balance-overview',
        x: 0,
        y: 0,
        w: 4,
        h: 3,
      };

      const result = toGridStackWidget(config);

      expect(result.minW).toBeUndefined();
      expect(result.minH).toBeUndefined();
      expect(result.maxW).toBeUndefined();
      expect(result.maxH).toBeUndefined();
      expect(result.noMove).toBeUndefined();
      expect(result.noResize).toBeUndefined();
    });

    it('should not include type property in GridStack widget', () => {
      const config: WidgetConfig = {
        i: 'widget-1',
        type: 'cash-flow-summary',
        x: 0,
        y: 0,
        w: 4,
        h: 3,
      };

      const result = toGridStackWidget(config);

      // GridStack doesn't know about our widget types
      expect(result).not.toHaveProperty('type');
    });
  });

  describe('toWidgetConfig', () => {
    it('should convert id to i', () => {
      const gs: GridStackWidget = {
        id: 'widget-1',
        x: 0,
        y: 0,
        w: 4,
        h: 3,
      };

      const result = toWidgetConfig(gs, 'balance-overview');

      expect(result).not.toBeNull();
      expect(result!.i).toBe('widget-1');
      expect(result).not.toHaveProperty('id');
    });

    it('should set the widget type from parameter', () => {
      const gs: GridStackWidget = {
        id: 'widget-1',
        x: 0,
        y: 0,
        w: 4,
        h: 3,
      };

      const result = toWidgetConfig(gs, 'cash-flow-chart');

      expect(result).not.toBeNull();
      expect(result!.type).toBe('cash-flow-chart');
    });

    it('should preserve position and size properties', () => {
      const gs: GridStackWidget = {
        id: 'widget-1',
        x: 4,
        y: 2,
        w: 6,
        h: 4,
      };

      const result = toWidgetConfig(gs, 'balance-overview');

      expect(result).not.toBeNull();
      expect(result!.x).toBe(4);
      expect(result!.y).toBe(2);
      expect(result!.w).toBe(6);
      expect(result!.h).toBe(4);
    });

    it('should convert min/max constraints', () => {
      const gs: GridStackWidget = {
        id: 'widget-1',
        x: 0,
        y: 0,
        w: 4,
        h: 3,
        minW: 2,
        minH: 2,
        maxW: 8,
        maxH: 5,
      };

      const result = toWidgetConfig(gs, 'balance-overview');

      expect(result).not.toBeNull();
      expect(result!.minW).toBe(2);
      expect(result!.minH).toBe(2);
      expect(result!.maxW).toBe(8);
      expect(result!.maxH).toBe(5);
    });

    it('should convert noMove to static', () => {
      const gs: GridStackWidget = {
        id: 'widget-1',
        x: 0,
        y: 0,
        w: 4,
        h: 3,
        noMove: true,
      };

      const result = toWidgetConfig(gs, 'balance-overview');

      expect(result).not.toBeNull();
      expect(result!.static).toBe(true);
    });

    it('should convert noResize to static', () => {
      const gs: GridStackWidget = {
        id: 'widget-1',
        x: 0,
        y: 0,
        w: 4,
        h: 3,
        noResize: true,
      };

      const result = toWidgetConfig(gs, 'balance-overview');

      expect(result).not.toBeNull();
      expect(result!.static).toBe(true);
    });

    it('should set static to true when either noMove or noResize is true', () => {
      const gs: GridStackWidget = {
        id: 'widget-1',
        x: 0,
        y: 0,
        w: 4,
        h: 3,
        noMove: true,
        noResize: false,
      };

      const result = toWidgetConfig(gs, 'balance-overview');

      expect(result).not.toBeNull();
      expect(result!.static).toBe(true);
    });

    it('should handle undefined optional fields', () => {
      const gs: GridStackWidget = {
        id: 'widget-1',
        x: 0,
        y: 0,
        w: 4,
        h: 3,
      };

      const result = toWidgetConfig(gs, 'balance-overview');

      expect(result).not.toBeNull();
      expect(result!.minW).toBeUndefined();
      expect(result!.minH).toBeUndefined();
      expect(result!.maxW).toBeUndefined();
      expect(result!.maxH).toBeUndefined();
      expect(result!.static).toBeUndefined();
    });

    /**
     * Issue #476: toWidgetConfig should return null for invalid intermediate resize states
     * instead of defaulting to 0 which causes 400 errors from backend validation
     */
    it('should return null for widget with undefined dimensions (invalid intermediate state)', () => {
      const gs: GridStackWidget = {
        id: 'widget-1',
      };

      const result = toWidgetConfig(gs, 'balance-overview');

      expect(result).toBeNull();
    });

    it('should return null for widget with w=0 (invalid intermediate state)', () => {
      const gs: GridStackWidget = {
        id: 'widget-1',
        x: 0,
        y: 0,
        w: 0,
        h: 3,
      };

      const result = toWidgetConfig(gs, 'balance-overview');

      expect(result).toBeNull();
    });

    it('should return null for widget with h=0 (invalid intermediate state)', () => {
      const gs: GridStackWidget = {
        id: 'widget-1',
        x: 0,
        y: 0,
        w: 4,
        h: 0,
      };

      const result = toWidgetConfig(gs, 'balance-overview');

      expect(result).toBeNull();
    });

    it('should return null for widget with undefined id', () => {
      const gs: GridStackWidget = {
        x: 0,
        y: 0,
        w: 4,
        h: 3,
      };

      const result = toWidgetConfig(gs, 'balance-overview');

      expect(result).toBeNull();
    });

    it('should default x, y to 0 when undefined but w, h are valid', () => {
      const gs: GridStackWidget = {
        id: 'widget-1',
        w: 4,
        h: 3,
      };

      const result = toWidgetConfig(gs, 'balance-overview');

      expect(result).not.toBeNull();
      expect(result!.x).toBe(0);
      expect(result!.y).toBe(0);
      expect(result!.w).toBe(4);
      expect(result!.h).toBe(3);
    });
  });

  describe('toGridStackOptions', () => {
    it('should return base options for view mode (not editing)', () => {
      const result = toGridStackOptions(false);

      expect(result.disableDrag).toBe(true);
      expect(result.disableResize).toBe(true);
    });

    it('should enable drag and resize in edit mode', () => {
      const result = toGridStackOptions(true);

      expect(result.disableDrag).toBe(false);
      expect(result.disableResize).toBe(false);
    });

    it('should set correct grid configuration', () => {
      const result = toGridStackOptions(false);

      expect(result.column).toBe(12);
      expect(result.cellHeight).toBe(80);
      expect(result.margin).toBe(16);
      expect(result.float).toBe(false);
    });

    it('should set handleClass for drag handle', () => {
      const result = toGridStackOptions(true);

      expect(result.handleClass).toBe('cursor-move');
    });

    it('should set acceptWidgets for drop functionality', () => {
      const result = toGridStackOptions(true);

      expect(result.acceptWidgets).toBe(true);
    });
  });

  describe('Round-trip conversion', () => {
    it('should preserve data through toGridStackWidget -> toWidgetConfig', () => {
      const original: WidgetConfig = {
        i: 'widget-1',
        type: 'cash-flow-chart',
        x: 2,
        y: 3,
        w: 8,
        h: 5,
        minW: 4,
        minH: 3,
        maxW: 12,
        maxH: 6,
        static: true,
      };

      const gsWidget = toGridStackWidget(original);
      const restored = toWidgetConfig(gsWidget, original.type);

      expect(restored).not.toBeNull();
      expect(restored!.i).toBe(original.i);
      expect(restored!.type).toBe(original.type);
      expect(restored!.x).toBe(original.x);
      expect(restored!.y).toBe(original.y);
      expect(restored!.w).toBe(original.w);
      expect(restored!.h).toBe(original.h);
      expect(restored!.minW).toBe(original.minW);
      expect(restored!.minH).toBe(original.minH);
      expect(restored!.maxW).toBe(original.maxW);
      expect(restored!.maxH).toBe(original.maxH);
      expect(restored!.static).toBe(original.static);
    });

    it('should preserve data for widget without optional fields', () => {
      const original: WidgetConfig = {
        i: 'widget-2',
        type: 'balance-overview',
        x: 0,
        y: 0,
        w: 4,
        h: 3,
      };

      const gsWidget = toGridStackWidget(original);
      const restored = toWidgetConfig(gsWidget, original.type);

      expect(restored).not.toBeNull();
      expect(restored!.i).toBe(original.i);
      expect(restored!.type).toBe(original.type);
      expect(restored!.x).toBe(original.x);
      expect(restored!.y).toBe(original.y);
      expect(restored!.w).toBe(original.w);
      expect(restored!.h).toBe(original.h);
    });
  });
});
