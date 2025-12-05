/**
 * Dashboard Grid Component
 * Issue #354: Customizable Dashboard Widget System
 *
 * Uses react-grid-layout for drag-drop and resize functionality.
 * Renders widgets using WidgetWrapper and the widget registry.
 */

import { useMemo } from 'react';
import { Responsive, WidthProvider, Layout } from 'react-grid-layout';
import 'react-grid-layout/css/styles.css';
import 'react-resizable/css/styles.css';

import { WidgetWrapper } from './WidgetWrapper';
import { getWidget } from './registry';
import type { WidgetConfig, WidgetType, DashboardLayout } from '../types';

const ResponsiveGridLayout = WidthProvider(Responsive);

interface DashboardGridProps {
  layout: DashboardLayout;
  isEditing: boolean;
  onLayoutChange: (layout: DashboardLayout) => void;
  onRemoveWidget: (widgetId: string) => void;
  onDropWidget?: (widgetType: WidgetType, x: number, y: number) => void;
}

// Convert WidgetConfig to react-grid-layout Layout item
function toLayoutItem(widget: WidgetConfig): Layout {
  return {
    i: widget.i,
    x: widget.x,
    y: widget.y,
    w: widget.w,
    h: widget.h,
    minW: widget.minW,
    minH: widget.minH,
    maxW: widget.maxW,
    maxH: widget.maxH,
    static: widget.static,
  };
}

// Convert react-grid-layout Layout item back to WidgetConfig
function toWidgetConfig(
  layoutItem: Layout,
  widgets: WidgetConfig[]
): WidgetConfig | null {
  const originalWidget = widgets.find((w) => w.i === layoutItem.i);
  if (!originalWidget) return null;

  return {
    ...originalWidget,
    x: layoutItem.x,
    y: layoutItem.y,
    w: layoutItem.w,
    h: layoutItem.h,
  };
}

export function DashboardGrid({
  layout,
  isEditing,
  onLayoutChange,
  onRemoveWidget,
  onDropWidget,
}: DashboardGridProps) {
  // Convert widgets to react-grid-layout format
  const gridLayout = useMemo(
    () => layout.widgets.map(toLayoutItem),
    [layout.widgets]
  );

  // Handle layout changes from react-grid-layout
  const handleLayoutChange = (currentLayout: Layout[]) => {
    const updatedWidgets = currentLayout
      .map((item) => toWidgetConfig(item, layout.widgets))
      .filter((w): w is WidgetConfig => w !== null);

    // Only update if positions actually changed
    const hasChanges = updatedWidgets.some((widget) => {
      const original = layout.widgets.find((w) => w.i === widget.i);
      return (
        original &&
        (original.x !== widget.x ||
          original.y !== widget.y ||
          original.w !== widget.w ||
          original.h !== widget.h)
      );
    });

    if (hasChanges) {
      onLayoutChange({
        ...layout,
        widgets: updatedWidgets,
      });
    }
  };

  // Handle drop of new widget from catalog
  const handleDrop = (
    _layout: Layout[],
    item: Layout,
    e: DragEvent
  ) => {
    const widgetType = e.dataTransfer?.getData('widgetType') as WidgetType;
    if (widgetType && onDropWidget) {
      onDropWidget(widgetType, item.x, item.y);
    }
  };

  return (
    <ResponsiveGridLayout
      className="layout"
      layouts={{ lg: gridLayout, md: gridLayout, sm: gridLayout }}
      breakpoints={{ lg: 1200, md: 996, sm: 768 }}
      cols={{ lg: 12, md: 10, sm: 6 }}
      rowHeight={80}
      onLayoutChange={handleLayoutChange}
      isDraggable={isEditing}
      isResizable={isEditing}
      isDroppable={isEditing}
      onDrop={handleDrop}
      droppingItem={{ i: '__dropping__', w: 4, h: 3 }}
      draggableHandle=".cursor-move"
      margin={[16, 16]}
      containerPadding={[0, 0]}
      useCSSTransforms={true}
      compactType="vertical"
    >
      {layout.widgets.map((widget) => {
        const Definition = getWidget(widget.type);
        const WidgetComponent = Definition?.Component;

        return (
          <div key={widget.i}>
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
        );
      })}
    </ResponsiveGridLayout>
  );
}
