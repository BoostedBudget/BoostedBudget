import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import type { WidgetDefinition } from '../types'

const mockGetWidget = mock(() => undefined as WidgetDefinition | undefined)

mock.module('./registry', () => ({
  getWidget: mockGetWidget,
}))

import { WidgetWrapper } from './WidgetWrapper'

describe('WidgetWrapper', () => {
  const mockOnRemove = mock(() => {})

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
    mockGetWidget.mockClear()
    mockOnRemove.mockClear()
    mockGetWidget.mockReturnValue(mockDefinition)
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

    it('should show widget type as fallback when definition not found', () => {
      mockGetWidget.mockReturnValue(undefined)

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

      expect(screen.getByText('balance-overview')).toBeInTheDocument()
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

    it('should open confirmation dialog without calling onRemove', () => {
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

      expect(screen.getByRole('dialog')).toBeInTheDocument()
      expect(mockOnRemove).not.toHaveBeenCalled()
    })

    it('should stop event propagation on remove click', () => {
      const parentClickHandler = mock(() => {})

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
    it('should show confirmation dialog with title and message', () => {
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

      expect(screen.getByText('Remove Widget')).toBeInTheDocument()
      expect(screen.getByText(/Are you sure you want to remove/)).toBeInTheDocument()
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
      expect(wrapper).toHaveClass('h-full', 'bg-white', 'rounded-lg', 'shadow')
      expect(wrapper).not.toHaveClass('absolute', 'inset-0')
    })

    it('should have min-h-0 and overflow-auto on content container', () => {
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

      const contentContainer = container.querySelector('.flex-1.overflow-auto')
      expect(contentContainer).toBeInTheDocument()
      expect(contentContainer).toHaveClass('min-h-0')
    })

    it('should have dark mode and border classes', () => {
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
      expect(wrapper).toHaveClass(
        'dark:bg-gray-800',
        'border',
        'border-gray-200',
        'dark:border-gray-700'
      )
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

      expect(mockGetWidget).toHaveBeenCalledWith('cash-flow-summary')
    })
  })

  describe('Edit Mode Visual Outline', () => {
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
      expect(wrapper).toHaveClass('outline', 'outline-2', 'outline-blue-400', '-outline-offset-2')
    })

    it('should not show outline when not editing', () => {
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
      expect(wrapper).not.toHaveClass('outline', 'outline-2', 'outline-blue-400')
    })

    it('should maintain other styling classes when editing', () => {
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
      expect(wrapper).toHaveClass(
        'h-full',
        'bg-white',
        'rounded-lg',
        'outline',
        'outline-blue-400'
      )
    })
  })
})
