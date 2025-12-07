/**
 * BalanceOverview Component Tests
 *
 * Tests the balance overview component with period comparisons
 * and formatted currency/date displays.
 *
 * Issue #353: Balance Tracking Dashboard
 */

import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { BalanceOverview } from './BalanceOverview'
import type { BalanceSummary } from '../types'

describe('BalanceOverview', () => {
  const mockSummaryWithPositiveChange: BalanceSummary = {
    as_of_date: '2025-11-15',
    total_balance: 15000.50,
    by_type: {
      depository: {
        balance: 5000.00,
        account_count: 2,
        accounts: [],
      },
    },
    period_comparison: {
      previous_balance: 14000.00,
      change_amount: 1000.50,
      change_percent: 7.15,
    },
  }

  const mockSummaryWithNegativeChange: BalanceSummary = {
    as_of_date: '2025-11-15',
    total_balance: 13000.00,
    by_type: {
      depository: {
        balance: 5000.00,
        account_count: 2,
        accounts: [],
      },
    },
    period_comparison: {
      previous_balance: 14000.00,
      change_amount: -1000.00,
      change_percent: -7.14,
    },
  }

  const mockSummaryWithZeroChange: BalanceSummary = {
    as_of_date: '2025-11-15',
    total_balance: 14000.00,
    by_type: {
      depository: {
        balance: 5000.00,
        account_count: 2,
        accounts: [],
      },
    },
    period_comparison: {
      previous_balance: 14000.00,
      change_amount: 0.00,
      change_percent: 0.00,
    },
  }

  const mockSummaryWithoutComparison: BalanceSummary = {
    as_of_date: '2025-11-15',
    total_balance: 15000.50,
    by_type: {
      depository: {
        balance: 5000.00,
        account_count: 2,
        accounts: [],
      },
    },
  }

  describe('Basic Rendering', () => {
    it('should display "Total Balance" heading', () => {
      // Act
      render(<BalanceOverview summary={mockSummaryWithPositiveChange} />)

      // Assert
      expect(screen.getByText('Total Balance')).toBeInTheDocument()
    })

    it('should show total balance formatted as currency', () => {
      // Act
      render(<BalanceOverview summary={mockSummaryWithPositiveChange} />)

      // Assert
      expect(screen.getByText('$15,000.50')).toBeInTheDocument()
    })

    it('should display as-of date in "MMM d, yyyy" format', () => {
      // Act
      render(<BalanceOverview summary={mockSummaryWithPositiveChange} />)

      // Assert
      expect(screen.getByText(/as of Nov 15, 2025/)).toBeInTheDocument()
    })
  })

  describe('Period Comparison - Positive Change', () => {
    it('should show green upward arrow for positive change', () => {
      // Act
      const { container } = render(<BalanceOverview summary={mockSummaryWithPositiveChange} />)

      // Assert - Check for green text color and upward arrow
      const changeElement = container.querySelector('.text-green-600')
      expect(changeElement).toBeInTheDocument()
      expect(changeElement).toHaveTextContent('↑')
    })

    it('should display change amount with positive styling', () => {
      // Act
      const { container } = render(<BalanceOverview summary={mockSummaryWithPositiveChange} />)

      // Assert
      const changeElement = container.querySelector('.text-green-600')
      expect(changeElement).toHaveTextContent('$1,000.50')
    })

    it('should show change percentage', () => {
      // Act
      render(<BalanceOverview summary={mockSummaryWithPositiveChange} />)

      // Assert
      expect(screen.getByText('(7.15%)')).toBeInTheDocument()
    })

    it('should format change amount as currency without sign', () => {
      // Act
      const { container } = render(<BalanceOverview summary={mockSummaryWithPositiveChange} />)

      // Assert - Should show $1,000.50, not +$1,000.50
      const changeElement = container.querySelector('.text-green-600')
      expect(changeElement).toHaveTextContent('↑ $1,000.50')
      expect(changeElement).not.toHaveTextContent('+')
    })
  })

  describe('Period Comparison - Negative Change', () => {
    it('should show red downward arrow for negative change', () => {
      // Act
      const { container } = render(<BalanceOverview summary={mockSummaryWithNegativeChange} />)

      // Assert - Check for red text color and downward arrow
      const changeElement = container.querySelector('.text-red-600')
      expect(changeElement).toBeInTheDocument()
      expect(changeElement).toHaveTextContent('↓')
    })

    it('should display change amount with negative styling', () => {
      // Act
      const { container } = render(<BalanceOverview summary={mockSummaryWithNegativeChange} />)

      // Assert
      const changeElement = container.querySelector('.text-red-600')
      expect(changeElement).toHaveTextContent('$1,000.00')
    })

    it('should show change percentage', () => {
      // Act
      render(<BalanceOverview summary={mockSummaryWithNegativeChange} />)

      // Assert
      expect(screen.getByText('(-7.14%)')).toBeInTheDocument()
    })

    it('should format change amount as currency without sign', () => {
      // Act
      const { container } = render(<BalanceOverview summary={mockSummaryWithNegativeChange} />)

      // Assert - Should show $1,000.00, not -$1,000.00 (absolute value)
      const changeElement = container.querySelector('.text-red-600')
      expect(changeElement).toHaveTextContent('↓ $1,000.00')
      expect(changeElement).not.toHaveTextContent('-')
    })
  })

  describe('Period Comparison - No Change', () => {
    it('should handle zero change (0.00%)', () => {
      // Act
      const { container } = render(<BalanceOverview summary={mockSummaryWithZeroChange} />)

      // Assert - Zero change is considered positive (>=0), so green
      const changeElement = container.querySelector('.text-green-600')
      expect(changeElement).toBeInTheDocument()
      expect(changeElement).toHaveTextContent('↑ $0.00')
    })

    it('should show appropriate arrow for zero change', () => {
      // Act
      const { container } = render(<BalanceOverview summary={mockSummaryWithZeroChange} />)

      // Assert
      const changeElement = container.querySelector('.text-green-600')
      expect(changeElement).toHaveTextContent('↑')
    })
  })

  describe('Missing Comparison', () => {
    it('should show "No historical data for comparison" when period_comparison is null', () => {
      // Act
      render(<BalanceOverview summary={mockSummaryWithoutComparison} />)

      // Assert
      expect(screen.getByText('No historical data for comparison')).toBeInTheDocument()
    })

    it('should show "No historical data for comparison" when period_comparison is undefined', () => {
      // Arrange
      const summaryWithUndefinedComparison: BalanceSummary = {
        ...mockSummaryWithoutComparison,
        period_comparison: undefined,
      }

      // Act
      render(<BalanceOverview summary={summaryWithUndefinedComparison} />)

      // Assert
      expect(screen.getByText('No historical data for comparison')).toBeInTheDocument()
    })

    it('should not show change arrow when no comparison data', () => {
      // Act
      const { container } = render(<BalanceOverview summary={mockSummaryWithoutComparison} />)

      // Assert
      expect(container.querySelector('.text-green-600')).not.toBeInTheDocument()
      expect(container.querySelector('.text-red-600')).not.toBeInTheDocument()
    })

    it('should not show percentage when no comparison data', () => {
      // Act
      render(<BalanceOverview summary={mockSummaryWithoutComparison} />)

      // Assert
      expect(screen.queryByText(/\(\d+\.\d+%\)/)).not.toBeInTheDocument()
    })
  })

  describe('Date Formatting', () => {
    it('should parse ISO date string correctly', () => {
      // Act
      render(<BalanceOverview summary={mockSummaryWithPositiveChange} />)

      // Assert - Date should be formatted as "Nov 15, 2025"
      expect(screen.getByText(/Nov 15, 2025/)).toBeInTheDocument()
    })

    it('should format date in human-readable format', () => {
      // Arrange
      const summaryWithDifferentDate: BalanceSummary = {
        ...mockSummaryWithPositiveChange,
        as_of_date: '2025-01-01',
      }

      // Act
      render(<BalanceOverview summary={summaryWithDifferentDate} />)

      // Assert
      expect(screen.getByText(/Jan 1, 2025/)).toBeInTheDocument()
    })
  })

  describe('Currency Formatting', () => {
    it('should format large balances with commas', () => {
      // Arrange
      const largeBalance: BalanceSummary = {
        ...mockSummaryWithPositiveChange,
        total_balance: 1000000.50,
      }

      // Act
      render(<BalanceOverview summary={largeBalance} />)

      // Assert
      expect(screen.getByText('$1,000,000.50')).toBeInTheDocument()
    })

    it('should format negative balances correctly', () => {
      // Arrange
      const negativeBalance: BalanceSummary = {
        ...mockSummaryWithPositiveChange,
        total_balance: -5000.00,
      }

      // Act
      render(<BalanceOverview summary={negativeBalance} />)

      // Assert
      expect(screen.getByText('-$5,000.00')).toBeInTheDocument()
    })

    it('should include USD currency symbol', () => {
      // Act
      render(<BalanceOverview summary={mockSummaryWithPositiveChange} />)

      // Assert
      expect(screen.getByText('$15,000.50')).toBeInTheDocument()
    })

    it('should handle decimal amounts', () => {
      // Arrange
      const decimalBalance: BalanceSummary = {
        ...mockSummaryWithPositiveChange,
        total_balance: 1234.56,
      }

      // Act
      render(<BalanceOverview summary={decimalBalance} />)

      // Assert
      expect(screen.getByText('$1,234.56')).toBeInTheDocument()
    })
  })

  describe('Edge Cases', () => {
    it('should handle very large percentage changes', () => {
      // Arrange
      const largePercentageChange: BalanceSummary = {
        ...mockSummaryWithPositiveChange,
        period_comparison: {
          previous_balance: 1000.00,
          change_amount: 9000.00,
          change_percent: 900.00,
        },
      }

      // Act
      render(<BalanceOverview summary={largePercentageChange} />)

      // Assert
      expect(screen.getByText('(900.00%)')).toBeInTheDocument()
    })

    it('should handle very small percentage changes', () => {
      // Arrange
      const smallPercentageChange: BalanceSummary = {
        ...mockSummaryWithPositiveChange,
        period_comparison: {
          previous_balance: 10000.00,
          change_amount: 1.00,
          change_percent: 0.01,
        },
      }

      // Act
      render(<BalanceOverview summary={smallPercentageChange} />)

      // Assert
      expect(screen.getByText('(0.01%)')).toBeInTheDocument()
    })

    it('should handle negative total balance', () => {
      // Arrange
      const negativeTotal: BalanceSummary = {
        ...mockSummaryWithPositiveChange,
        total_balance: -10000.00,
      }

      // Act
      render(<BalanceOverview summary={negativeTotal} />)

      // Assert
      expect(screen.getByText('-$10,000.00')).toBeInTheDocument()
    })
  })

  /**
   * Widget Styling Compliance Tests - Issue #422
   *
   * BalanceOverview is rendered inside WidgetWrapper which already provides:
   * - bg-white dark:bg-gray-800 (background)
   * - rounded-lg (rounded corners)
   * - shadow (box shadow)
   * - p-4 (padding via content div)
   *
   * The component should NOT have its own card styling to avoid double-cards.
   */
  describe('Widget Styling Compliance - Issue #422', () => {
    it('should NOT have redundant bg-white card styling', () => {
      // WidgetWrapper provides bg-white, so component should not
      const { container } = render(<BalanceOverview summary={mockSummaryWithPositiveChange} />)

      const outerDiv = container.firstChild as HTMLElement
      expect(outerDiv).not.toHaveClass('bg-white')
    })

    it('should NOT have redundant rounded-lg styling', () => {
      // WidgetWrapper provides rounded corners, so component should not
      const { container } = render(<BalanceOverview summary={mockSummaryWithPositiveChange} />)

      const outerDiv = container.firstChild as HTMLElement
      expect(outerDiv).not.toHaveClass('rounded-lg')
    })

    it('should NOT have redundant shadow styling', () => {
      // WidgetWrapper provides shadow, so component should not
      const { container } = render(<BalanceOverview summary={mockSummaryWithPositiveChange} />)

      const outerDiv = container.firstChild as HTMLElement
      expect(outerDiv).not.toHaveClass('shadow')
    })

    it('should NOT have redundant p-6 padding', () => {
      // WidgetWrapper provides p-4 padding, so component should not add more
      const { container } = render(<BalanceOverview summary={mockSummaryWithPositiveChange} />)

      const outerDiv = container.firstChild as HTMLElement
      expect(outerDiv).not.toHaveClass('p-6')
    })

    it('should have h-full for proper height in widget grid', () => {
      const { container } = render(<BalanceOverview summary={mockSummaryWithPositiveChange} />)

      const outerDiv = container.firstChild as HTMLElement
      expect(outerDiv).toHaveClass('h-full')
    })

    it('should use flex layout for content distribution', () => {
      const { container } = render(<BalanceOverview summary={mockSummaryWithPositiveChange} />)

      const outerDiv = container.firstChild as HTMLElement
      expect(outerDiv).toHaveClass('flex')
      expect(outerDiv).toHaveClass('flex-col')
    })
  })

  /**
   * Dark Mode Support Tests - Issue #422
   *
   * Component must support dark mode since it's used in dashboard widgets.
   */
  describe('Dark Mode Support - Issue #422', () => {
    it('should have dark mode class on Total Balance heading', () => {
      render(<BalanceOverview summary={mockSummaryWithPositiveChange} />)

      const heading = screen.getByText('Total Balance')
      expect(heading).toHaveClass('dark:text-gray-400')
    })

    it('should have dark mode class on balance amount', () => {
      render(<BalanceOverview summary={mockSummaryWithPositiveChange} />)

      // Balance is the large text element
      const balanceElement = screen.getByText('$15,000.50')
      expect(balanceElement).toHaveClass('dark:text-white')
    })

    it('should have dark mode class on date text', () => {
      render(<BalanceOverview summary={mockSummaryWithPositiveChange} />)

      const dateText = screen.getByText(/as of Nov 15, 2025/)
      expect(dateText).toHaveClass('dark:text-gray-500')
    })
  })
})
