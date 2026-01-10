/**
 * Dashboard Grid Tests
 *
 * Tests the DashboardGrid component with GridStack.js
 * Issue #441: Migrate from react-grid-layout to GridStack.js
 *
 * Issue #354: Customizable Dashboard Widget System
 */

import { describe, it, expect, mock, beforeEach, afterEach } from 'bun:test'
import React from 'react'
import type {
  DashboardLayout,
  WidgetType,
  WidgetProps,
  WidgetDefinition,
} from '../types'

const mockGridStackOn = mock(() => {})
const mockGridStackOff = mock(() => {})
const mockGridStackLoad = mock(() => {})
const mockGridStackSave = mock(() => [])
const mockGridStackEnableMove = mock(() => {})
const mockGridStackEnableResize = mock(() => {})
const mockGridStackRemoveWidget = mock(() => {})
const mockGridStackDestroy = mock(() => {})
const mockGridStackBatchUpdate = mock(() => {})
const mockGridStackUpdate = mock(() => {})
const mockGridStackGetGridItems = mock(() => [])
const mockGridStackMakeWidget = mock(() => {})

const mockGridStackInstance = {
  on: mockGridStackOn,
  off: mockGridStackOff,
  load: mockGridStackLoad,
  save: mockGridStackSave,
  enableMove: mockGridStackEnableMove,
  enableResize: mockGridStackEnableResize,
  removeWidget: mockGridStackRemoveWidget,
  destroy: mockGridStackDestroy,
  batchUpdate: mockGridStackBatchUpdate,
  update: mockGridStackUpdate,
  getGridItems: mockGridStackGetGridItems,
  makeWidget: mockGridStackMakeWidget,
  engine: {
    nodes: [] as Array<{ id?: string }>,
  },
  _ignoreCB: false,
}

mock.module('gridstack', () => ({
  GridStack: {
    init: mock(() => mockGridStackInstance),
  },
}))

const mockGetWidget = mock(() => undefined as WidgetDefinition | undefined)
mock.module('./registry', () => ({
  getWidget: mockGetWidget,
}))

mock.module('./WidgetWrapper', () => ({
  WidgetWrapper: ({
    children,
    onRemove,
    widgetType,
  }: {
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

import { render, screen, fireEvent } from '@testing-library/react'
import { DashboardGrid } from './DashboardGrid'

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

  const mockOnLayoutChange = mock(() => {})
  const mockOnRemoveWidget = mock(() => {})
  const mockOnDropWidget = mock(() => {})

  const MockWidgetComponent = ({ widgetId }: WidgetProps) => (
    <div data-testid={`widget-content-${widgetId}`}>Widget content</div>
  )

  beforeEach(() => {
    ;[
      mockGridStackOn,
      mockGridStackOff,
      mockGridStackLoad,
      mockGridStackSave,
      mockGridStackEnableMove,
      mockGridStackEnableResize,
      mockGridStackDestroy,
      mockGridStackBatchUpdate,
      mockGridStackUpdate,
      mockGridStackGetGridItems,
      mockGridStackMakeWidget,
      mockOnLayoutChange,
      mockOnRemoveWidget,
      mockOnDropWidget,
    ].forEach((m) => m.mockClear())

    mockGridStackGetGridItems.mockReturnValue([])
    mockGridStackInstance._ignoreCB = false

    mockGetWidget.mockImplementation((type: WidgetType) => {
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

  afterEach(() => {
    mockGetWidget.mockClear()
  })

  describe('Rendering', () => {
    it('should render grid-stack container', () => {
      const { container } = render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={false}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      )

      expect(container.querySelector('.grid-stack')).toBeInTheDocument()
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

      expect(
        screen.getByTestId('widget-wrapper-balance-overview')
      ).toBeInTheDocument()
      expect(
        screen.getByTestId('widget-wrapper-cash-flow-summary')
      ).toBeInTheDocument()
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
      mockGetWidget.mockReturnValue(undefined)

      render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={false}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      )

      expect(
        screen.getByText(/Widget not found: balance-overview/)
      ).toBeInTheDocument()
    })

    it('should render empty grid when no widgets', () => {
      const emptyLayout: DashboardLayout = { widgets: [], version: 1 }

      const { container } = render(
        <DashboardGrid
          layout={emptyLayout}
          isEditing={false}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      )

      expect(container.querySelector('.grid-stack')).toBeInTheDocument()
      expect(
        screen.queryByTestId('widget-wrapper-balance-overview')
      ).not.toBeInTheDocument()
    })
  })

  describe('GridStack Initialization - Issue #441', () => {
    it('should render widgets with gs-* data attributes and content wrappers', () => {
      const { container } = render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={false}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      )

      const gridItems = container.querySelectorAll('.grid-stack-item')
      expect(gridItems.length).toBe(2)

      const widget1 = gridItems[0] as HTMLElement
      expect(widget1.getAttribute('gs-id')).toBe('widget-1')
      expect(widget1.getAttribute('gs-x')).toBe('0')
      expect(widget1.getAttribute('gs-y')).toBe('0')
      expect(widget1.getAttribute('gs-w')).toBe('4')
      expect(widget1.getAttribute('gs-h')).toBe('3')
      expect(widget1.getAttribute('gs-min-w')).toBe('3')
      expect(widget1.getAttribute('gs-min-h')).toBe('2')

      const widget2 = gridItems[1] as HTMLElement
      expect(widget2.getAttribute('gs-id')).toBe('widget-2')
      expect(widget2.getAttribute('gs-x')).toBe('4')

      const contentWrappers = container.querySelectorAll(
        '.grid-stack-item-content'
      )
      expect(contentWrappers.length).toBe(2)
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

      expect(screen.getByTestId('widget-content-widget-1')).toBeInTheDocument()
      expect(screen.getByTestId('widget-content-widget-2')).toBeInTheDocument()
    })
  })

  describe('Drop Handler', () => {
    it('should accept onDropWidget prop', () => {
      const { container } = render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={true}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
          onDropWidget={mockOnDropWidget}
        />
      )

      expect(container.querySelector('.grid-stack')).toBeInTheDocument()
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

      expect(mockGetWidget).toHaveBeenCalledWith('balance-overview')
      expect(mockGetWidget).toHaveBeenCalledWith('cash-flow-summary')
    })
  })

  describe('Grid Cell Height Chain Fix - Issue #422, #441', () => {
    it('should use grid-stack-item with content wrappers in both modes', () => {
      const { container } = render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={false}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      )

      let gridItems = container.querySelectorAll('.grid-stack-item')
      expect(gridItems.length).toBe(mockLayout.widgets.length)
      gridItems.forEach((item) => {
        const content = item.querySelector('.grid-stack-item-content')
        expect(content).toBeInTheDocument()
      })

      const { container: editContainer } = render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={true}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      )

      gridItems = editContainer.querySelectorAll('.grid-stack-item')
      gridItems.forEach((item) => {
        expect(item.querySelector('.grid-stack-item-content')).toBeInTheDocument()
      })
    })

    it('should NOT use legacy h-full or grid-cell-content classes', () => {
      const { container } = render(
        <DashboardGrid
          layout={mockLayout}
          isEditing={false}
          onLayoutChange={mockOnLayoutChange}
          onRemoveWidget={mockOnRemoveWidget}
        />
      )

      const oldClassElements = container.querySelectorAll('.grid-cell-content')
      expect(oldClassElements.length).toBe(0)
    })
  })
})
