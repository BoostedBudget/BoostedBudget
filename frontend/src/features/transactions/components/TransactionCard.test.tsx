/**
 * TransactionCard Component Tests
 *
 * TDD RED: These tests are written first and will initially fail.
 * Tests the transaction card display component.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
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
    it('should display primary category from JSON string for consistency', () => {
      // Use a transaction without "Food" in merchant name to avoid confusion
      const transactionWithCategories: Transaction = {
        ...mockTransaction,
        merchant_name: 'Local Restaurant',
        category: '["Food and Drink", "Restaurants"]',
      }
      render(<TransactionCard transaction={transactionWithCategories} />)
      // Should show primary category "Food and Drink" for consistency
      expect(screen.getByText(/Food and Drink/i)).toBeInTheDocument()
    })

    it('should display primary category when only one category exists', () => {
      const transactionWithPrimaryOnly: Transaction = {
        ...mockTransaction,
        category: '["Travel"]', // Only primary category
      }

      render(<TransactionCard transaction={transactionWithPrimaryOnly} />)
      expect(screen.getByText(/Travel/i)).toBeInTheDocument()
    })

    it('should display primary category even when secondary exists', () => {
      const transactionWithSecondary: Transaction = {
        ...mockTransaction,
        category: '["Shops", "Clothing"]', // Has secondary but should show primary
      }

      render(<TransactionCard transaction={transactionWithSecondary} />)
      expect(screen.getByText(/Shops/i)).toBeInTheDocument()
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

    it('should display category_override when present (priority over category)', () => {
      const transactionWithOverride: Transaction = {
        ...mockTransaction,
        merchant_name: 'Local Store',
        category: '["Food and Drink", "Restaurants"]',
        category_override: '["Shopping", "Groceries"]',
      }

      render(<TransactionCard transaction={transactionWithOverride} />)
      // Should show override primary category "Shopping", not original "Food and Drink"
      expect(screen.getByText(/Shopping/i)).toBeInTheDocument()
      expect(screen.queryByText(/Food and Drink/i)).not.toBeInTheDocument()
    })

    it('should fall back to category when category_override is null', () => {
      const transactionWithoutOverride: Transaction = {
        ...mockTransaction,
        merchant_name: 'Local Restaurant',
        category: '["Food and Drink", "Restaurants"]',
        category_override: null,
      }

      render(<TransactionCard transaction={transactionWithoutOverride} />)
      // Should show primary category "Food and Drink"
      expect(screen.getByText(/Food and Drink/i)).toBeInTheDocument()
    })

    it('should fall back to category when category_override is empty array and category_source is NOT manual', () => {
      const transactionWithEmptyOverride: Transaction = {
        ...mockTransaction,
        merchant_name: 'Local Restaurant',
        category: '["Food and Drink", "Restaurants"]',
        category_override: '[]',
        category_source: 'plaid', // Not manual, so fall back
      }

      render(<TransactionCard transaction={transactionWithEmptyOverride} />)
      // Should show primary category "Food and Drink"
      expect(screen.getByText(/Food and Drink/i)).toBeInTheDocument()
    })
  })

  describe('Explicit Uncategorized Display', () => {
    it('should display Uncategorized when category_source is manual and category_override is empty', () => {
      const explicitlyUncategorized: Transaction = {
        ...mockTransaction,
        category: '["Food and Drink", "Coffee"]',
        category_override: '[]',
        category_source: 'manual', // User explicitly cleared the category
      }

      render(<TransactionCard transaction={explicitlyUncategorized} />)
      expect(screen.getByText('Uncategorized')).toBeInTheDocument()
      // Should NOT fall back to Plaid category
      expect(screen.queryByText(/Food and Drink/i)).not.toBeInTheDocument()
    })

    it('should display Uncategorized when category_source is manual and category_override is null', () => {
      const explicitlyUncategorized: Transaction = {
        ...mockTransaction,
        category: '["Food and Drink", "Coffee"]',
        category_override: null,
        category_source: 'manual',
      }

      render(<TransactionCard transaction={explicitlyUncategorized} />)
      expect(screen.getByText('Uncategorized')).toBeInTheDocument()
      expect(screen.queryByText(/Food and Drink/i)).not.toBeInTheDocument()
    })

    it('should show warning indicator for explicitly uncategorized transactions', () => {
      const explicitlyUncategorized: Transaction = {
        ...mockTransaction,
        category: '["Food and Drink", "Coffee"]',
        category_override: '[]',
        category_source: 'manual',
      }

      const { container } = render(<TransactionCard transaction={explicitlyUncategorized} />)
      // Should have warning indicator element
      const warningIndicator = container.querySelector('.transaction-card__uncategorized-warning')
      expect(warningIndicator).toBeInTheDocument()
    })

    it('should show warning indicator for any uncategorized transaction', () => {
      const neverCategorized: Transaction = {
        ...mockTransaction,
        category: null,
        category_override: null,
      }

      const { container } = render(<TransactionCard transaction={neverCategorized} />)
      const warningIndicator = container.querySelector('.transaction-card__uncategorized-warning')
      expect(warningIndicator).toBeInTheDocument()
    })

    it('should NOT show warning indicator for categorized transactions', () => {
      const { container } = render(<TransactionCard transaction={mockTransaction} />)
      const warningIndicator = container.querySelector('.transaction-card__uncategorized-warning')
      expect(warningIndicator).not.toBeInTheDocument()
    })

    it('should display original_category when available for reset context', () => {
      const withOriginalCategory: Transaction = {
        ...mockTransaction,
        category: '["Food and Drink", "Coffee"]',
        category_override: '[]',
        category_source: 'manual',
        original_category: '["Food and Drink", "Coffee"]',
      }

      render(<TransactionCard transaction={withOriginalCategory} />)
      // Original category should be accessible (e.g., in data attribute or tooltip)
      const card = screen.getByRole('article')
      expect(card).toHaveAttribute('data-original-category', '["Food and Drink", "Coffee"]')
    })
  })

  describe('Tags Display', () => {
    it('should display tags when present', () => {
      const transactionWithTags: Transaction = {
        ...mockTransaction,
        tags: ['Groceries', 'Monthly'],
      }

      render(<TransactionCard transaction={transactionWithTags} />)
      expect(screen.getByText('Groceries')).toBeInTheDocument()
      expect(screen.getByText('Monthly')).toBeInTheDocument()
    })

    it('should not display tags section when tags array is empty', () => {
      const transactionWithEmptyTags: Transaction = {
        ...mockTransaction,
        tags: [],
      }

      render(<TransactionCard transaction={transactionWithEmptyTags} />)
      // Should not have a tags container when no tags
      expect(screen.queryByTestId('transaction-tags')).not.toBeInTheDocument()
    })

    it('should not display tags section when tags is undefined', () => {
      const transactionWithoutTags: Transaction = {
        ...mockTransaction,
        tags: undefined,
      }

      render(<TransactionCard transaction={transactionWithoutTags} />)
      expect(screen.queryByTestId('transaction-tags')).not.toBeInTheDocument()
    })

    it('should display multiple tags with correct styling', () => {
      const transactionWithMultipleTags: Transaction = {
        ...mockTransaction,
        tags: ['Work', 'Expense', 'Reimbursable'],
      }

      const { container } = render(<TransactionCard transaction={transactionWithMultipleTags} />)

      // Should have all three tags
      expect(screen.getByText('Work')).toBeInTheDocument()
      expect(screen.getByText('Expense')).toBeInTheDocument()
      expect(screen.getByText('Reimbursable')).toBeInTheDocument()

      // Should have tags container with correct class
      const tagsContainer = container.querySelector('.transaction-card__tags')
      expect(tagsContainer).toBeInTheDocument()
    })

    it('should display single tag correctly', () => {
      const transactionWithSingleTag: Transaction = {
        ...mockTransaction,
        tags: ['Important'],
      }

      render(<TransactionCard transaction={transactionWithSingleTag} />)
      expect(screen.getByText('Important')).toBeInTheDocument()
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
    // Mock system time to Oct 29, 2025, 12:00 PM local time for consistent date testing
    beforeEach(() => {
      vi.useFakeTimers()
      vi.setSystemTime(new Date(2025, 9, 29, 12, 0, 0)) // Oct 29, 2025, 12:00 PM (month is 0-indexed)
    })

    afterEach(() => {
      vi.useRealTimers()
    })

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
      // System time mocked to Oct 29, 2025
      const todayTransaction: Transaction = {
        ...mockTransaction,
        date: '2025-10-29', // Today in mocked system time
      }

      render(<TransactionCard transaction={todayTransaction} />)
      expect(screen.getByText('Today')).toBeInTheDocument()
    })

    it('should show yesterday for yesterday transactions', () => {
      // System time mocked to Oct 29, 2025, so yesterday is Oct 28
      const yesterdayTransaction: Transaction = {
        ...mockTransaction,
        date: '2025-10-28', // Yesterday in mocked system time
      }

      render(<TransactionCard transaction={yesterdayTransaction} />)
      expect(screen.getByText('Yesterday')).toBeInTheDocument()
    })

    // TDD RED: These tests will fail until formatDate() is fixed
    describe('Date Format Edge Cases', () => {
      it('should handle RFC3339 date format from backend', () => {
        // Backend returns dates as RFC3339: "2025-09-15T00:00:00Z"
        const transactionWithRFC3339Date: Transaction = {
          ...mockTransaction,
          date: '2025-09-15T00:00:00Z',
          authorized_date: null,
        }

        render(<TransactionCard transaction={transactionWithRFC3339Date} />)
        expect(screen.getByText(/Sep.*15.*2025/i)).toBeInTheDocument()
      })

      it('should handle null/undefined date gracefully', () => {
        // Edge case: both date fields are null (shouldn't happen but must not crash)
        const transactionWithNullDate: Transaction = {
          ...mockTransaction,
          date: null as unknown as string, // Type assertion to test runtime behavior
          authorized_date: null,
        }

        render(<TransactionCard transaction={transactionWithNullDate} />)
        expect(screen.getByText('Invalid Date')).toBeInTheDocument()
      })

      it('should handle malformed date string gracefully', () => {
        // Edge case: corrupted/invalid date string
        const transactionWithBadDate: Transaction = {
          ...mockTransaction,
          date: 'not-a-valid-date',
          authorized_date: null,
        }

        render(<TransactionCard transaction={transactionWithBadDate} />)
        expect(screen.getByText('Invalid Date')).toBeInTheDocument()
      })
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
      // Should show primary category for consistency
      expect(screen.getByText(/Food and Drink/i)).toBeInTheDocument()
    })
  })
})
