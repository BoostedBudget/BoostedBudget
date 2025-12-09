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
          widgetType="balance-overview"
          isEditing={false}
          onRemove={mockOnRemove}
        >
          <div>Content</div>
        </WidgetWrapper>
      )

      // When registry returns undefined, component shows type as fallback
      expect(screen.getByText('balance-overview')).toBeInTheDocument()
    })

// Note: Drag handle SVG was removed in Issue #443 as it was redundant
    // GridStack handles drag-and-drop via handleClass, no visual indicator needed
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

    it('should open confirmation dialog when remove button clicked', () => {
      // Issue #412: Changed behavior - now shows confirmation dialog instead of direct removal
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

      // Dialog should open, onRemove not called yet
      expect(screen.getByRole('dialog')).toBeInTheDocument()
      expect(mockOnRemove).not.toHaveBeenCalled()
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

      // Parent click should not be triggered
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

  describe('Remove Confirmation Dialog', () => {
    it('should show confirmation dialog when remove button clicked', async () => {
      // Issue #412: Delete confirmation before removing widget
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

      // Dialog should appear with title and confirmation message
      expect(screen.getByText('Remove Widget')).toBeInTheDocument()
      expect(screen.getByText(/Are you sure you want to remove/)).toBeInTheDocument()
    })

    it('should not call onRemove immediately when remove button clicked', () => {
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

      // Click remove button
      fireEvent.click(screen.getByLabelText('Remove widget'))

      // onRemove should NOT be called yet - dialog should show first
      expect(mockOnRemove).not.toHaveBeenCalled()
    })

    it('should call onRemove only after confirming in dialog', () => {
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

      // Click remove button to open dialog
      fireEvent.click(screen.getByLabelText('Remove widget'))

      // Click confirm in dialog
      fireEvent.click(screen.getByRole('button', { name: 'Remove' }))

      expect(mockOnRemove).toHaveBeenCalledTimes(1)
    })

    it('should not call onRemove when canceling dialog', () => {
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

      // Click remove button to open dialog
      fireEvent.click(screen.getByLabelText('Remove widget'))

      // Click cancel
      fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))

      expect(mockOnRemove).not.toHaveBeenCalled()
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
    it('should have correct container classes with h-full (GridStack provides positioning)', () => {
      // Issue #441: Changed from absolute inset-0 back to h-full
      // GridStack's .grid-stack-item-content provides the absolute positioning
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
      // Issue #441: h-full works now because GridStack parent has explicit height
      expect(wrapper).toHaveClass('h-full')
      expect(wrapper).not.toHaveClass('absolute')
      expect(wrapper).not.toHaveClass('inset-0')
      expect(wrapper).toHaveClass('bg-white')
      expect(wrapper).toHaveClass('rounded-lg')
      expect(wrapper).toHaveClass('shadow')
    })

    it('should have min-h-0 on content container to enable flex shrinking', () => {
      // Issue #410: Content container needs min-h-0 to allow flex item to shrink
      // below its content size, enabling overflow-auto to work correctly
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

      // Find the content container (the one with flex-1 and overflow-auto)
      const contentContainer = container.querySelector('.flex-1.overflow-auto')
      expect(contentContainer).toBeInTheDocument()
      expect(contentContainer).toHaveClass('min-h-0')
    })

    it('should have overflow-auto on content container for scrollable content', () => {
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

      const contentContainer = container.querySelector('.flex-1')
      expect(contentContainer).toBeInTheDocument()
      expect(contentContainer).toHaveClass('overflow-auto')
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

    it('should have border classes for clear widget outline', () => {
      // Issue #412: Widgets should have visible borders for clear visual boundaries
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
      expect(wrapper).toHaveClass('border')
      expect(wrapper).toHaveClass('border-gray-200')
      expect(wrapper).toHaveClass('dark:border-gray-700')
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

  /**
   * Edit Mode Visual Outline Tests - Issue #439, #443
   *
   * In edit mode, widgets should display a visible outline
   * to clearly indicate draggable/droppable bounds.
   *
   * Issue #443: Changed from ring (box-shadow) to outline because
   * ring was being clipped by parent overflow:hidden in grid libraries.
   * Outline is not affected by overflow and renders correctly.
   */
  describe('Edit Mode Visual Outline - Issue #439, #443', () => {
    it('should show outline when in edit mode', () => {
      const { container } = render(
        <WidgetWrapper
          widgetId="widget-1"
          widgetType="balance-overview"
          isEditing={true}
          onRemove={mockOnRemove}
        >
          <div>Content</div>
        </WidgetWrapper>
      )

      const wrapper = container.firstChild as HTMLElement
      expect(wrapper).toHaveClass('outline')
      expect(wrapper).toHaveClass('outline-2')
      expect(wrapper).toHaveClass('outline-blue-400')
      expect(wrapper).toHaveClass('-outline-offset-2')
    })

    it('should NOT show outline when not editing', () => {
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
      expect(wrapper).not.toHaveClass('outline')
      expect(wrapper).not.toHaveClass('outline-2')
      expect(wrapper).not.toHaveClass('outline-blue-400')
    })

    it('should maintain other styling classes in edit mode', () => {
      const { container } = render(
        <WidgetWrapper
          widgetId="widget-1"
          widgetType="balance-overview"
          isEditing={true}
          onRemove={mockOnRemove}
        >
          <div>Content</div>
        </WidgetWrapper>
      )

      const wrapper = container.firstChild as HTMLElement
      // Outline should be added without removing other classes
      // Issue #441: Using h-full instead of absolute/inset-0 (GridStack provides positioning)
      expect(wrapper).toHaveClass('h-full')
      expect(wrapper).toHaveClass('bg-white')
      expect(wrapper).toHaveClass('rounded-lg')
      expect(wrapper).toHaveClass('outline')
      expect(wrapper).toHaveClass('outline-blue-400')
    })
  })
})
