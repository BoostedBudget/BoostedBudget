/**
 * Dashboard Grid Component
 * Issue #354: Customizable Dashboard Widget System
 * Issue #441: Migrated from react-grid-layout to GridStack.js
 *
 * Uses GridStack.js for drag-drop and resize functionality.
 * Renders widgets using WidgetWrapper and the widget registry.
 *
 * Key architecture difference from react-grid-layout:
 * - GridStack creates a SEPARATE .grid-stack-item-content wrapper
 *   with position: absolute; inset: 0, solving the height chain issue
 * - Uses gs-* data attributes for widget configuration
 * - Controlled pattern with _ignoreCB to prevent infinite update loops
 */

import { useEffect, useRef, useCallback } from 'react';
import { GridStack } from 'gridstack';
import 'gridstack/dist/gridstack.min.css';
import './DashboardGrid.css';

import { WidgetWrapper } from './WidgetWrapper';
import { getWidget } from './registry';
import { toWidgetConfig } from '../types/gridstack-utils';
import type { WidgetType, DashboardLayout, WidgetConfig } from '../types';

// Extend GridStack type to include our _ignoreCB flag
declare module 'gridstack' {
  interface GridStack {
    _ignoreCB?: boolean;
  }
}

interface DashboardGridProps {
  layout: DashboardLayout;
  isEditing: boolean;
  onLayoutChange: (layout: DashboardLayout) => void;
  onRemoveWidget: (widgetId: string) => void;
  onDropWidget?: (widgetType: WidgetType, x: number, y: number) => void;
}

export function DashboardGrid({
  layout,
  isEditing,
  onLayoutChange,
  onRemoveWidget,
  onDropWidget,
}: DashboardGridProps) {
  const gridContainerRef = useRef<HTMLDivElement>(null);
  const gridRef = useRef<GridStack | null>(null);
  // Store widget DOM elements by widget ID
  const widgetElementsRef = useRef<Map<string, HTMLDivElement>>(new Map());

  // Stable references for callbacks used in effects
  const layoutRef = useRef(layout);
  const onLayoutChangeRef = useRef(onLayoutChange);
  const onDropWidgetRef = useRef(onDropWidget);

  // Keep refs updated
  layoutRef.current = layout;
  onLayoutChangeRef.current = onLayoutChange;
  onDropWidgetRef.current = onDropWidget;

  // Callback ref for widget elements - called by React when elements mount/unmount
  const setWidgetRef = useCallback(
    (widgetId: string) => (el: HTMLDivElement | null) => {
      if (el) {
        widgetElementsRef.current.set(widgetId, el);
      } else {
        widgetElementsRef.current.delete(widgetId);
      }
    },
    []
  );

  // Initialize GridStack on mount
  useEffect(() => {
    if (!gridContainerRef.current) return;

    if (!gridRef.current) {
      // Initialize GridStack
      gridRef.current = GridStack.init(
        {
          // Issue #478: true to preserve inline widget x-positions
          float: true,
          cellHeight: 80,
          column: 12,
          margin: 16,
          handleClass: 'cursor-move',
          disableDrag: !isEditing,
          disableResize: !isEditing,
          acceptWidgets: true,
        },
        gridContainerRef.current
      );

      /**
       * Issue #476: Helper function to process GridStack items and update layout
       * Filters out invalid intermediate resize states (null from toWidgetConfig)
       */
      const processLayoutUpdate = () => {
        if (!gridRef.current || gridRef.current._ignoreCB) return;

        const items = gridRef.current.save(false);
        if (!Array.isArray(items)) return;

        // Build a map of widget types by id from current layout
        const currentLayout = layoutRef.current;
        const typeMap = new Map<string, WidgetType>();
        currentLayout.widgets.forEach((w) => typeMap.set(w.i, w.type));

        const validItems = items.filter((item) => item.id && typeMap.has(item.id));

        // Issue #476: Convert to WidgetConfig, filtering out invalid intermediate states
        const updatedWidgets = validItems
          .map((item) => toWidgetConfig(item, typeMap.get(item.id!)!))
          .filter((widget): widget is WidgetConfig => widget !== null);

        // Issue #476: Skip update if any widget had invalid dimensions (intermediate resize state)
        // This prevents 400 errors during resize drag
        if (updatedWidgets.length !== validItems.length) {
          return;
        }

        // Only update if positions actually changed
        const hasChanges = updatedWidgets.some((widget) => {
          const original = currentLayout.widgets.find((w) => w.i === widget.i);
          return (
            original &&
            (original.x !== widget.x ||
              original.y !== widget.y ||
              original.w !== widget.w ||
              original.h !== widget.h)
          );
        });

        if (hasChanges) {
          onLayoutChangeRef.current({
            ...currentLayout,
            widgets: updatedWidgets,
          });
        }
      };

      // Handle layout changes from GridStack (programmatic changes, constraint adjustments)
      gridRef.current.on('change', processLayoutUpdate);

      // Issue #476: Handle resize completion - fires AFTER user finishes resizing
      // This ensures we capture the final valid dimensions, not intermediate states
      gridRef.current.on('resizestop', processLayoutUpdate);

      // Issue #476: Handle drag completion - fires AFTER user finishes dragging
      gridRef.current.on('dragstop', processLayoutUpdate);

      // Handle drop of new widget from catalog
      gridRef.current.on('dropped', (_event, _previousWidget, newWidget) => {
        if (!newWidget || !onDropWidgetRef.current) return;

        // Get widget type from data transfer (set by widget catalog)
        const widgetType = newWidget.el?.dataset?.widgetType as WidgetType;
        if (widgetType) {
          onDropWidgetRef.current(widgetType, newWidget.x ?? 0, newWidget.y ?? 0);
          // Remove the dropped placeholder - actual widget will be added via layout
          gridRef.current?.removeWidget(newWidget.el!, false);
        }
      });
    }

    return () => {
      // Cleanup on unmount
      if (gridRef.current) {
        gridRef.current.destroy(false);
        gridRef.current = null;
      }
    };
    // Only run on mount - we use refs for callbacks to avoid re-initialization
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Sync edit mode with GridStack
  useEffect(() => {
    if (!gridRef.current) return;

    gridRef.current.enableMove(isEditing);
    gridRef.current.enableResize(isEditing);
  }, [isEditing]);

  // Sync layout changes from React state to GridStack
  // Issue #478: Use update() instead of load() for React-managed DOM
  // load() doesn't properly update positions when DOM is controlled by React
  useEffect(() => {
    if (!gridRef.current) return;

    // Prevent callback loop
    gridRef.current._ignoreCB = true;

    // Use batchUpdate to prevent multiple relayouts during sync
    gridRef.current.batchUpdate(true);

    // Explicitly update each widget's position
    // This works correctly with React-managed DOM elements
    layout.widgets.forEach((widget) => {
      const el = widgetElementsRef.current.get(widget.i);
      if (el) {
        gridRef.current!.update(el, {
          x: widget.x,
          y: widget.y,
          w: widget.w,
          h: widget.h,
        });
      }
    });

    gridRef.current.batchUpdate(false);

    // Re-enable callbacks
    gridRef.current._ignoreCB = false;
  }, [layout.widgets]);

  return (
    <div ref={gridContainerRef} className="grid-stack">
      {layout.widgets.map((widget) => {
        const Definition = getWidget(widget.type);
        const WidgetComponent = Definition?.Component;

        return (
          <div
            key={widget.i}
            ref={setWidgetRef(widget.i)}
            className="grid-stack-item"
            gs-id={widget.i}
            gs-x={String(widget.x)}
            gs-y={String(widget.y)}
            gs-w={String(widget.w)}
            gs-h={String(widget.h)}
            gs-min-w={widget.minW !== undefined ? String(widget.minW) : undefined}
            gs-min-h={widget.minH !== undefined ? String(widget.minH) : undefined}
            gs-max-w={widget.maxW !== undefined ? String(widget.maxW) : undefined}
            gs-max-h={widget.maxH !== undefined ? String(widget.maxH) : undefined}
          >
            <div className="grid-stack-item-content">
              <WidgetWrapper
                widgetId={widget.i}
                widgetType={widget.type}
                isEditing={isEditing}
                onRemove={() => onRemoveWidget(widget.i)}
              >
                {WidgetComponent ? (
                  <WidgetComponent
                    widgetId={widget.i}
                    onRemove={() => onRemoveWidget(widget.i)}
                  />
                ) : (
                  <div className="flex items-center justify-center h-full text-gray-400">
                    Widget not found: {widget.type}
                  </div>
                )}
              </WidgetWrapper>
            </div>
          </div>
        );
      })}
    </div>
  );
}
