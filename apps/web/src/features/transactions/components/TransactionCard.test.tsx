import { describe, it, expect, beforeEach, afterEach, mock, setSystemTime } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { TransactionCard } from './TransactionCard'
import type { Transaction } from '../types'
import type { Account } from '../../accounts/types'
import type { BudgetCategoryMatch } from '../../budgets/utils/transactionBudgetMatcher'
import type { BudgetCategory } from '../../budgets/types'
import { CATEGORY_COLORS, DEFAULT_CATEGORY_COLOR } from '@/lib/category-colors'

const createBudgetCategoryMatch = (
  matchReason: 'category' | 'tag' | 'both' = 'category',
  categoryOverrides: Partial<BudgetCategory> = {}
): BudgetCategoryMatch => ({
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
})

const createMockAccount = (overrides: Partial<Account> = {}): Account => ({
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
})

const renderWithRouter = (ui: React.ReactElement) =>
  render(<MemoryRouter>{ui}</MemoryRouter>)

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
      const pastTransaction: Transaction = {
        ...mockTransaction,
        date: '2025-09-15',
        authorized_date: null,
      }
      render(<TransactionCard transaction={pastTransaction} />)
      expect(screen.getByText(/Sep.*15.*2025/i)).toBeInTheDocument()
    })

    it('should show merchant name when available', () => {
      render(<TransactionCard transaction={mockTransaction} />)
      expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
    })

    it('should fallback to transaction name when merchant name missing', () => {
      const transactionWithoutMerchant: Transaction = {
        ...mockTransaction,
        merchant_name: null,
      }
      render(<TransactionCard transaction={transactionWithoutMerchant} />)
      expect(screen.getByText('Starbucks')).toBeInTheDocument()
    })
  })

  describe('Category Display', () => {
    it('should display primary category from JSON string', () => {
      const transactionWithCategories: Transaction = {
        ...mockTransaction,
        merchant_name: 'Local Restaurant',
        category: '["Food and Drink", "Restaurants"]',
      }
      render(<TransactionCard transaction={transactionWithCategories} />)
      expect(screen.getByText(/Food and Drink/i)).toBeInTheDocument()
    })

    it('should display primary category when only one exists', () => {
      const transactionWithPrimaryOnly: Transaction = {
        ...mockTransaction,
        category: '["Travel"]',
      }
      render(<TransactionCard transaction={transactionWithPrimaryOnly} />)
      expect(screen.getByText(/Travel/i)).toBeInTheDocument()
    })

    it('should display primary category even when secondary exists', () => {
      const transactionWithSecondary: Transaction = {
        ...mockTransaction,
        category: '["Shops", "Clothing"]',
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

    it('should display category_override when present', () => {
      const transactionWithOverride: Transaction = {
        ...mockTransaction,
        merchant_name: 'Local Store',
        category: '["Food and Drink", "Restaurants"]',
        category_override: '["Shopping", "Groceries"]',
      }
      render(<TransactionCard transaction={transactionWithOverride} />)
      expect(screen.getByText(/Shopping/i)).toBeInTheDocument()
      expect(screen.queryByText(/Food and Drink/i)).not.toBeInTheDocument()
    })

    it('should fall back to category when override is null', () => {
      const transactionWithoutOverride: Transaction = {
        ...mockTransaction,
        merchant_name: 'Local Restaurant',
        category: '["Food and Drink", "Restaurants"]',
        category_override: null,
      }
      render(<TransactionCard transaction={transactionWithoutOverride} />)
      expect(screen.getByText(/Food and Drink/i)).toBeInTheDocument()
    })

    it('should fall back to category when override is empty and source not manual', () => {
      const transactionWithEmptyOverride: Transaction = {
        ...mockTransaction,
        merchant_name: 'Local Restaurant',
        category: '["Food and Drink", "Restaurants"]',
        category_override: '[]',
        category_source: 'plaid',
      }
      render(<TransactionCard transaction={transactionWithEmptyOverride} />)
      expect(screen.getByText(/Food and Drink/i)).toBeInTheDocument()
    })
  })

  describe('Explicit Uncategorized Display', () => {
    it('should display Uncategorized when manually cleared', () => {
      const explicitlyUncategorized: Transaction = {
        ...mockTransaction,
        category: '["Food and Drink", "Coffee"]',
        category_override: '[]',
        category_source: 'manual',
      }
      render(<TransactionCard transaction={explicitlyUncategorized} />)
      expect(screen.getByText('Uncategorized')).toBeInTheDocument()
      expect(screen.queryByText(/Food and Drink/i)).not.toBeInTheDocument()
    })

    it('should display Uncategorized when manual override is null', () => {
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

    it('should show warning indicator for explicitly uncategorized', () => {
      const explicitlyUncategorized: Transaction = {
        ...mockTransaction,
        category: '["Food and Drink", "Coffee"]',
        category_override: '[]',
        category_source: 'manual',
      }
      const { container } = render(<TransactionCard transaction={explicitlyUncategorized} />)
      const warningIndicator = container.querySelector('.transaction-card__uncategorized-warning')
      expect(warningIndicator).toBeInTheDocument()
    })

    it('should show warning indicator for any uncategorized', () => {
      const neverCategorized: Transaction = {
        ...mockTransaction,
        category: null,
        category_override: null,
      }
      const { container } = render(<TransactionCard transaction={neverCategorized} />)
      const warningIndicator = container.querySelector('.transaction-card__uncategorized-warning')
      expect(warningIndicator).toBeInTheDocument()
    })

    it('should not show warning for categorized', () => {
      const { container } = render(<TransactionCard transaction={mockTransaction} />)
      const warningIndicator = container.querySelector('.transaction-card__uncategorized-warning')
      expect(warningIndicator).not.toBeInTheDocument()
    })

    it('should display original_category when available', () => {
      const withOriginalCategory: Transaction = {
        ...mockTransaction,
        category: '["Food and Drink", "Coffee"]',
        category_override: '[]',
        category_source: 'manual',
        original_category: '["Food and Drink", "Coffee"]',
      }
      render(<TransactionCard transaction={withOriginalCategory} />)
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

    it('should not display tags when empty', () => {
      const transactionWithEmptyTags: Transaction = {
        ...mockTransaction,
        tags: [],
      }
      render(<TransactionCard transaction={transactionWithEmptyTags} />)
      expect(screen.queryByTestId('transaction-tags')).not.toBeInTheDocument()
    })

    it('should not display tags when undefined', () => {
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

      expect(screen.getByText('Work')).toBeInTheDocument()
      expect(screen.getByText('Expense')).toBeInTheDocument()
      expect(screen.getByText('Reimbursable')).toBeInTheDocument()

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

    it('should apply debit styling to negative', () => {
      render(<TransactionCard transaction={mockTransaction} />)
      const amountElement = screen.getByText('-$25.50')
      expect(
        amountElement.classList.contains('debit') ||
        amountElement.classList.contains('negative') ||
        amountElement.classList.contains('text-red-600') ||
        amountElement.classList.contains('text-destructive')
      ).toBe(true)
    })

    it('should apply credit styling to positive', () => {
      const creditTransaction: Transaction = {
        ...mockTransaction,
        amount: 100.00,
      }
      render(<TransactionCard transaction={creditTransaction} />)
      const amountElement = screen.getByText('+$100.00')
      expect(
        amountElement.classList.contains('credit') ||
        amountElement.classList.contains('positive') ||
        amountElement.classList.contains('text-green-600') ||
        amountElement.classList.contains('text-success')
      ).toBe(true)
    })
  })

  describe('Date Formatting', () => {
    // Mock system time to Oct 29, 2025, 12:00 PM local time for consistent date testing
    beforeEach(() => {
      setSystemTime(new Date(2025, 9, 29, 12, 0, 0)) // Oct 29, 2025, 12:00 PM (month is 0-indexed)
    })

    afterEach(() => {
      setSystemTime() // Reset to real time
    })

    it('should use authorized_date when available', () => {
      const transactionWithAuthDate: Transaction = {
        ...mockTransaction,
        date: '2025-09-20',
        authorized_date: '2025-09-19',
      }
      render(<TransactionCard transaction={transactionWithAuthDate} />)
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

    it('should show today for current date', () => {
      const todayTransaction: Transaction = {
        ...mockTransaction,
        date: '2025-10-29',
      }
      render(<TransactionCard transaction={todayTransaction} />)
      expect(screen.getByText('Today')).toBeInTheDocument()
    })

    it('should show yesterday for yesterday', () => {
      const yesterdayTransaction: Transaction = {
        ...mockTransaction,
        date: '2025-10-28',
      }
      render(<TransactionCard transaction={yesterdayTransaction} />)
      expect(screen.getByText('Yesterday')).toBeInTheDocument()
    })

    describe('Date Format Edge Cases', () => {
      it('should handle RFC3339 date format', () => {
        const transactionWithRFC3339Date: Transaction = {
          ...mockTransaction,
          date: '2025-09-15T00:00:00Z',
          authorized_date: null,
        }
        render(<TransactionCard transaction={transactionWithRFC3339Date} />)
        expect(screen.getByText(/Sep.*15.*2025/i)).toBeInTheDocument()
      })

      it('should handle null date gracefully', () => {
        const transactionWithNullDate: Transaction = {
          ...mockTransaction,
          date: null as unknown as string,
          authorized_date: null,
        }
        render(<TransactionCard transaction={transactionWithNullDate} />)
        expect(screen.getByText('Invalid Date')).toBeInTheDocument()
      })

      it('should handle malformed date string', () => {
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

    it('should position badge before category', () => {
      const budgetMatch = createBudgetCategoryMatch('category')
      const { container } = renderWithRouter(
        <TransactionCard transaction={mockTransaction} budgetMatch={budgetMatch} />
      )

      const meta = container.querySelector('.transaction-card__meta')
      expect(meta).toBeInTheDocument()

      const badge = meta?.querySelector('.transaction-budget-badge')
      const category = meta?.querySelector('.transaction-card__category')
      expect(badge).toBeInTheDocument()
      expect(category).toBeInTheDocument()

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

  describe('Account Badge Display', () => {
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

  describe('Category Color Dot', () => {
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

  describe('Income/Expense Arrows', () => {
    it('should display up arrow for income', () => {
      const incomeTransaction: Transaction = {
        ...mockTransaction,
        amount: 1000.00,
      }
      render(<TransactionCard transaction={incomeTransaction} />)

      expect(screen.getByTestId('income-arrow')).toBeInTheDocument()
      expect(screen.queryByTestId('expense-arrow')).not.toBeInTheDocument()
    })

    it('should display down arrow for expense', () => {
      render(<TransactionCard transaction={mockTransaction} />)

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

  describe('Enhanced Pending Badge', () => {
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

  describe('Hover Quick Actions', () => {
    it('should render quick actions container', () => {
      const onEdit = mock(() => {})

      const { container } = render(
        <TransactionCard transaction={mockTransaction} onEdit={onEdit} />
      )

      const quickActions = container.querySelector('.transaction-card__quick-actions')
      expect(quickActions).toBeInTheDocument()
    })

    it('should render edit button when onEdit is provided', () => {
      const onEdit = mock(() => {})

      render(
        <TransactionCard transaction={mockTransaction} onEdit={onEdit} />
      )

      const editButton = screen.getByRole('button', { name: /edit transaction/i })
      expect(editButton).toBeInTheDocument()
    })

    it('should render categorize button when onCategorize is provided', () => {
      const onCategorize = mock(() => {})

      render(
        <TransactionCard transaction={mockTransaction} onCategorize={onCategorize} />
      )

      const categorizeButton = screen.getByRole('button', { name: /categorize transaction/i })
      expect(categorizeButton).toBeInTheDocument()
    })

    it('should call onEdit when edit button is clicked', () => {
      const onEdit = mock(() => {})

      render(
        <TransactionCard transaction={mockTransaction} onEdit={onEdit} />
      )

      const editButton = screen.getByRole('button', { name: /edit transaction/i })
      fireEvent.click(editButton)

      expect(onEdit).toHaveBeenCalledWith(mockTransaction)
    })

    it('should call onCategorize when categorize button is clicked', () => {
      const onCategorize = mock(() => {})

      render(
        <TransactionCard transaction={mockTransaction} onCategorize={onCategorize} />
      )

      const categorizeButton = screen.getByRole('button', { name: /categorize transaction/i })
      fireEvent.click(categorizeButton)

      expect(onCategorize).toHaveBeenCalledWith(mockTransaction)
    })

    it('should call onEdit when quick action clicked', () => {
      const onCardClick = mock(() => {})
      render(
        <TransactionCard
          transaction={mockTransaction}
          onEdit={onCardClick}
        />
      )

      const editButton = screen.getByRole('button', { name: /edit transaction/i })
      fireEvent.click(editButton)

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
      const onEdit = mock(() => {})
      const onCategorize = mock(() => {})

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
