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
// toWidgetConfig removed - Issue #478: We now read directly from DOM attributes
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
       * Issue #476, #478: Helper function to process GridStack items and update layout
       *
       * Issue #478: Fixed to read positions from DOM attributes instead of save(false).
       * GridStack's save(false) returns incorrect x-positions for React-managed elements,
       * but GridStack correctly updates gs-x/gs-y DOM attributes after drag.
       */
      const processLayoutUpdate = () => {
        if (!gridRef.current || gridRef.current._ignoreCB) return;

        // Build a map of widget types by id from current layout
        const currentLayout = layoutRef.current;
        const typeMap = new Map<string, WidgetType>();
        currentLayout.widgets.forEach((w) => typeMap.set(w.i, w.type));

        // Issue #478: Read positions directly from DOM attributes that GridStack updates after drag
        // This fixes the bug where save(false) returns x=0 for React-managed elements
        const gridItems = gridRef.current.getGridItems();
        const updatedWidgets: WidgetConfig[] = [];

        gridItems.forEach((el: HTMLElement) => {
          const id = el.getAttribute('gs-id');
          if (!id || !typeMap.has(id)) return;

          // Read actual positions from DOM attributes
          const x = parseInt(el.getAttribute('gs-x') || '0', 10);
          const y = parseInt(el.getAttribute('gs-y') || '0', 10);
          const w = parseInt(el.getAttribute('gs-w') || '1', 10);
          const h = parseInt(el.getAttribute('gs-h') || '1', 10);

          // Issue #476: Skip invalid intermediate states (e.g., during resize drag)
          if (w < 1 || h < 1) return;

          const widget: WidgetConfig = {
            i: id,
            type: typeMap.get(id)!,
            x,
            y,
            w,
            h,
          };

          // Preserve min/max constraints from DOM attributes
          const minW = el.getAttribute('gs-min-w');
          const minH = el.getAttribute('gs-min-h');
          const maxW = el.getAttribute('gs-max-w');
          const maxH = el.getAttribute('gs-max-h');
          if (minW) widget.minW = parseInt(minW, 10);
          if (minH) widget.minH = parseInt(minH, 10);
          if (maxW) widget.maxW = parseInt(maxW, 10);
          if (maxH) widget.maxH = parseInt(maxH, 10);

          updatedWidgets.push(widget);
        });

        // Skip update if we couldn't get all widgets
        if (updatedWidgets.length !== currentLayout.widgets.length) {
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

    // Get IDs of widgets GridStack is actually managing (in engine.nodes)
    // NOTE: getGridItems() returns ALL .grid-stack-item DOM children, not just tracked ones!
    // We must use engine.nodes to know which widgets are registered with GridStack
    const gridItems = gridRef.current.getGridItems();
    const trackedIds = new Set(
      gridRef.current.engine.nodes.map((n) => n.id).filter(Boolean) as string[]
    );
    const layoutIds = new Set(layout.widgets.map((w) => w.i));

    // Process each widget in the layout
    layout.widgets.forEach((widget) => {
      const el = widgetElementsRef.current.get(widget.i);
      if (!el) return;

      if (!trackedIds.has(widget.i)) {
        // NEW widget - register with GridStack first
        // makeWidget reads from gs-* attributes set by React
        gridRef.current!.makeWidget(el);
      }

      // Update position and constraints (works for both new and existing widgets)
      // Issue #486: MUST pass minW/minH/maxW/maxH to enforce size constraints
      // Without these, GridStack loses constraint info and allows any resize
      gridRef.current!.update(el, {
        x: widget.x,
        y: widget.y,
        w: widget.w,
        h: widget.h,
        minW: widget.minW,
        minH: widget.minH,
        maxW: widget.maxW,
        maxH: widget.maxH,
      });
    });

    // Clean up removed widgets (GridStack still tracking but not in layout)
    // This prevents stale references and potential memory leaks
    gridItems.forEach((el) => {
      const id = el.getAttribute('gs-id');
      if (id && !layoutIds.has(id)) {
        gridRef.current!.removeWidget(el, false); // false = don't remove from DOM (React handles that)
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
