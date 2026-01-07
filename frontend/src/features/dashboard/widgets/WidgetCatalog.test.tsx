/**
 * Widget Catalog Tests
 * Issue #354: Customizable Dashboard Widget System
 * Issue #127: Added API mock for widget types validation
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { WidgetCatalog } from './WidgetCatalog'
import * as registry from './registry'
import type { WidgetDefinition } from '../types'

// Mock the registry
vi.mock('./registry', () => ({
  getAllWidgets: vi.fn(),
}))

// Mock the API hook - returns all widget types as valid
vi.mock('@/api/generated/endpoints/dashboard/dashboard', () => ({
  useGetWidgetTypes: vi.fn(() => ({
    data: {
      widgetTypes: [
        { type: 'balance-overview', name: 'Balance Overview', category: 'overview' },
        { type: 'balance-history', name: 'Balance History', category: 'charts' },
        { type: 'cash-flow-summary', name: 'Cash Flow Summary', category: 'overview' },
        { type: 'cash-flow-chart', name: 'Cash Flow Chart', category: 'charts' },
        { type: 'account-cards', name: 'Account Cards', category: 'accounts' },
        { type: 'net-worth', name: 'Net Worth', category: 'overview' },
      ],
    },
    isLoading: false,
    error: null,
  })),
}))

describe('WidgetCatalog', () => {
  const mockOnClose = vi.fn()
  const mockOnAddWidget = vi.fn()

  const mockWidgets: WidgetDefinition[] = [
    {
      type: 'balance-overview',
      title: 'Balance Overview',
      description: 'Current balance summary with period comparison',
      icon: 'banknotes',
      category: 'overview',
      defaultSize: { w: 4, h: 3 },
      minSize: { w: 3, h: 2 },
      Component: () => null,
    },
    {
      type: 'cash-flow-summary',
      title: 'Cash Flow Summary',
      description: 'Income and expenses summary',
      icon: 'chart-bar',
      category: 'overview',
      defaultSize: { w: 8, h: 3 },
      minSize: { w: 6, h: 2 },
      Component: () => null,
    },
    {
      type: 'balance-history',
      title: 'Balance History',
      description: 'Historical balance chart',
      icon: 'chart-line',
      category: 'charts',
      defaultSize: { w: 6, h: 4 },
      minSize: { w: 4, h: 3 },
      Component: () => null,
    },
    {
      type: 'account-cards',
      title: 'Account Cards',
      description: 'View your linked accounts',
      icon: 'credit-card',
      category: 'accounts',
      defaultSize: { w: 4, h: 3 },
      minSize: { w: 3, h: 2 },
      Component: () => null,
    },
  ]

  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(registry.getAllWidgets).mockReturnValue(mockWidgets)
  })

  describe('Modal Display', () => {
    it('should render when open', () => {
      render(
        <WidgetCatalog
          isOpen={true}
          onClose={mockOnClose}
          onAddWidget={mockOnAddWidget}
        />
      )

      expect(screen.getByText('Add Widget')).toBeInTheDocument()
    })

    it('should not render when closed', () => {
      render(
        <WidgetCatalog
          isOpen={false}
          onClose={mockOnClose}
          onAddWidget={mockOnAddWidget}
        />
      )

      expect(screen.queryByText('Add Widget')).not.toBeInTheDocument()
    })

    it('should call onClose when close button clicked', async () => {
      render(
        <WidgetCatalog
          isOpen={true}
          onClose={mockOnClose}
          onAddWidget={mockOnAddWidget}
        />
      )

      const closeButton = screen.getAllByRole('button').find(btn => btn.querySelector('svg'))
      if (closeButton) {
        fireEvent.click(closeButton)
      }

      expect(mockOnClose).toHaveBeenCalled()
    })
  })

  describe('Widget Display', () => {
    it('should display all widgets from registry', () => {
      render(
        <WidgetCatalog
          isOpen={true}
          onClose={mockOnClose}
          onAddWidget={mockOnAddWidget}
        />
      )

      expect(screen.getByText('Balance Overview')).toBeInTheDocument()
      expect(screen.getByText('Cash Flow Summary')).toBeInTheDocument()
      expect(screen.getByText('Balance History')).toBeInTheDocument()
      expect(screen.getByText('Account Cards')).toBeInTheDocument()
    })

    it('should display widget descriptions', () => {
      render(
        <WidgetCatalog
          isOpen={true}
          onClose={mockOnClose}
          onAddWidget={mockOnAddWidget}
        />
      )

      expect(screen.getByText('Current balance summary with period comparison')).toBeInTheDocument()
      expect(screen.getByText('Income and expenses summary')).toBeInTheDocument()
    })

    it('should display widget default sizes', () => {
      render(
        <WidgetCatalog
          isOpen={true}
          onClose={mockOnClose}
          onAddWidget={mockOnAddWidget}
        />
      )

      expect(screen.getAllByText('4×3').length).toBeGreaterThanOrEqual(1)
      expect(screen.getByText('8×3')).toBeInTheDocument()
      expect(screen.getByText('6×4')).toBeInTheDocument()
    })
  })

  describe('Category Grouping', () => {
    it('should group widgets by category', () => {
      render(
        <WidgetCatalog
          isOpen={true}
          onClose={mockOnClose}
          onAddWidget={mockOnAddWidget}
        />
      )

      // Category labels
      expect(screen.getByText('Overview')).toBeInTheDocument()
      expect(screen.getByText('Charts')).toBeInTheDocument()
      expect(screen.getByText('Accounts')).toBeInTheDocument()
    })

    it('should show correct category labels', () => {
      render(
        <WidgetCatalog
          isOpen={true}
          onClose={mockOnClose}
          onAddWidget={mockOnAddWidget}
        />
      )

      // Capitalized category labels
      expect(screen.getByText('Overview')).toBeInTheDocument()
      expect(screen.getByText('Charts')).toBeInTheDocument()
      expect(screen.getByText('Accounts')).toBeInTheDocument()
    })
  })

  describe('Add Widget', () => {
    it('should call onAddWidget when widget card clicked', () => {
      render(
        <WidgetCatalog
          isOpen={true}
          onClose={mockOnClose}
          onAddWidget={mockOnAddWidget}
        />
      )

      // Find and click a widget card
      fireEvent.click(screen.getByText('Balance Overview'))

      expect(mockOnAddWidget).toHaveBeenCalledWith('balance-overview')
    })

    it('should close modal after adding widget', () => {
      render(
        <WidgetCatalog
          isOpen={true}
          onClose={mockOnClose}
          onAddWidget={mockOnAddWidget}
        />
      )

      fireEvent.click(screen.getByText('Balance History'))

      expect(mockOnAddWidget).toHaveBeenCalledWith('balance-history')
      expect(mockOnClose).toHaveBeenCalled()
    })

    it('should call onAddWidget with correct widget type', () => {
      render(
        <WidgetCatalog
          isOpen={true}
          onClose={mockOnClose}
          onAddWidget={mockOnAddWidget}
        />
      )

      fireEvent.click(screen.getByText('Account Cards'))

      expect(mockOnAddWidget).toHaveBeenCalledWith('account-cards')
    })
  })

  describe('Already Added State', () => {
    it('should show "Added" badge for existing widgets', () => {
      render(
        <WidgetCatalog
          isOpen={true}
          onClose={mockOnClose}
          onAddWidget={mockOnAddWidget}
          existingWidgetTypes={['balance-overview', 'cash-flow-summary']}
        />
      )

      // "Added" text should appear for existing widgets
      const addedBadges = screen.getAllByText('Added')
      expect(addedBadges.length).toBe(2)
    })

    it('should not call onAddWidget for already added widgets', () => {
      render(
        <WidgetCatalog
          isOpen={true}
          onClose={mockOnClose}
          onAddWidget={mockOnAddWidget}
          existingWidgetTypes={['balance-overview']}
        />
      )

      fireEvent.click(screen.getByText('Balance Overview'))
      expect(mockOnAddWidget).not.toHaveBeenCalled()
    })

    it('should allow adding widgets not in existingWidgetTypes', () => {
      render(
        <WidgetCatalog
          isOpen={true}
          onClose={mockOnClose}
          onAddWidget={mockOnAddWidget}
          existingWidgetTypes={['balance-overview']}
        />
      )

      fireEvent.click(screen.getByText('Balance History'))
      expect(mockOnAddWidget).toHaveBeenCalledWith('balance-history')
    })
  })

  describe('Empty State', () => {
    it('should handle empty widget list', () => {
      vi.mocked(registry.getAllWidgets).mockReturnValue([])

      render(
        <WidgetCatalog
          isOpen={true}
          onClose={mockOnClose}
          onAddWidget={mockOnAddWidget}
        />
      )

      // Should still render the modal
      expect(screen.getByText('Add Widget')).toBeInTheDocument()
    })
  })

  describe('Footer Text', () => {
    it('should show help text', () => {
      render(
        <WidgetCatalog
          isOpen={true}
          onClose={mockOnClose}
          onAddWidget={mockOnAddWidget}
        />
      )

      expect(screen.getByText(/Drag widgets to reorder/)).toBeInTheDocument()
      expect(screen.getByText(/Resize from corners/)).toBeInTheDocument()
      expect(screen.getByText(/Click × to remove/)).toBeInTheDocument()
    })
  })

  describe('Registry Integration', () => {
    it('should call getAllWidgets on render', () => {
      render(
        <WidgetCatalog
          isOpen={true}
          onClose={mockOnClose}
          onAddWidget={mockOnAddWidget}
        />
      )

      expect(registry.getAllWidgets).toHaveBeenCalled()
    })
  })
})
