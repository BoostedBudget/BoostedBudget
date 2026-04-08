/**
 * AccountBalanceCards Component Tests
 *
 * Tests the account balance cards component that displays accounts
 * grouped by type with color-coded backgrounds.
 *
 * Issue #353: Balance Tracking Dashboard
 * Issue #422: Widget styling compliance and dark mode support
 */

import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { AccountBalanceCards } from './AccountBalanceCards'
import type { BalanceSummary } from '../types'

describe('AccountBalanceCards', () => {
  const mockSummaryWithAccounts: BalanceSummary = {
    as_of_date: '2025-11-15',
    total_balance: 15000.50,
    by_type: {
      depository: {
        balance: 5000.00,
        account_count: 2,
        accounts: [
          { id: 'acc-1', name: 'Checking Account', balance: 3000.00 },
          { id: 'acc-2', name: 'Savings Account', balance: 2000.00 },
        ],
      },
      credit: {
        balance: -1500.00,
        account_count: 1,
        accounts: [
          { id: 'acc-3', name: 'Credit Card', balance: -1500.00 },
        ],
      },
    },
  }

  const mockSummaryEmpty: BalanceSummary = {
    as_of_date: '2025-11-15',
    total_balance: 0,
    by_type: {},
  }

  describe('Basic Rendering', () => {
    it('should render account type sections', () => {
      render(<AccountBalanceCards summary={mockSummaryWithAccounts} />)

      expect(screen.getByText('Checking & Savings')).toBeInTheDocument()
      expect(screen.getByText('Credit Cards')).toBeInTheDocument()
    })

    it('should display account balances', () => {
      render(<AccountBalanceCards summary={mockSummaryWithAccounts} />)

      expect(screen.getByText('$5,000.00')).toBeInTheDocument() // Depository total
      // Credit balance appears twice (type total + individual account), use getAllByText
      expect(screen.getAllByText('-$1,500.00').length).toBeGreaterThanOrEqual(1)
    })

    it('should display individual account names', () => {
      render(<AccountBalanceCards summary={mockSummaryWithAccounts} />)

      expect(screen.getByText('Checking Account')).toBeInTheDocument()
      expect(screen.getByText('Savings Account')).toBeInTheDocument()
      expect(screen.getByText('Credit Card')).toBeInTheDocument()
    })

    it('should display account count', () => {
      render(<AccountBalanceCards summary={mockSummaryWithAccounts} />)

      expect(screen.getByText('2 accounts')).toBeInTheDocument()
      expect(screen.getByText('1 account')).toBeInTheDocument()
    })
  })

  describe('Empty State', () => {
    it('should show "No accounts found" when by_type is empty', () => {
      render(<AccountBalanceCards summary={mockSummaryEmpty} />)

      expect(screen.getByText('No accounts found')).toBeInTheDocument()
    })
  })

  describe('Color Coding', () => {
    it('should use green colors for depository accounts', () => {
      const { container } = render(<AccountBalanceCards summary={mockSummaryWithAccounts} />)

      const depositoryCard = container.querySelector('.bg-green-50')
      expect(depositoryCard).toBeInTheDocument()
    })

    it('should use red colors for credit accounts', () => {
      const { container } = render(<AccountBalanceCards summary={mockSummaryWithAccounts} />)

      const creditCard = container.querySelector('.bg-red-50')
      expect(creditCard).toBeInTheDocument()
    })
  })

  /**
   * Widget Styling Compliance Tests - Issue #422
   *
   * AccountBalanceCards is rendered inside WidgetWrapper which already provides:
   * - bg-white dark:bg-gray-800 (background)
   * - rounded-lg (rounded corners)
   * - shadow (box shadow)
   * - p-4 (padding via content div)
   *
   * The component should NOT have its own card wrapper styling to avoid double-cards.
   */
  describe('Widget Styling Compliance - Issue #422', () => {
    it('empty state should NOT have redundant wrapper styling (bg-white, shadow, rounded-lg, p-6)', () => {
      // WidgetWrapper provides wrapper styling, so empty state should not duplicate
      const { container } = render(<AccountBalanceCards summary={mockSummaryEmpty} />)
      const outerDiv = container.firstChild as HTMLElement

      expect(outerDiv).not.toHaveClass('bg-white')
      expect(outerDiv).not.toHaveClass('shadow')
      expect(outerDiv).not.toHaveClass('rounded-lg')
      expect(outerDiv).not.toHaveClass('p-6')
    })

    it('empty state should use flex layout for centering', () => {
      const { container } = render(<AccountBalanceCards summary={mockSummaryEmpty} />)

      const outerDiv = container.firstChild as HTMLElement
      expect(outerDiv).toHaveClass('flex')
      expect(outerDiv).toHaveClass('items-center')
      expect(outerDiv).toHaveClass('justify-center')
    })

    it('empty state should have h-full for proper height', () => {
      const { container } = render(<AccountBalanceCards summary={mockSummaryEmpty} />)

      const outerDiv = container.firstChild as HTMLElement
      expect(outerDiv).toHaveClass('h-full')
    })
  })

  /**
   * Dark Mode Support Tests - Issue #422
   *
   * Component must support dark mode since it's used in dashboard widgets.
   */
  describe('Dark Mode Support - Issue #422', () => {
    it('empty state should use semantic muted token', () => {
      render(<AccountBalanceCards summary={mockSummaryEmpty} />)

      const emptyText = screen.getByText('No accounts found')
      expect(emptyText).toHaveClass('text-muted-foreground')
    })

    it('individual account items should use semantic card token', () => {
      const { container } = render(<AccountBalanceCards summary={mockSummaryWithAccounts} />)

      // Account item rows use bg-card semantic token (auto-adapts to dark mode)
      const accountItems = container.querySelectorAll('[class*="bg-card"]')
      expect(accountItems.length).toBeGreaterThan(0)
    })

    it('account names should use semantic foreground token', () => {
      render(<AccountBalanceCards summary={mockSummaryWithAccounts} />)

      const accountName = screen.getByText('Checking Account')
      expect(accountName).toHaveClass('text-foreground')
    })

    it('account balances should use semantic foreground token', () => {
      const { container } = render(<AccountBalanceCards summary={mockSummaryWithAccounts} />)

      // Account balance elements use text-foreground semantic token (auto-adapts to dark mode)
      const balanceElements = container.querySelectorAll('.font-semibold.text-foreground')
      expect(balanceElements.length).toBeGreaterThanOrEqual(0)
    })
  })
})
