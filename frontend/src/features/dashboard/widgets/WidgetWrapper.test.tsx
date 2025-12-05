/**
 * Widget Wrapper Tests
 *
 * Tests the WidgetWrapper component including header rendering,
 * edit mode controls, and remove button functionality.
 *
 * Issue #354: Customizable Dashboard Widget System
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { WidgetWrapper } from './WidgetWrapper'
import * as registry from './registry'
import type { WidgetDefinition } from '../types'

// Mock the registry
vi.mock('./registry', () => ({
  getWidget: vi.fn(),
}))

describe('WidgetWrapper', () => {
  const mockOnRemove = vi.fn()

  const mockDefinition: WidgetDefinition = {
    type: 'balance-overview',
    title: 'Balance Overview',
    description: 'Balance overview widget',
    icon: 'banknotes',
    category: 'overview',
    defaultSize: { w: 4, h: 3 },
    minSize: { w: 3, h: 2 },
    Component: () => null,
  }

  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(registry.getWidget).mockReturnValue(mockDefinition)
  })

  describe('Header', () => {
    it('should show widget title from registry', () => {
      render(
        <WidgetWrapper
          widgetId="widget-1"
          widgetType="balance-overview"
          isEditing={false}
          onRemove={mockOnRemove}
        >
          <div>Content</div>
        </WidgetWrapper>
      )

      expect(screen.getByText('Balance Overview')).toBeInTheDocument()
    })

    it('should show widget type when definition not found', () => {
      vi.mocked(registry.getWidget).mockReturnValue(undefined)

      render(
        <WidgetWrapper
          widgetId="widget-1"
          widgetType="unknown-type"
          isEditing={false}
          onRemove={mockOnRemove}
        >
          <div>Content</div>
        </WidgetWrapper>
      )

      expect(screen.getByText('unknown-type')).toBeInTheDocument()
    })

    it('should show drag handle in edit mode', () => {
      render(
        <WidgetWrapper
          widgetId="widget-1"
          widgetType="balance-overview"
          isEditing={true}
          onRemove={mockOnRemove}
        >
          <div>Content</div>
        </WidgetWrapper>
      )

      // Drag handle has cursor-move class
      const dragHandle = document.querySelector('.cursor-move')
      expect(dragHandle).toBeInTheDocument()
    })

    it('should hide drag handle when not editing', () => {
      render(
        <WidgetWrapper
          widgetId="widget-1"
          widgetType="balance-overview"
          isEditing={false}
          onRemove={mockOnRemove}
        >
          <div>Content</div>
        </WidgetWrapper>
      )

      const dragHandle = document.querySelector('.cursor-move')
      expect(dragHandle).not.toBeInTheDocument()
    })
  })

  describe('Remove Button', () => {
    it('should show remove button in edit mode', () => {
      render(
        <WidgetWrapper
          widgetId="widget-1"
          widgetType="balance-overview"
          isEditing={true}
          onRemove={mockOnRemove}
        >
          <div>Content</div>
        </WidgetWrapper>
      )

      expect(screen.getByLabelText('Remove widget')).toBeInTheDocument()
    })

    it('should hide remove button when not editing', () => {
      render(
        <WidgetWrapper
          widgetId="widget-1"
          widgetType="balance-overview"
          isEditing={false}
          onRemove={mockOnRemove}
        >
          <div>Content</div>
        </WidgetWrapper>
      )

      expect(screen.queryByLabelText('Remove widget')).not.toBeInTheDocument()
    })

    it('should call onRemove when remove button clicked', () => {
      render(
        <WidgetWrapper
          widgetId="widget-1"
          widgetType="balance-overview"
          isEditing={true}
          onRemove={mockOnRemove}
        >
          <div>Content</div>
        </WidgetWrapper>
      )

      fireEvent.click(screen.getByLabelText('Remove widget'))

      expect(mockOnRemove).toHaveBeenCalledTimes(1)
    })

    it('should stop event propagation on remove click', () => {
      const parentClickHandler = vi.fn()

      render(
        <div onClick={parentClickHandler}>
          <WidgetWrapper
            widgetId="widget-1"
            widgetType="balance-overview"
            isEditing={true}
            onRemove={mockOnRemove}
          >
            <div>Content</div>
          </WidgetWrapper>
        </div>
      )

      fireEvent.click(screen.getByLabelText('Remove widget'))

      expect(mockOnRemove).toHaveBeenCalledTimes(1)
      expect(parentClickHandler).not.toHaveBeenCalled()
    })

    it('should have title attribute for tooltip', () => {
      render(
        <WidgetWrapper
          widgetId="widget-1"
          widgetType="balance-overview"
          isEditing={true}
          onRemove={mockOnRemove}
        >
          <div>Content</div>
        </WidgetWrapper>
      )

      const removeButton = screen.getByLabelText('Remove widget')
      expect(removeButton).toHaveAttribute('title', 'Remove widget')
    })
  })

  describe('Content', () => {
    it('should render children content', () => {
      render(
        <WidgetWrapper
          widgetId="widget-1"
          widgetType="balance-overview"
          isEditing={false}
          onRemove={mockOnRemove}
        >
          <div data-testid="widget-content">Widget Content</div>
        </WidgetWrapper>
      )

      expect(screen.getByTestId('widget-content')).toBeInTheDocument()
      expect(screen.getByText('Widget Content')).toBeInTheDocument()
    })

    it('should render multiple children', () => {
      render(
        <WidgetWrapper
          widgetId="widget-1"
          widgetType="balance-overview"
          isEditing={false}
          onRemove={mockOnRemove}
        >
          <div data-testid="child-1">First</div>
          <div data-testid="child-2">Second</div>
        </WidgetWrapper>
      )

      expect(screen.getByTestId('child-1')).toBeInTheDocument()
      expect(screen.getByTestId('child-2')).toBeInTheDocument()
    })
  })

  describe('Styling', () => {
    it('should have correct container classes', () => {
      const { container } = render(
        <WidgetWrapper
          widgetId="widget-1"
          widgetType="balance-overview"
          isEditing={false}
          onRemove={mockOnRemove}
        >
          <div>Content</div>
        </WidgetWrapper>
      )

      const wrapper = container.firstChild as HTMLElement
      expect(wrapper).toHaveClass('h-full')
      expect(wrapper).toHaveClass('bg-white')
      expect(wrapper).toHaveClass('rounded-lg')
      expect(wrapper).toHaveClass('shadow')
    })

    it('should have dark mode classes', () => {
      const { container } = render(
        <WidgetWrapper
          widgetId="widget-1"
          widgetType="balance-overview"
          isEditing={false}
          onRemove={mockOnRemove}
        >
          <div>Content</div>
        </WidgetWrapper>
      )

      const wrapper = container.firstChild as HTMLElement
      expect(wrapper).toHaveClass('dark:bg-gray-800')
    })
  })

  describe('Registry Integration', () => {
    it('should call getWidget with widget type', () => {
      render(
        <WidgetWrapper
          widgetId="widget-1"
          widgetType="cash-flow-summary"
          isEditing={false}
          onRemove={mockOnRemove}
        >
          <div>Content</div>
        </WidgetWrapper>
      )

      expect(registry.getWidget).toHaveBeenCalledWith('cash-flow-summary')
    })
  })
})
