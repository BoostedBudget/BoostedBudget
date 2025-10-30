/**
 * TransactionCard Component Tests
 *
 * TDD RED: These tests are written first and will initially fail.
 * Tests the transaction card display component.
 */

import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { TransactionCard } from './TransactionCard'
import type { Transaction } from '../types'

describe('TransactionCard', () => {
  const mockTransaction: Transaction = {
    id: 'txn-1',
    user_id: 'user-123',
    account_id: 'account-456',
    linked_item_id: 'item-789',
    source: 'plaid',
    external_transaction_id: 'plaid-txn-abc',
    amount: -25.50,
    date: '2025-10-25',
    name: 'Starbucks',
    merchant_name: 'Starbucks Coffee',
    category: '["Food and Drink", "Coffee"]',
    pending: false,
    created_at: '2025-10-25T10:00:00Z',
    updated_at: '2025-10-25T10:00:00Z',
  }

  describe('Basic Rendering', () => {
    it('should render transaction name', () => {
      render(<TransactionCard transaction={mockTransaction} />)
      // Displays merchant_name when available, otherwise name
      expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
    })

    it('should render transaction amount as debit (negative)', () => {
      render(<TransactionCard transaction={mockTransaction} />)
      expect(screen.getByText('-$25.50')).toBeInTheDocument()
    })

    it('should render transaction amount as credit (positive)', () => {
      const creditTransaction: Transaction = {
        ...mockTransaction,
        amount: 1000.00,
        name: 'Salary Deposit',
      }

      render(<TransactionCard transaction={creditTransaction} />)
      expect(screen.getByText('+$1,000.00')).toBeInTheDocument()
    })

    it('should render transaction date in readable format', () => {
      // Use a date in the past to get formatted output instead of "Today"
      const pastTransaction: Transaction = {
        ...mockTransaction,
        date: '2025-09-15',
        authorized_date: null,
      }
      render(<TransactionCard transaction={pastTransaction} />)
      // Should show "Sep 15, 2025" or similar human-readable format
      expect(screen.getByText(/Sep.*15.*2025/i)).toBeInTheDocument()
    })

    it('should show merchant name when available', () => {
      render(<TransactionCard transaction={mockTransaction} />)
      expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
    })

    it('should fallback to transaction name when merchant name is missing', () => {
      const transactionWithoutMerchant: Transaction = {
        ...mockTransaction,
        merchant_name: null,
      }

      render(<TransactionCard transaction={transactionWithoutMerchant} />)
      expect(screen.getByText('Starbucks')).toBeInTheDocument()
    })
  })

  describe('Category Display', () => {
    it('should display parsed category from JSON string', () => {
      render(<TransactionCard transaction={mockTransaction} />)
      // Should show primary category "Food and Drink"
      expect(screen.getByText(/Food and Drink/i)).toBeInTheDocument()
    })

    it('should handle null category gracefully', () => {
      const transactionWithoutCategory: Transaction = {
        ...mockTransaction,
        category: null,
      }

      render(<TransactionCard transaction={transactionWithoutCategory} />)
      expect(screen.getByText('Uncategorized')).toBeInTheDocument()
    })

    it('should handle invalid JSON category gracefully', () => {
      const transactionWithInvalidCategory: Transaction = {
        ...mockTransaction,
        category: 'not-valid-json',
      }

      render(<TransactionCard transaction={transactionWithInvalidCategory} />)
      expect(screen.getByText('Uncategorized')).toBeInTheDocument()
    })

    it('should handle empty category array', () => {
      const transactionWithEmptyCategory: Transaction = {
        ...mockTransaction,
        category: '[]',
      }

      render(<TransactionCard transaction={transactionWithEmptyCategory} />)
      expect(screen.getByText('Uncategorized')).toBeInTheDocument()
    })
  })

  describe('Pending Status', () => {
    it('should show pending indicator for pending transactions', () => {
      const pendingTransaction: Transaction = {
        ...mockTransaction,
        pending: true,
      }

      render(<TransactionCard transaction={pendingTransaction} />)
      expect(screen.getByText('Pending')).toBeInTheDocument()
    })

    it('should not show pending indicator for completed transactions', () => {
      render(<TransactionCard transaction={mockTransaction} />)
      expect(screen.queryByText('Pending')).not.toBeInTheDocument()
    })

    it('should apply pending visual styling', () => {
      const pendingTransaction: Transaction = {
        ...mockTransaction,
        pending: true,
      }

      const { container } = render(<TransactionCard transaction={pendingTransaction} />)
      // Check for pending class or data attribute
      const card = container.firstChild as HTMLElement
      expect(
        card.classList.contains('pending') ||
        card.getAttribute('data-pending') === 'true'
      ).toBe(true)
    })
  })

  describe('Amount Formatting', () => {
    it('should format small amounts with 2 decimals', () => {
      const smallTransaction: Transaction = {
        ...mockTransaction,
        amount: -1.23,
      }

      render(<TransactionCard transaction={smallTransaction} />)
      expect(screen.getByText('-$1.23')).toBeInTheDocument()
    })

    it('should format large amounts with comma separators', () => {
      const largeTransaction: Transaction = {
        ...mockTransaction,
        amount: -123456.78,
      }

      render(<TransactionCard transaction={largeTransaction} />)
      expect(screen.getByText('-$123,456.78')).toBeInTheDocument()
    })

    it('should handle zero amount', () => {
      const zeroTransaction: Transaction = {
        ...mockTransaction,
        amount: 0,
      }

      render(<TransactionCard transaction={zeroTransaction} />)
      expect(screen.getByText('$0.00')).toBeInTheDocument()
    })

    it('should apply debit styling to negative amounts', () => {
      render(<TransactionCard transaction={mockTransaction} />)
      const amountElement = screen.getByText('-$25.50')
      expect(
        amountElement.classList.contains('debit') ||
        amountElement.classList.contains('negative') ||
        amountElement.classList.contains('text-red-600')
      ).toBe(true)
    })

    it('should apply credit styling to positive amounts', () => {
      const creditTransaction: Transaction = {
        ...mockTransaction,
        amount: 100.00,
      }

      render(<TransactionCard transaction={creditTransaction} />)
      const amountElement = screen.getByText('+$100.00')
      expect(
        amountElement.classList.contains('credit') ||
        amountElement.classList.contains('positive') ||
        amountElement.classList.contains('text-green-600')
      ).toBe(true)
    })
  })

  describe('Date Formatting', () => {
    it('should use authorized_date when available', () => {
      const transactionWithAuthDate: Transaction = {
        ...mockTransaction,
        date: '2025-09-20',
        authorized_date: '2025-09-19',
      }

      render(<TransactionCard transaction={transactionWithAuthDate} />)
      // Should show authorized date (Sep 19) instead of date (Sep 20)
      expect(screen.getByText(/Sep.*19.*2025/i)).toBeInTheDocument()
    })

    it('should fallback to date when authorized_date is null', () => {
      const transactionWithoutAuthDate: Transaction = {
        ...mockTransaction,
        date: '2025-09-15',
        authorized_date: null,
      }

      render(<TransactionCard transaction={transactionWithoutAuthDate} />)
      expect(screen.getByText(/Sep.*15.*2025/i)).toBeInTheDocument()
    })

    it('should show today for current date transactions', () => {
      // Use local date to match formatDate's local time logic
      const now = new Date()
      const today = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
      const todayTransaction: Transaction = {
        ...mockTransaction,
        date: today,
      }

      render(<TransactionCard transaction={todayTransaction} />)
      expect(screen.getByText('Today')).toBeInTheDocument()
    })

    it('should show yesterday for yesterday transactions', () => {
      // Use local date to match formatDate's local time logic
      const yesterdayDate = new Date()
      yesterdayDate.setDate(yesterdayDate.getDate() - 1)
      const yesterday = `${yesterdayDate.getFullYear()}-${String(yesterdayDate.getMonth() + 1).padStart(2, '0')}-${String(yesterdayDate.getDate()).padStart(2, '0')}`
      const yesterdayTransaction: Transaction = {
        ...mockTransaction,
        date: yesterday,
      }

      render(<TransactionCard transaction={yesterdayTransaction} />)
      expect(screen.getByText('Yesterday')).toBeInTheDocument()
    })
  })

  describe('Accessibility', () => {
    it('should have proper semantic structure', () => {
      const { container } = render(<TransactionCard transaction={mockTransaction} />)
      // Should use article or similar semantic element
      const article = container.querySelector('article')
      expect(article).toBeInTheDocument()
    })

    it('should include transaction ID in data attribute for testing', () => {
      const { container } = render(<TransactionCard transaction={mockTransaction} />)
      const card = container.firstChild as HTMLElement
      expect(card.getAttribute('data-transaction-id')).toBe('txn-1')
    })

    it('should have appropriate ARIA labels', () => {
      render(<TransactionCard transaction={mockTransaction} />)
      const card = screen.getByRole('article')
      expect(card).toHaveAttribute('aria-label')
    })
  })

  describe('Edge Cases', () => {
    it('should handle very long transaction names', () => {
      const longNameTransaction: Transaction = {
        ...mockTransaction,
        name: 'A'.repeat(200),
        merchant_name: null, // Remove merchant_name so name is displayed
      }

      render(<TransactionCard transaction={longNameTransaction} />)
      // Should truncate or handle long names gracefully
      expect(screen.getByText(/A{10,}/)).toBeInTheDocument()
    })

    it('should handle very long merchant names', () => {
      const longMerchantTransaction: Transaction = {
        ...mockTransaction,
        merchant_name: 'Very Long Merchant Name That Should Be Truncated or Wrapped Properly',
      }

      render(<TransactionCard transaction={longMerchantTransaction} />)
      expect(screen.getByText(/Very Long Merchant/)).toBeInTheDocument()
    })

    it('should handle transactions with multiple category levels', () => {
      const multiCategoryTransaction: Transaction = {
        ...mockTransaction,
        category: '["Food and Drink", "Restaurants", "Fast Food", "Coffee Shops"]',
      }

      render(<TransactionCard transaction={multiCategoryTransaction} />)
      // Should show primary category
      expect(screen.getByText(/Food and Drink/i)).toBeInTheDocument()
    })
  })
})
