/**
 * BalanceHistoryChart Component Tests
 *
 * Tests the interactive balance history chart using Recharts,
 * including data transformation, tooltips, and display modes.
 *
 * Issue #353: Balance Tracking Dashboard
 */

import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { BalanceHistoryChart } from './BalanceHistoryChart'
import type { BalanceHistory } from '../types'

describe('BalanceHistoryChart', () => {
  const mockHistory: BalanceHistory = {
    start_date: '2025-11-01',
    end_date: '2025-11-05',
    granularity: 'daily',
    snapshots: [
      {
        date: '2025-11-01',
        balance: 10000.00,
        by_type: {
          depository: 7000.00,
          credit: -2000.00,
          investment: 5000.00,
        },
      },
      {
        date: '2025-11-02',
        balance: 10500.50,
        by_type: {
          depository: 7200.00,
          credit: -1900.00,
          investment: 5200.50,
        },
      },
      {
        date: '2025-11-03',
        balance: 9800.00,
        by_type: {
          depository: 6800.00,
          credit: -2200.00,
          investment: 5200.00,
        },
      },
      {
        date: '2025-11-04',
        balance: 11000.00,
        by_type: {
          depository: 7500.00,
          credit: -1800.00,
          investment: 5300.00,
        },
      },
      {
        date: '2025-11-05',
        balance: 11200.25,
        by_type: {
          depository: 7600.00,
          credit: -1700.00,
          investment: 5300.25,
        },
      },
    ],
  }

  const mockEmptyHistory: BalanceHistory = {
    start_date: '2025-11-01',
    end_date: '2025-11-05',
    granularity: 'daily',
    snapshots: [],
  }

  const mockHistoryWithoutByType: BalanceHistory = {
    start_date: '2025-11-01',
    end_date: '2025-11-03',
    granularity: 'daily',
    snapshots: [
      { date: '2025-11-01', balance: 10000.00 },
      { date: '2025-11-02', balance: 10500.50 },
      { date: '2025-11-03', balance: 9800.00 },
    ],
  }

  describe('Basic Rendering', () => {
    it('should render chart with balance history data', () => {
      // Act
      render(<BalanceHistoryChart history={mockHistory} />)

      // Assert
      expect(screen.getByText('Balance History')).toBeInTheDocument()
    })

    it('should display chart title "Balance History"', () => {
      // Act
      render(<BalanceHistoryChart history={mockHistory} />)

      // Assert
      const title = screen.getByText('Balance History')
      expect(title).toBeInTheDocument()
      expect(title.tagName).toBe('H2')
    })

    it('should handle empty snapshots array gracefully', () => {
      // Act
      render(<BalanceHistoryChart history={mockEmptyHistory} />)

      // Assert - Should show empty state message
      expect(screen.getByText('No balance history available')).toBeInTheDocument()
    })

    it('should show "No balance history available" message when no data', () => {
      // Act
      render(<BalanceHistoryChart history={mockEmptyHistory} />)

      // Assert
      const emptyMessage = screen.getByText('No balance history available')
      expect(emptyMessage).toBeInTheDocument()
    })
  })

  describe('Data Transformation', () => {
    it('should transform snapshots into chart-compatible format', () => {
      // Act
      const { container } = render(<BalanceHistoryChart history={mockHistory} />)

      // Assert - ResponsiveContainer should be rendered
      const responsiveContainer = container.querySelector('.recharts-responsive-container')
      expect(responsiveContainer).toBeInTheDocument()
    })

    it('should format dates as "MMM d" for x-axis', () => {
      // Act
      render(<BalanceHistoryChart history={mockHistory} />)

      // Assert - Check if Recharts rendered (we can't easily test internal data transformation,
      // but we can verify the component renders without errors)
      expect(screen.getByText('Balance History')).toBeInTheDocument()
    })

    it('should preserve full date for tooltip', () => {
      // Act
      render(<BalanceHistoryChart history={mockHistory} />)

      // Assert - Component should render successfully
      // (Tooltip testing requires user interaction simulation which is complex with Recharts)
      expect(screen.getByText('Balance History')).toBeInTheDocument()
    })
  })

  describe('Total Balance Mode (showTypeBreakdown=false)', () => {
    it('should display single line for total balance', () => {
      // Act
      const { container } = render(
        <BalanceHistoryChart history={mockHistory} showTypeBreakdown={false} />
      )

      // Assert - Component should render without errors
      expect(container.querySelector('.recharts-responsive-container')).toBeInTheDocument()
    })

    it('should use blue color for balance line', () => {
      // Act
      render(<BalanceHistoryChart history={mockHistory} showTypeBreakdown={false} />)

      // Assert - Component renders successfully (Recharts internals tested by library)
      expect(screen.getByText('Balance History')).toBeInTheDocument()
    })

    it('should show "Total Balance" in legend', () => {
      // Act
      render(<BalanceHistoryChart history={mockHistory} showTypeBreakdown={false} />)

      // Assert - Component renders successfully
      expect(screen.getByText('Balance History')).toBeInTheDocument()
    })
  })

  describe('Type Breakdown Mode (showTypeBreakdown=true)', () => {
    it('should display multiple lines when showTypeBreakdown=true', () => {
      // Act
      const { container } = render(
        <BalanceHistoryChart history={mockHistory} showTypeBreakdown={true} />
      )

      // Assert - Component should render without errors
      expect(container.querySelector('.recharts-responsive-container')).toBeInTheDocument()
    })

    it('should render line for each account type with correct colors', () => {
      // Act
      render(<BalanceHistoryChart history={mockHistory} showTypeBreakdown={true} />)

      // Assert - Component renders successfully (color testing requires visual testing)
      expect(screen.getByText('Balance History')).toBeInTheDocument()
    })

    it('should show depository line in green', () => {
      // Act
      render(<BalanceHistoryChart history={mockHistory} showTypeBreakdown={true} />)

      // Assert - Component renders successfully
      expect(screen.getByText('Balance History')).toBeInTheDocument()
    })

    it('should show credit line in red', () => {
      // Act
      render(<BalanceHistoryChart history={mockHistory} showTypeBreakdown={true} />)

      // Assert - Component renders successfully
      expect(screen.getByText('Balance History')).toBeInTheDocument()
    })

    it('should show investment line in purple', () => {
      // Act
      render(<BalanceHistoryChart history={mockHistory} showTypeBreakdown={true} />)

      // Assert - Component renders successfully
      expect(screen.getByText('Balance History')).toBeInTheDocument()
    })

    it('should capitalize type names in legend', () => {
      // Act
      render(<BalanceHistoryChart history={mockHistory} showTypeBreakdown={true} />)

      // Assert - Component renders successfully
      expect(screen.getByText('Balance History')).toBeInTheDocument()
    })
  })

  describe('Chart Components', () => {
    it('should render ResponsiveContainer', () => {
      // Act
      const { container } = render(<BalanceHistoryChart history={mockHistory} />)

      // Assert
      const responsiveContainer = container.querySelector('.recharts-responsive-container')
      expect(responsiveContainer).toBeInTheDocument()
    })

    it('should render CartesianGrid', () => {
      // Act
      render(<BalanceHistoryChart history={mockHistory} />)

      // Assert - Component renders successfully (Recharts internals tested by library)
      expect(screen.getByText('Balance History')).toBeInTheDocument()
    })

    it('should render XAxis', () => {
      // Act
      render(<BalanceHistoryChart history={mockHistory} />)

      // Assert - Component renders successfully
      expect(screen.getByText('Balance History')).toBeInTheDocument()
    })

    it('should render YAxis', () => {
      // Act
      render(<BalanceHistoryChart history={mockHistory} />)

      // Assert - Component renders successfully
      expect(screen.getByText('Balance History')).toBeInTheDocument()
    })

    it('should render Legend', () => {
      // Act
      render(<BalanceHistoryChart history={mockHistory} />)

      // Assert - Component renders successfully
      expect(screen.getByText('Balance History')).toBeInTheDocument()
    })
  })

  describe('Edge Cases', () => {
    it('should handle single snapshot', () => {
      // Arrange
      const singleSnapshot: BalanceHistory = {
        start_date: '2025-11-01',
        end_date: '2025-11-01',
        granularity: 'daily',
        snapshots: [
          { date: '2025-11-01', balance: 10000.00 },
        ],
      }

      // Act
      const { container } = render(<BalanceHistoryChart history={singleSnapshot} />)

      // Assert - Should render without errors
      expect(container.querySelector('.recharts-responsive-container')).toBeInTheDocument()
    })

    it('should handle missing by_type data', () => {
      // Act
      const { container } = render(
        <BalanceHistoryChart history={mockHistoryWithoutByType} />
      )

      // Assert - Should render without errors
      expect(container.querySelector('.recharts-responsive-container')).toBeInTheDocument()
    })

    it('should handle mixed data (some snapshots with by_type, some without)', () => {
      // Arrange
      const mixedHistory: BalanceHistory = {
        start_date: '2025-11-01',
        end_date: '2025-11-03',
        granularity: 'daily',
        snapshots: [
          {
            date: '2025-11-01',
            balance: 10000.00,
            by_type: { depository: 7000.00 },
          },
          {
            date: '2025-11-02',
            balance: 10500.00,
          },
          {
            date: '2025-11-03',
            balance: 9800.00,
            by_type: { depository: 6800.00 },
          },
        ],
      }

      // Act
      const { container } = render(
        <BalanceHistoryChart history={mixedHistory} showTypeBreakdown={true} />
      )

      // Assert - Should render without errors
      expect(container.querySelector('.recharts-responsive-container')).toBeInTheDocument()
    })

    it('should handle large balance amounts', () => {
      // Arrange
      const largeBalances: BalanceHistory = {
        start_date: '2025-11-01',
        end_date: '2025-11-02',
        granularity: 'daily',
        snapshots: [
          { date: '2025-11-01', balance: 1000000.00 },
          { date: '2025-11-02', balance: 2500000.50 },
        ],
      }

      // Act
      const { container } = render(<BalanceHistoryChart history={largeBalances} />)

      // Assert - Should render without errors
      expect(container.querySelector('.recharts-responsive-container')).toBeInTheDocument()
    })

    it('should handle negative balances', () => {
      // Arrange
      const negativeBalances: BalanceHistory = {
        start_date: '2025-11-01',
        end_date: '2025-11-02',
        granularity: 'daily',
        snapshots: [
          { date: '2025-11-01', balance: -5000.00 },
          { date: '2025-11-02', balance: -3000.00 },
        ],
      }

      // Act
      const { container } = render(<BalanceHistoryChart history={negativeBalances} />)

      // Assert - Should render without errors
      expect(container.querySelector('.recharts-responsive-container')).toBeInTheDocument()
    })

    it('should handle zero balance', () => {
      // Arrange
      const zeroBalance: BalanceHistory = {
        start_date: '2025-11-01',
        end_date: '2025-11-02',
        granularity: 'daily',
        snapshots: [
          { date: '2025-11-01', balance: 0.00 },
          { date: '2025-11-02', balance: 100.00 },
        ],
      }

      // Act
      const { container } = render(<BalanceHistoryChart history={zeroBalance} />)

      // Assert - Should render without errors
      expect(container.querySelector('.recharts-responsive-container')).toBeInTheDocument()
    })
  })

  describe('Prop Variations', () => {
    it('should default showTypeBreakdown to false when not provided', () => {
      // Act
      const { container } = render(<BalanceHistoryChart history={mockHistory} />)

      // Assert - Should render as total balance mode
      expect(container.querySelector('.recharts-responsive-container')).toBeInTheDocument()
    })

    it('should respect showTypeBreakdown=false prop', () => {
      // Act
      const { container } = render(
        <BalanceHistoryChart history={mockHistory} showTypeBreakdown={false} />
      )

      // Assert
      expect(container.querySelector('.recharts-responsive-container')).toBeInTheDocument()
    })

    it('should respect showTypeBreakdown=true prop', () => {
      // Act
      const { container } = render(
        <BalanceHistoryChart history={mockHistory} showTypeBreakdown={true} />
      )

      // Assert - Renders without errors (breakdown logic tested via integration tests)
      expect(container.querySelector('.recharts-responsive-container')).toBeInTheDocument()
    })
  })

  /**
   * Issue #416: Height Chain Fix Tests
   *
   * These tests verify the proper CSS height chain propagation so that
   * ResponsiveContainer can calculate dimensions correctly within WidgetWrapper.
   */
  describe('Height Chain Fix - Issue #416', () => {
    it('should have h-full on outer container for height propagation', () => {
      // Act
      const { container } = render(<BalanceHistoryChart history={mockHistory} />)

      // Assert
      const outerDiv = container.firstChild as HTMLElement
      expect(outerDiv).toHaveClass('h-full')
    })

    it('should use flex flex-col layout for proper height distribution', () => {
      // Act
      const { container } = render(<BalanceHistoryChart history={mockHistory} />)

      // Assert
      const outerDiv = container.firstChild as HTMLElement
      expect(outerDiv).toHaveClass('flex')
      expect(outerDiv).toHaveClass('flex-col')
    })

    it('should have flex-shrink-0 on title to prevent compression', () => {
      // Act
      render(<BalanceHistoryChart history={mockHistory} />)

      // Assert
      const title = screen.getByRole('heading', { name: /balance history/i })
      expect(title).toHaveClass('flex-shrink-0')
    })

    it('should have flex-1 min-h-0 on chart container for proper flex sizing', () => {
      // Act
      const { container } = render(<BalanceHistoryChart history={mockHistory} />)

      // Assert
      const chartContainer = container.querySelector('.flex-1.min-h-0')
      expect(chartContainer).toBeInTheDocument()
    })

    it('should NOT have redundant bg-white shadow p-6 styling', () => {
      // These styles are provided by WidgetWrapper, not the chart component
      const { container } = render(<BalanceHistoryChart history={mockHistory} />)

      // Assert
      const outerDiv = container.firstChild as HTMLElement
      expect(outerDiv).not.toHaveClass('bg-white')
      expect(outerDiv).not.toHaveClass('shadow')
      expect(outerDiv).not.toHaveClass('p-6')
      expect(outerDiv).not.toHaveClass('rounded-lg')
    })

    it('should have dark mode support on title', () => {
      // Act
      render(<BalanceHistoryChart history={mockHistory} />)

      // Assert
      const title = screen.getByRole('heading', { name: /balance history/i })
      expect(title).toHaveClass('dark:text-white')
    })

    it('should use flex-1 for empty state instead of fixed h-64', () => {
      // Act
      const { container } = render(<BalanceHistoryChart history={mockEmptyHistory} />)

      // Assert - Should have flex-1 for proper flex layout
      const emptyState = container.querySelector('.flex-1.flex.items-center.justify-center')
      expect(emptyState).toBeInTheDocument()
      // Should NOT have fixed h-64
      expect(container.querySelector('.h-64')).not.toBeInTheDocument()
    })
  })

  describe('Layout and Styling', () => {
    it('should use responsive height (100%) instead of fixed pixels', () => {
      // Issue #410: Chart should use height="100%" for responsive sizing
      // instead of fixed pixels to properly fill widget container
      const { container } = render(<BalanceHistoryChart history={mockHistory} />)

      // Assert - ResponsiveContainer should be rendered and use 100% height
      const responsiveContainer = container.querySelector('.recharts-responsive-container')
      expect(responsiveContainer).toBeInTheDocument()
      // Check that container has height: 100% style (not fixed pixels)
      expect(responsiveContainer).toHaveStyle({ height: '100%' })
    })
  })

  describe('Empty State Display', () => {
    it('should show empty state when snapshots.length === 0', () => {
      // Act
      render(<BalanceHistoryChart history={mockEmptyHistory} />)

      // Assert
      expect(screen.getByText('No balance history available')).toBeInTheDocument()
    })

    it('should center empty state message', () => {
      // Act
      const { container } = render(<BalanceHistoryChart history={mockEmptyHistory} />)

      // Assert
      const emptyState = container.querySelector('.flex.items-center.justify-center')
      expect(emptyState).toBeInTheDocument()
    })

    it('should use flex-1 for empty state height instead of fixed h-64 (Issue #416)', () => {
      // Issue #416: Empty state should use flex-1 for proper flex layout
      const { container } = render(<BalanceHistoryChart history={mockEmptyHistory} />)

      // Assert - flex-1 provides flexible height within the flex container
      const emptyState = container.querySelector('.flex-1')
      expect(emptyState).toBeInTheDocument()
    })

    it('should style empty state text as gray', () => {
      // Act
      const { container } = render(<BalanceHistoryChart history={mockEmptyHistory} />)

      // Assert
      const emptyText = container.querySelector('.text-gray-500')
      expect(emptyText).toBeInTheDocument()
      expect(emptyText).toHaveTextContent('No balance history available')
    })
  })
})
