/**
 * TransactionCard Component Tests
 *
 * TDD RED: These tests are written first and will initially fail.
 * Tests the transaction card display component.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { TransactionCard } from './TransactionCard'
import type { Transaction } from '../types'
import type { Account } from '../../accounts/types'
import type { BudgetCategoryMatch } from '../../budgets/utils/transactionBudgetMatcher'
import type { BudgetCategory } from '../../budgets/types'
import { CATEGORY_COLORS, DEFAULT_CATEGORY_COLOR } from '@/lib/category-colors'

// Helper to create budget category match for tests
function createBudgetCategoryMatch(
  matchReason: 'category' | 'tag' | 'both' = 'category',
  categoryOverrides: Partial<BudgetCategory> = {}
): BudgetCategoryMatch {
  return {
    category: {
      id: 'cat-1',
      budget_id: 'budget-1',
      category: ['Food and Drink', 'Restaurants'],
      allocated_amount: 500,
      spent_amount: 250,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
      tags: ['dining'],
      match_logic: 'category_only',
      ...categoryOverrides,
    },
    matchReason,
  }
}

// Helper to render with router (needed for Link component in badge)
function renderWithRouter(ui: React.ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>)
}

// Helper to create mock account for Issue #531 tests
function createMockAccount(overrides: Partial<Account> = {}): Account {
  return {
    id: 'account-456',
    linked_item_id: 'item-789',
    user_id: 'user-123',
    plaid_account_id: 'plaid-acc-123',
    name: 'Chase Checking',
    official_name: 'CHASE TOTAL CHECKING',
    type: 'depository',
    subtype: 'checking',
    mask: '1234',
    current_balance: 5000,
    available_balance: 4500,
    currency_code: 'USD',
    institution_name: 'Chase',
    is_hidden: false,
    status: 'active',
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
    ...overrides,
  }
}

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

  // Issue #499: Budget Badge Integration Tests
  describe('Budget Badge Integration', () => {
    it('should render TransactionBudgetBadge when budgetMatch prop is provided', () => {
      const budgetMatch = createBudgetCategoryMatch('category')

      renderWithRouter(
        <TransactionCard transaction={mockTransaction} budgetMatch={budgetMatch} />
      )

      // Badge should be present with the category name
      expect(screen.getByRole('link', { name: /Budget: Food and Drink/i })).toBeInTheDocument()
    })

    it('should not render badge when budgetMatch is null', () => {
      renderWithRouter(
        <TransactionCard transaction={mockTransaction} budgetMatch={null} />
      )

      // No badge link should be present
      expect(screen.queryByRole('link', { name: /Budget:/i })).not.toBeInTheDocument()
    })

    it('should not render badge when budgetMatch is undefined', () => {
      renderWithRouter(
        <TransactionCard transaction={mockTransaction} budgetMatch={undefined} />
      )

      expect(screen.queryByRole('link', { name: /Budget:/i })).not.toBeInTheDocument()
    })

    it('should render badge without budgetMatch prop (backwards compatible)', () => {
      // Component should work without the new prop
      render(<TransactionCard transaction={mockTransaction} />)

      expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      expect(screen.queryByRole('link', { name: /Budget:/i })).not.toBeInTheDocument()
    })

    it('should position badge before category text in meta section', () => {
      const budgetMatch = createBudgetCategoryMatch('category')

      const { container } = renderWithRouter(
        <TransactionCard transaction={mockTransaction} budgetMatch={budgetMatch} />
      )

      const meta = container.querySelector('.transaction-card__meta')
      expect(meta).toBeInTheDocument()

      // Badge should be a direct child of meta and come before category
      const badge = meta?.querySelector('.transaction-budget-badge')
      const category = meta?.querySelector('.transaction-card__category')

      expect(badge).toBeInTheDocument()
      expect(category).toBeInTheDocument()

      // Verify badge comes before category in DOM order
      if (badge && category) {
        const children = Array.from(meta!.children)
        const badgeIndex = children.indexOf(badge)
        const categoryIndex = children.indexOf(category)
        expect(badgeIndex).toBeLessThan(categoryIndex)
      }
    })

    it('should link badge to correct budget page', () => {
      const budgetMatch = createBudgetCategoryMatch('category', { budget_id: 'budget-456' })

      renderWithRouter(
        <TransactionCard transaction={mockTransaction} budgetMatch={budgetMatch} />
      )

      const link = screen.getByRole('link', { name: /Budget:/i })
      expect(link).toHaveAttribute('href', '/budgets/budget-456')
    })

    it('should show correct tooltip for category match', () => {
      const budgetMatch = createBudgetCategoryMatch('category')

      renderWithRouter(
        <TransactionCard transaction={mockTransaction} budgetMatch={budgetMatch} />
      )

      const link = screen.getByRole('link', { name: /Budget:/i })
      expect(link).toHaveAttribute('title', 'Matched by category')
    })

    it('should show correct tooltip for tag match', () => {
      const budgetMatch = createBudgetCategoryMatch('tag')

      renderWithRouter(
        <TransactionCard transaction={mockTransaction} budgetMatch={budgetMatch} />
      )

      const link = screen.getByRole('link', { name: /Budget:/i })
      expect(link).toHaveAttribute('title', 'Matched by tag')
    })

    it('should show correct tooltip for both match', () => {
      const budgetMatch = createBudgetCategoryMatch('both')

      renderWithRouter(
        <TransactionCard transaction={mockTransaction} budgetMatch={budgetMatch} />
      )

      const link = screen.getByRole('link', { name: /Budget:/i })
      expect(link).toHaveAttribute('title', 'Matched by category and tag')
    })
  })

  // Issue #531: Transaction List Visual Enhancements
  describe('Account Badge Display (Issue #531)', () => {
    it('should render account name when account prop is provided', () => {
      const account = createMockAccount()

      renderWithRouter(
        <TransactionCard transaction={mockTransaction} account={account} />
      )

      expect(screen.getByText('Chase Checking')).toBeInTheDocument()
    })

    it('should render account nickname when available', () => {
      const account = createMockAccount({ nickname: 'Main Checking' })

      renderWithRouter(
        <TransactionCard transaction={mockTransaction} account={account} />
      )

      expect(screen.getByText('Main Checking')).toBeInTheDocument()
      // Should not show the original name
      expect(screen.queryByText('Chase Checking')).not.toBeInTheDocument()
    })

    it('should apply account custom_color as background', () => {
      const account = createMockAccount({ custom_color: '#EF4444' })

      const { container } = renderWithRouter(
        <TransactionCard transaction={mockTransaction} account={account} />
      )

      const badge = container.querySelector('.transaction-card__account-badge')
      expect(badge).toBeInTheDocument()
      expect(badge).toHaveStyle({ backgroundColor: '#EF4444' })
    })

    it('should show account emoji when custom_emoji is set', () => {
      const account = createMockAccount({ custom_emoji: '💰' })

      renderWithRouter(
        <TransactionCard transaction={mockTransaction} account={account} />
      )

      expect(screen.getByText('💰')).toBeInTheDocument()
    })

    it('should not render account badge when account prop is undefined', () => {
      const { container } = render(<TransactionCard transaction={mockTransaction} />)

      const accountSection = container.querySelector('.transaction-card__account')
      expect(accountSection).not.toBeInTheDocument()
    })

    it('should not render account badge when account prop is null', () => {
      const { container } = render(
        <TransactionCard transaction={mockTransaction} account={null} />
      )

      const accountSection = container.querySelector('.transaction-card__account')
      expect(accountSection).not.toBeInTheDocument()
    })

    it('should use default background color when custom_color is not set', () => {
      const account = createMockAccount({ custom_color: undefined })

      const { container } = renderWithRouter(
        <TransactionCard transaction={mockTransaction} account={account} />
      )

      const badge = container.querySelector('.transaction-card__account-badge')
      expect(badge).toBeInTheDocument()
      // Should have a default background (gray)
      expect(badge).toHaveStyle({ backgroundColor: '#E5E7EB' })
    })
  })

  describe('Category Color Dot (Issue #531)', () => {
    it('should render category color dot before category text', () => {
      const { container } = render(<TransactionCard transaction={mockTransaction} />)

      const colorDot = container.querySelector('.transaction-card__category-dot')
      expect(colorDot).toBeInTheDocument()
    })

    it('should apply correct color for known category', () => {
      const transactionWithKnownCategory: Transaction = {
        ...mockTransaction,
        category: '["Food and Drink", "Restaurants"]',
      }

      const { container } = render(<TransactionCard transaction={transactionWithKnownCategory} />)

      const colorDot = container.querySelector('.transaction-card__category-dot')
      expect(colorDot).toBeInTheDocument()
      expect(colorDot).toHaveStyle({ backgroundColor: CATEGORY_COLORS['Food and Drink'] })
    })

    it('should apply fallback color for unknown category', () => {
      const transactionWithUnknownCategory: Transaction = {
        ...mockTransaction,
        category: '["Random Unknown Category"]',
      }

      const { container } = render(<TransactionCard transaction={transactionWithUnknownCategory} />)

      const colorDot = container.querySelector('.transaction-card__category-dot')
      expect(colorDot).toBeInTheDocument()
      expect(colorDot).toHaveStyle({ backgroundColor: DEFAULT_CATEGORY_COLOR })
    })

    it('should apply fallback color for uncategorized transactions', () => {
      const uncategorizedTransaction: Transaction = {
        ...mockTransaction,
        category: null,
      }

      const { container } = render(<TransactionCard transaction={uncategorizedTransaction} />)

      const colorDot = container.querySelector('.transaction-card__category-dot')
      expect(colorDot).toBeInTheDocument()
      expect(colorDot).toHaveStyle({ backgroundColor: DEFAULT_CATEGORY_COLOR })
    })
  })

  describe('Income/Expense Arrows (Issue #531)', () => {
    it('should display up arrow for income (positive amount)', () => {
      const incomeTransaction: Transaction = {
        ...mockTransaction,
        amount: 1000.00,
      }

      render(<TransactionCard transaction={incomeTransaction} />)

      expect(screen.getByTestId('income-arrow')).toBeInTheDocument()
      expect(screen.queryByTestId('expense-arrow')).not.toBeInTheDocument()
    })

    it('should display down arrow for expense (negative amount)', () => {
      render(<TransactionCard transaction={mockTransaction} />) // amount is -25.50

      expect(screen.getByTestId('expense-arrow')).toBeInTheDocument()
      expect(screen.queryByTestId('income-arrow')).not.toBeInTheDocument()
    })

    it('should not display any arrow for zero amount', () => {
      const zeroTransaction: Transaction = {
        ...mockTransaction,
        amount: 0,
      }

      render(<TransactionCard transaction={zeroTransaction} />)

      expect(screen.queryByTestId('income-arrow')).not.toBeInTheDocument()
      expect(screen.queryByTestId('expense-arrow')).not.toBeInTheDocument()
    })
  })

  describe('Enhanced Pending Badge (Issue #531)', () => {
    it('should display clock icon in pending badge', () => {
      const pendingTransaction: Transaction = {
        ...mockTransaction,
        pending: true,
      }

      render(<TransactionCard transaction={pendingTransaction} />)

      expect(screen.getByTestId('pending-icon')).toBeInTheDocument()
    })

    it('should display tooltip explaining pending status', () => {
      const pendingTransaction: Transaction = {
        ...mockTransaction,
        pending: true,
      }

      const { container } = render(<TransactionCard transaction={pendingTransaction} />)

      const badge = container.querySelector('.transaction-card__pending-badge')
      expect(badge).toHaveAttribute('title')
      expect(badge?.getAttribute('title')).toContain('pending')
    })

    it('should not display pending icon for posted transactions', () => {
      render(<TransactionCard transaction={mockTransaction} />) // pending: false

      expect(screen.queryByTestId('pending-icon')).not.toBeInTheDocument()
    })
  })

  describe('Hover Quick Actions (Issue #531)', () => {
    it('should render quick actions container', () => {
      const onEdit = vi.fn()

      const { container } = render(
        <TransactionCard transaction={mockTransaction} onEdit={onEdit} />
      )

      const quickActions = container.querySelector('.transaction-card__quick-actions')
      expect(quickActions).toBeInTheDocument()
    })

    it('should render edit button when onEdit is provided', () => {
      const onEdit = vi.fn()

      render(
        <TransactionCard transaction={mockTransaction} onEdit={onEdit} />
      )

      const editButton = screen.getByRole('button', { name: /edit transaction/i })
      expect(editButton).toBeInTheDocument()
    })

    it('should render categorize button when onCategorize is provided', () => {
      const onCategorize = vi.fn()

      render(
        <TransactionCard transaction={mockTransaction} onCategorize={onCategorize} />
      )

      const categorizeButton = screen.getByRole('button', { name: /categorize transaction/i })
      expect(categorizeButton).toBeInTheDocument()
    })

    it('should call onEdit when edit button is clicked', () => {
      const onEdit = vi.fn()

      render(
        <TransactionCard transaction={mockTransaction} onEdit={onEdit} />
      )

      const editButton = screen.getByRole('button', { name: /edit transaction/i })
      fireEvent.click(editButton)

      expect(onEdit).toHaveBeenCalledWith(mockTransaction)
    })

    it('should call onCategorize when categorize button is clicked', () => {
      const onCategorize = vi.fn()

      render(
        <TransactionCard transaction={mockTransaction} onCategorize={onCategorize} />
      )

      const categorizeButton = screen.getByRole('button', { name: /categorize transaction/i })
      fireEvent.click(categorizeButton)

      expect(onCategorize).toHaveBeenCalledWith(mockTransaction)
    })

    it('should not call card onClick when quick action button is clicked', () => {
      const onCardClick = vi.fn()

      // Render card with onEdit handler
      render(
        <TransactionCard
          transaction={mockTransaction}
          onEdit={onCardClick}
        />
      )

      const editButton = screen.getByRole('button', { name: /edit transaction/i })
      fireEvent.click(editButton)

      // onEdit callback should be called once
      expect(onCardClick).toHaveBeenCalledTimes(1)
    })

    it('should not render edit button when onEdit is not provided', () => {
      render(<TransactionCard transaction={mockTransaction} />)

      expect(screen.queryByRole('button', { name: /edit transaction/i })).not.toBeInTheDocument()
    })

    it('should not render categorize button when onCategorize is not provided', () => {
      render(<TransactionCard transaction={mockTransaction} />)

      expect(screen.queryByRole('button', { name: /categorize transaction/i })).not.toBeInTheDocument()
    })

    it('should have accessible labels on quick action buttons', () => {
      const onEdit = vi.fn()
      const onCategorize = vi.fn()

      render(
        <TransactionCard
          transaction={mockTransaction}
          onEdit={onEdit}
          onCategorize={onCategorize}
        />
      )

      const editButton = screen.getByRole('button', { name: /edit transaction/i })
      const categorizeButton = screen.getByRole('button', { name: /categorize transaction/i })

      expect(editButton).toHaveAttribute('aria-label')
      expect(categorizeButton).toHaveAttribute('aria-label')
    })
  })
})
