/**
 * Remove Widget Dialog Tests
 *
 * Tests the confirmation dialog for removing widgets.
 * Issue #412: Dashboard widget UX improvements
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { RemoveWidgetDialog } from './RemoveWidgetDialog'

describe('RemoveWidgetDialog', () => {
  const mockOnConfirm = vi.fn()
  const mockOnCancel = vi.fn()

  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Display', () => {
    it('should render when open', () => {
      render(
        <RemoveWidgetDialog
          isOpen={true}
          widgetTitle="Balance Overview"
          onConfirm={mockOnConfirm}
          onCancel={mockOnCancel}
        />
      )

      expect(screen.getByText('Remove Widget')).toBeInTheDocument()
    })

    it('should not render when closed', () => {
      render(
        <RemoveWidgetDialog
          isOpen={false}
          widgetTitle="Balance Overview"
          onConfirm={mockOnConfirm}
          onCancel={mockOnCancel}
        />
      )

      expect(screen.queryByText('Remove Widget')).not.toBeInTheDocument()
    })

    it('should display widget title in message', () => {
      render(
        <RemoveWidgetDialog
          isOpen={true}
          widgetTitle="Cash Flow Summary"
          onConfirm={mockOnConfirm}
          onCancel={mockOnCancel}
        />
      )

      expect(screen.getByText(/Cash Flow Summary/)).toBeInTheDocument()
    })

    it('should display reassurance message about adding back', () => {
      render(
        <RemoveWidgetDialog
          isOpen={true}
          widgetTitle="Balance Overview"
          onConfirm={mockOnConfirm}
          onCancel={mockOnCancel}
        />
      )

      expect(screen.getByText(/add it back anytime/)).toBeInTheDocument()
    })
  })

  describe('Actions', () => {
    it('should call onConfirm when Remove button clicked', () => {
      render(
        <RemoveWidgetDialog
          isOpen={true}
          widgetTitle="Balance Overview"
          onConfirm={mockOnConfirm}
          onCancel={mockOnCancel}
        />
      )

      fireEvent.click(screen.getByRole('button', { name: 'Remove' }))

      expect(mockOnConfirm).toHaveBeenCalledTimes(1)
    })

    it('should call onCancel when Cancel button clicked', () => {
      render(
        <RemoveWidgetDialog
          isOpen={true}
          widgetTitle="Balance Overview"
          onConfirm={mockOnConfirm}
          onCancel={mockOnCancel}
        />
      )

      fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))

      expect(mockOnCancel).toHaveBeenCalledTimes(1)
    })
  })

  describe('Accessibility', () => {
    it('should have dialog role', () => {
      render(
        <RemoveWidgetDialog
          isOpen={true}
          widgetTitle="Balance Overview"
          onConfirm={mockOnConfirm}
          onCancel={mockOnCancel}
        />
      )

      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })

    it('should have proper heading', () => {
      render(
        <RemoveWidgetDialog
          isOpen={true}
          widgetTitle="Balance Overview"
          onConfirm={mockOnConfirm}
          onCancel={mockOnCancel}
        />
      )

      expect(screen.getByRole('heading', { name: 'Remove Widget' })).toBeInTheDocument()
    })
  })
})
