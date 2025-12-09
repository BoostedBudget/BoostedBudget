/**
 * Dashboard Grid Tests
 *
 * Tests the DashboardGrid component with GridStack.js
 * Issue #441: Migrate from react-grid-layout to GridStack.js
 *
 * Issue #354: Customizable Dashboard Widget System
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { DashboardGrid } from './DashboardGrid';
import * as registry from './registry';
import type {
  DashboardLayout,
  WidgetType,
  WidgetProps,
  WidgetDefinition,
} from '../types';

// Mock GridStack
const mockGridStackInstance = {
  on: vi.fn(),
  off: vi.fn(),
  load: vi.fn(),
  save: vi.fn(() => []),
  enableMove: vi.fn(),
  enableResize: vi.fn(),
  removeWidget: vi.fn(),
  destroy: vi.fn(),
  _ignoreCB: false,
};

vi.mock('gridstack', () => ({
  GridStack: {
    init: vi.fn(() => mockGridStackInstance),
  },
}));

// Mock the registry
vi.mock('./registry', () => ({
  getWidget: vi.fn(),
}));

// Mock WidgetWrapper
vi.mock('./WidgetWrapper', () => ({
  WidgetWrapper: ({
    children,
    onRemove,
    widgetType,
  }: {
    children: React.ReactNode;
    onRemove: () => void;
    widgetType: string;
  }) => (
    <div data-testid={`widget-wrapper-${widgetType}`}>
      <button data-testid={`remove-${widgetType}`} onClick={onRemove}>
        Remove
      </button>
      {children}
    </div>
  ),
}));

describe('DashboardGrid', () => {
  const mockLayout: DashboardLayout = {
    widgets: [
      {
        i: 'widget-1',
        type: 'balance-overview',
        x: 0,
        y: 0,
        w: 4,
        h: 3,
        minW: 3,
        minH: 2,
      },
      {
        i: 'widget-2',
        type: 'cash-flow-summary',
        x: 4,
        y: 0,
        w: 8,
        h: 3,
        minW: 6,
        minH: 2,
      },
    ],
    version: 1,
  };

  const mockOnLayoutChange = vi.fn();
  const mockOnRemoveWidget = vi.fn();
  const mockOnDropWidget = vi.fn();

  // Mock widget component
  const MockWidgetComponent = ({ widgetId }: WidgetProps) => (
    <div data-testid={`widget-content-${widgetId}`}>Widget content</div>
  );

  beforeEach(() => {
    vi.clearAllMocks();

    // Reset mock instance
    mockGridStackInstance.on.mockClear();
    mockGridStackInstance.off.mockClear();
    mockGridStackInstance.load.mockClear();
    mockGridStackInstance.save.mockClear();
    mockGridStackInstance.enableMove.mockClear();
    mockGridStackInstance.enableResize.mockClear();
    mockGridStackInstance.destroy.mockClear();
    mockGridStackInstance._ignoreCB = false;

    // Setup default mock registry behavior
    vi.mocked(registry.getWidget).mockImplementation((type: WidgetType) => {
      const definitions: Record<string, WidgetDefinition> = {
        'balance-overview': {
          type: 'balance-overview',
          title: 'Balance Overview',
          description: 'Balance overview widget',
          icon: 'banknotes',
          category: 'overview',
          defaultSize: { w: 4, h: 3 },
          minSize: { w: 3, h: 2 },
          Component: MockWidgetComponent,
        },
        'cash-flow-summary': {
          type: 'cash-flow-summary',
          title: 'Cash Flow Summary',
          description: 'Cash flow summary widget',
          icon: 'chart-bar',
          category: 'overview',
          defaultSize: { w: 8, h: 3 },
          minSize: { w: 6, h: 2 },
          Component: MockWidgetComponent,
        },
      };
      return definitions[type];
    });
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  describe('Rendering', () => {
    it('should render grid-stack container', () => {
      const { container } = render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={false}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      );

      expect(container.querySelector('.grid-stack')).toBeInTheDocument();
    });

    it('should render all widgets from layout', () => {
      render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={false}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      );

      expect(
        screen.getByTestId('widget-wrapper-balance-overview')
      ).toBeInTheDocument();
      expect(
        screen.getByTestId('widget-wrapper-cash-flow-summary')
      ).toBeInTheDocument();
    });

    it('should render widget content from registry component', () => {
      render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={false}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      );

      expect(screen.getByTestId('widget-content-widget-1')).toBeInTheDocument();
      expect(screen.getByTestId('widget-content-widget-2')).toBeInTheDocument();
    });

    it('should show fallback for unregistered widget type', () => {
      vi.mocked(registry.getWidget).mockReturnValue(undefined);

      render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={false}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      );

      expect(
        screen.getByText(/Widget not found: balance-overview/)
      ).toBeInTheDocument();
    });

    it('should render empty grid when no widgets', () => {
      const emptyLayout: DashboardLayout = { widgets: [], version: 1 };

      const { container } = render(
        <DashboardGrid
          layout={emptyLayout}
          isEditing={false}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      );

      expect(container.querySelector('.grid-stack')).toBeInTheDocument();
      expect(
        screen.queryByTestId('widget-wrapper-balance-overview')
      ).not.toBeInTheDocument();
    });
  });

  describe('GridStack Initialization - Issue #441', () => {
    it('should render widgets with gs-* data attributes', () => {
      const { container } = render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={false}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      );

      const gridItems = container.querySelectorAll('.grid-stack-item');
      expect(gridItems.length).toBe(2);

      // First widget
      const widget1 = gridItems[0] as HTMLElement;
      expect(widget1.getAttribute('gs-id')).toBe('widget-1');
      expect(widget1.getAttribute('gs-x')).toBe('0');
      expect(widget1.getAttribute('gs-y')).toBe('0');
      expect(widget1.getAttribute('gs-w')).toBe('4');
      expect(widget1.getAttribute('gs-h')).toBe('3');
      expect(widget1.getAttribute('gs-min-w')).toBe('3');
      expect(widget1.getAttribute('gs-min-h')).toBe('2');

      // Second widget
      const widget2 = gridItems[1] as HTMLElement;
      expect(widget2.getAttribute('gs-id')).toBe('widget-2');
      expect(widget2.getAttribute('gs-x')).toBe('4');
    });

    it('should render grid-stack-item-content wrapper for each widget', () => {
      const { container } = render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={false}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      );

      const contentWrappers = container.querySelectorAll(
        '.grid-stack-item-content'
      );
      expect(contentWrappers.length).toBe(2);
    });
  });

  describe('Widget Removal', () => {
    it('should call onRemoveWidget when remove button clicked', () => {
      render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={true}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      );

      fireEvent.click(screen.getByTestId('remove-balance-overview'));

      expect(mockOnRemoveWidget).toHaveBeenCalledWith('widget-1');
    });

    it('should call onRemoveWidget with correct widget id for each widget', () => {
      render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={true}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      );

      fireEvent.click(screen.getByTestId('remove-cash-flow-summary'));

      expect(mockOnRemoveWidget).toHaveBeenCalledWith('widget-2');
    });
  });

  describe('Props Passing', () => {
    it('should pass widgetId to widget component', () => {
      render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={false}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      );

      // Widget content uses widgetId
      expect(screen.getByTestId('widget-content-widget-1')).toBeInTheDocument();
      expect(screen.getByTestId('widget-content-widget-2')).toBeInTheDocument();
    });
  });

  describe('Drop Handler', () => {
    it('should accept onDropWidget prop', () => {
      // Just verify it renders without error when prop is provided
      const { container } = render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={true}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
          onDropWidget={mockOnDropWidget}
        />
      );

      expect(container.querySelector('.grid-stack')).toBeInTheDocument();
    });
  });

  describe('Registry Integration', () => {
    it('should call getWidget for each widget in layout', () => {
      render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={false}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      );

      expect(registry.getWidget).toHaveBeenCalledWith('balance-overview');
      expect(registry.getWidget).toHaveBeenCalledWith('cash-flow-summary');
    });
  });

  /**
   * Grid Cell Height Propagation Tests - Issue #422, #441
   *
   * GridStack.js provides its own .grid-stack-item-content wrapper
   * with position: absolute; inset: 0 - solving the height chain issue.
   *
   * Unlike react-grid-layout which merged classes onto the same element,
   * GridStack creates a SEPARATE child wrapper with explicit positioning.
   */
  describe('Grid Cell Height Chain Fix - Issue #422, #441', () => {
    it('should use grid-stack-item class for grid items', () => {
      const { container } = render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={false}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      );

      const gridItems = container.querySelectorAll('.grid-stack-item');
      expect(gridItems.length).toBe(mockLayout.widgets.length);
    });

    it('should have grid-stack-item-content on each widget for height propagation', () => {
      const { container } = render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={false}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      );

      const gridItems = container.querySelectorAll('.grid-stack-item');
      gridItems.forEach((item) => {
        const content = item.querySelector('.grid-stack-item-content');
        expect(content).toBeInTheDocument();
      });
    });

    it('should have grid-stack-item-content in edit mode', () => {
      const { container } = render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={true}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      );

      const gridItems = container.querySelectorAll('.grid-stack-item');
      gridItems.forEach((item) => {
        expect(item.querySelector('.grid-stack-item-content')).toBeInTheDocument();
      });
    });

    it('should NOT use h-full or grid-cell-content classes (GridStack provides height)', () => {
      const { container } = render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={false}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      );

      // GridStack handles height via .grid-stack-item-content
      // We should NOT have the old classes that caused issues
      const oldClassElements = container.querySelectorAll('.grid-cell-content');
      expect(oldClassElements.length).toBe(0);
    });
  });
});
