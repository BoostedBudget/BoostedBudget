/**
 * Dashboard Grid Tests
 *
 * Tests the DashboardGrid component including widget rendering,
 * layout conversion, and widget removal.
 *
 * Issue #354: Customizable Dashboard Widget System
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { DashboardGrid } from './DashboardGrid'
import * as registry from './registry'
import type { DashboardLayout, WidgetType, WidgetProps, WidgetDefinition } from '../types'

// Mock react-grid-layout to simplify testing
vi.mock('react-grid-layout', () => ({
  Responsive: ({ children, onLayoutChange }: {
    children: React.ReactNode
    onLayoutChange: (layout: unknown[]) => void
  }) => (
    <div data-testid="responsive-grid" onClick={() => onLayoutChange([])}>
      {children}
    </div>
  ),
  WidthProvider: (Component: React.ComponentType) => Component,
}))

// Mock the registry
vi.mock('./registry', () => ({
  getWidget: vi.fn(),
}))

// Mock WidgetWrapper
vi.mock('./WidgetWrapper', () => ({
  WidgetWrapper: ({ children, onRemove, widgetType }: {
    children: React.ReactNode
    onRemove: () => void
    widgetType: string
  }) => (
    <div data-testid={`widget-wrapper-${widgetType}`}>
      <button data-testid={`remove-${widgetType}`} onClick={onRemove}>
        Remove
      </button>
      {children}
    </div>
  ),
}))

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
  }

  const mockOnLayoutChange = vi.fn()
  const mockOnRemoveWidget = vi.fn()
  const mockOnDropWidget = vi.fn()

  // Mock widget component
  const MockWidgetComponent = ({ widgetId }: WidgetProps) => (
    <div data-testid={`widget-content-${widgetId}`}>Widget content</div>
  )

  beforeEach(() => {
    vi.clearAllMocks()

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
      }
      return definitions[type]
    })
  })

  describe('Rendering', () => {
    it('should render grid layout container', () => {
      render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={false}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      )

      expect(screen.getByTestId('responsive-grid')).toBeInTheDocument()
    })

    it('should render all widgets from layout', () => {
      render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={false}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      )

      expect(screen.getByTestId('widget-wrapper-balance-overview')).toBeInTheDocument()
      expect(screen.getByTestId('widget-wrapper-cash-flow-summary')).toBeInTheDocument()
    })

    it('should render widget content from registry component', () => {
      render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={false}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      )

      expect(screen.getByTestId('widget-content-widget-1')).toBeInTheDocument()
      expect(screen.getByTestId('widget-content-widget-2')).toBeInTheDocument()
    })

    it('should show fallback for unregistered widget type', () => {
      vi.mocked(registry.getWidget).mockReturnValue(undefined)

      render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={false}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      )

      expect(screen.getByText(/Widget not found: balance-overview/)).toBeInTheDocument()
    })

    it('should render empty grid when no widgets', () => {
      const emptyLayout: DashboardLayout = { widgets: [], version: 1 }

      render(
        <DashboardGrid
          layout={emptyLayout}
          isEditing={false}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      )

      expect(screen.getByTestId('responsive-grid')).toBeInTheDocument()
      expect(screen.queryByTestId('widget-wrapper-balance-overview')).not.toBeInTheDocument()
    })
  })

  describe('Widget Removal', () => {
    it('should call onRemoveWidget when remove button clicked', () => {
      render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={true}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      )

      fireEvent.click(screen.getByTestId('remove-balance-overview'))

      expect(mockOnRemoveWidget).toHaveBeenCalledWith('widget-1')
    })

    it('should call onRemoveWidget with correct widget id for each widget', () => {
      render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={true}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      )

      fireEvent.click(screen.getByTestId('remove-cash-flow-summary'))

      expect(mockOnRemoveWidget).toHaveBeenCalledWith('widget-2')
    })
  })

  describe('Props Passing', () => {
    it('should pass widgetId to widget component', () => {
      render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={false}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      )

      // Widget content uses widgetId
      expect(screen.getByTestId('widget-content-widget-1')).toBeInTheDocument()
      expect(screen.getByTestId('widget-content-widget-2')).toBeInTheDocument()
    })
  })

  describe('Drop Handler', () => {
    it('should accept onDropWidget prop', () => {
      // Just verify it renders without error when prop is provided
      render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={true}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
          onDropWidget={mockOnDropWidget}
        />
      )

      expect(screen.getByTestId('responsive-grid')).toBeInTheDocument()
    })
  })

  describe('Registry Integration', () => {
    it('should call getWidget for each widget in layout', () => {
      render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={false}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      )

      expect(registry.getWidget).toHaveBeenCalledWith('balance-overview')
      expect(registry.getWidget).toHaveBeenCalledWith('cash-flow-summary')
    })
  })
})
