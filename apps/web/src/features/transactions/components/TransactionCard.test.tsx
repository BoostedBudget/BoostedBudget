import { describe, it, expect, beforeEach, afterEach, mock, setSystemTime } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { TransactionCard } from './TransactionCard'
import type { Transaction } from '../types'
import type { Account } from '../../accounts/types'
import type { BudgetCategoryMatch } from '../../budgets/utils/transactionBudgetMatcher'
import type { BudgetCategory } from '../../budgets/types'
import { CATEGORY_COLORS, DEFAULT_CATEGORY_COLOR } from '@/lib/category-colors'

/** Convert #RRGGBB to the rgb() form that happy-dom may normalize to. */
function hexToRgb(hex: string): string {
  const m = hex.replace(/^#/, '').match(/.{2}/g)
  if (!m || m.length < 3) return hex
  return `rgb(${parseInt(m[0], 16)}, ${parseInt(m[1], 16)}, ${parseInt(m[2], 16)})`
}

const createBudgetCategoryMatch = (
  matchReason: 'category' | 'tag' | 'both' = 'category',
  categoryOverrides: Partial<BudgetCategory> = {}
): BudgetCategoryMatch => ({
  category: {
    id: 'cat-1',
    budget_id: 'budget-1',
    category_kind: 'pfc',
    pfc_primary: 'FOOD_AND_DRINK',
    pfc_detailed: 'FOOD_AND_DRINK_RESTAURANTS',
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
    amount: 25.50, // PFCv2: positive = outflow (spending)
    date: '2025-10-25',
    name: 'Starbucks',
    merchant_name: 'Starbucks Coffee',
    pfc_primary: 'FOOD_AND_DRINK',
    pfc_detailed: 'FOOD_AND_DRINK_COFFEE',
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

    it('should render transaction amount as outflow/spending (positive = -$X under PFCv2)', () => {
      render(<TransactionCard transaction={mockTransaction} />)
      // PFCv2: positive amount = outflow, displayed as -$
      expect(screen.getByText('-$25.50')).toBeInTheDocument()
    })

    it('should render transaction amount as outflow/spending (positive = -$X under PFCv2)', () => {
      const outflowTransaction: Transaction = {
        ...mockTransaction,
        amount: 1000.00,
        name: 'Grocery Purchase',
      }

      render(<TransactionCard transaction={outflowTransaction} />)
      // PFCv2: positive amount = outflow, displayed as -$
      expect(screen.getByText('-$1,000.00')).toBeInTheDocument()
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
    it('should display detailed PFC label when both primary and detailed are set', () => {
      const transactionWithCategories: Transaction = {
        ...mockTransaction,
        merchant_name: 'Local Restaurant',
        pfc_primary: 'FOOD_AND_DRINK',
        pfc_detailed: 'FOOD_AND_DRINK_RESTAURANTS',
        // No display fields — falls back to formatPFCEnum
      }
      render(<TransactionCard transaction={transactionWithCategories} />)
      expect(screen.getByText('Food And Drink › Restaurants')).toBeInTheDocument()
    })

    it('should display primary PFC when no detailed is set', () => {
      const transactionWithPrimaryOnly: Transaction = {
        ...mockTransaction,
        pfc_primary: 'TRAVEL',
        pfc_detailed: null,
        // No display fields — falls back to formatPFCEnum
      }
      render(<TransactionCard transaction={transactionWithPrimaryOnly} />)
      expect(screen.getByText('Travel')).toBeInTheDocument()
    })

    it('should prefer detailed over primary in the rendered label', () => {
      const transactionWithSecondary: Transaction = {
        ...mockTransaction,
        pfc_primary: 'GENERAL_MERCHANDISE',
        pfc_detailed: 'GENERAL_MERCHANDISE_CLOTHING_AND_ACCESSORIES',
        // No display fields — falls back to formatPFCEnum
      }
      render(<TransactionCard transaction={transactionWithSecondary} />)
      expect(screen.getByText('General Merchandise › Clothing And Accessories')).toBeInTheDocument()
    })

    it('should handle null pfc_primary gracefully', () => {
      const transactionWithoutCategory: Transaction = {
        ...mockTransaction,
        pfc_primary: null,
        pfc_detailed: null,
      }

      render(<TransactionCard transaction={transactionWithoutCategory} />)
      expect(screen.getByText('Uncategorized')).toBeInTheDocument()
    })

    it('should display PFC override when category_override_kind is pfc', () => {
      const transactionWithOverride: Transaction = {
        ...mockTransaction,
        merchant_name: 'Local Store',
        pfc_primary: 'FOOD_AND_DRINK',
        pfc_detailed: 'FOOD_AND_DRINK_RESTAURANTS',
        category_override_kind: 'pfc',
        pfc_primary_override: 'GENERAL_MERCHANDISE',
        pfc_detailed_override: 'GENERAL_MERCHANDISE_GROCERIES',
        category_source: 'manual',
        // No display fields — falls back to formatPFCEnum
      }
      render(<TransactionCard transaction={transactionWithOverride} />)
      expect(screen.getByText('General Merchandise › Groceries')).toBeInTheDocument()
      // Original primary should not appear since override takes precedence
      expect(screen.queryByText('Food And Drink › Restaurants')).not.toBeInTheDocument()
    })

    it('should display custom category override when category_override_kind is custom', () => {
      const transactionWithCustom: Transaction = {
        ...mockTransaction,
        pfc_primary: 'FOOD_AND_DRINK',
        category_override_kind: 'custom',
        custom_category_override_id: 'custom-cat-uuid-123',
        custom_category_override_display: 'My Custom Groceries',
        category_source: 'manual',
      }
      render(<TransactionCard transaction={transactionWithCustom} />)
      expect(screen.getByText('My Custom Groceries')).toBeInTheDocument()
    })

    it('should fall back to source PFC when no override is set', () => {
      const transactionWithoutOverride: Transaction = {
        ...mockTransaction,
        merchant_name: 'Local Restaurant',
        pfc_primary: 'FOOD_AND_DRINK',
        pfc_detailed: 'FOOD_AND_DRINK_RESTAURANTS',
        category_override_kind: null,
        // No display fields — falls back to formatPFCEnum
      }
      render(<TransactionCard transaction={transactionWithoutOverride} />)
      expect(screen.getByText('Food And Drink › Restaurants')).toBeInTheDocument()
    })
  })

  describe('Explicit Uncategorized Display', () => {
    it('should display Uncategorized when pfc_primary is null and no override', () => {
      const explicitlyUncategorized: Transaction = {
        ...mockTransaction,
        pfc_primary: null,
        pfc_detailed: null,
        category_override_kind: null,
        category_source: 'manual',
      }
      render(<TransactionCard transaction={explicitlyUncategorized} />)
      expect(screen.getByText('Uncategorized')).toBeInTheDocument()
    })

    it('should show warning indicator for any uncategorized', () => {
      const neverCategorized: Transaction = {
        ...mockTransaction,
        pfc_primary: null,
        pfc_detailed: null,
        category_override_kind: null,
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
    it('should format small inflow amounts with 2 decimals (negative = +$ under PFCv2)', () => {
      const smallTransaction: Transaction = {
        ...mockTransaction,
        amount: -1.23, // PFCv2: negative = inflow
      }

      render(<TransactionCard transaction={smallTransaction} />)
      expect(screen.getByText('+$1.23')).toBeInTheDocument()
    })

    it('should format large inflow amounts with comma separators (negative = +$ under PFCv2)', () => {
      const largeTransaction: Transaction = {
        ...mockTransaction,
        amount: -123456.78, // PFCv2: negative = inflow
      }

      render(<TransactionCard transaction={largeTransaction} />)
      expect(screen.getByText('+$123,456.78')).toBeInTheDocument()
    })

    it('should handle zero amount', () => {
      const zeroTransaction: Transaction = {
        ...mockTransaction,
        amount: 0,
      }

      render(<TransactionCard transaction={zeroTransaction} />)
      expect(screen.getByText('$0.00')).toBeInTheDocument()
    })

    it('should apply inflow/success styling to negative amount (PFCv2: negative = inflow)', () => {
      const inflowTransaction: Transaction = {
        ...mockTransaction,
        amount: -25.50, // PFCv2: negative = inflow
      }
      render(<TransactionCard transaction={inflowTransaction} />)
      const amountElement = screen.getByText('+$25.50')
      expect(
        amountElement.classList.contains('text-success') ||
        amountElement.classList.contains('text-green-600')
      ).toBe(true)
    })

    it('should apply outflow/destructive styling to positive amount (PFCv2: positive = outflow)', () => {
      const outflowTransaction: Transaction = {
        ...mockTransaction,
        amount: 100.00, // PFCv2: positive = outflow/spending
      }
      render(<TransactionCard transaction={outflowTransaction} />)
      const amountElement = screen.getByText('-$100.00')
      expect(
        amountElement.classList.contains('text-destructive') ||
        amountElement.classList.contains('text-red-600')
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

    it('should handle transactions with both primary and detailed PFC', () => {
      const multiCategoryTransaction: Transaction = {
        ...mockTransaction,
        pfc_primary: 'FOOD_AND_DRINK',
        pfc_detailed: 'FOOD_AND_DRINK_FAST_FOOD',
        // No display fields — falls back to formatPFCEnum
      }

      render(<TransactionCard transaction={multiCategoryTransaction} />)
      // Effective label prefers detailed over primary
      expect(screen.getByText('Food And Drink › Fast Food')).toBeInTheDocument()
    })
  })

  describe('Budget Badge Integration', () => {
    it('should render TransactionBudgetBadge when budgetMatch prop is provided', () => {
      const budgetMatch = createBudgetCategoryMatch('category')

      renderWithRouter(
        <TransactionCard transaction={mockTransaction} budgetMatch={budgetMatch} />
      )

      // Badge should be present with the budget category name
      // The BudgetCategory in the mock has no display fields, so formatPFCEnum is used
      expect(screen.getByRole('link', { name: /Food And Drink/i })).toBeInTheDocument()
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
    it('should render category color dot when no icon URL is present', () => {
      const { container } = render(<TransactionCard transaction={mockTransaction} />)

      const colorDot = container.querySelector('.transaction-card__category-dot')
      expect(colorDot).toBeInTheDocument()
    })

    it('should render category icon img when pfc_category_icon_url is set', () => {
      const iconURL = 'https://plaid-category-icons.plaid.com/PFC_FOOD_AND_DRINK.png'
      const txWithIcon: Transaction = { ...mockTransaction, pfc_category_icon_url: iconURL }
      const { container } = render(<TransactionCard transaction={txWithIcon} />)

      const icon = container.querySelector('.transaction-card__category-icon') as HTMLImageElement | null
      expect(icon).toBeInTheDocument()
      expect(icon?.src).toBe(iconURL)
      expect(container.querySelector('.transaction-card__category-dot')).not.toBeInTheDocument()
    })

    it('should show color dot (not icon) when pfc_category_icon_url is null', () => {
      const txNoIcon: Transaction = { ...mockTransaction, pfc_category_icon_url: null }
      const { container } = render(<TransactionCard transaction={txNoIcon} />)

      expect(container.querySelector('.transaction-card__category-dot')).toBeInTheDocument()
      expect(container.querySelector('.transaction-card__category-icon')).not.toBeInTheDocument()
    })

    it('should apply correct color for known PFC primary category', () => {
      const transactionWithKnownCategory: Transaction = {
        ...mockTransaction,
        pfc_primary: 'FOOD_AND_DRINK',
        pfc_detailed: null,
      }

      const { container } = render(<TransactionCard transaction={transactionWithKnownCategory} />)

      const colorDot = container.querySelector('.transaction-card__category-dot') as HTMLElement | null
      expect(colorDot).toBeInTheDocument()
      // Check via DOM style property — toHaveStyle crashes in bun/jest-dom on failure
      const rendered = colorDot?.style.backgroundColor ?? ''
      const expected = CATEGORY_COLORS['FOOD_AND_DRINK']
      // happy-dom may normalize hex → rgb; accept either form
      const expectedRgb = hexToRgb(expected)
      expect([expected, expectedRgb]).toContain(rendered)
    })

    it('should apply fallback color for uncategorized transactions', () => {
      const uncategorizedTransaction: Transaction = {
        ...mockTransaction,
        pfc_primary: null,
        pfc_detailed: null,
      }

      const { container } = render(<TransactionCard transaction={uncategorizedTransaction} />)

      const colorDot = container.querySelector('.transaction-card__category-dot') as HTMLElement | null
      expect(colorDot).toBeInTheDocument()
      const rendered = colorDot?.style.backgroundColor ?? ''
      const expectedRgb = hexToRgb(DEFAULT_CATEGORY_COLOR)
      expect([DEFAULT_CATEGORY_COLOR, expectedRgb]).toContain(rendered)
    })
  })

  describe('Income/Expense Arrows', () => {
    it('should display expense arrow for outflow (positive amount under PFCv2)', () => {
      const outflowTransaction: Transaction = {
        ...mockTransaction,
        amount: 25.50, // PFCv2: positive = outflow/spending
      }
      render(<TransactionCard transaction={outflowTransaction} />)

      expect(screen.getByTestId('expense-arrow')).toBeInTheDocument()
      expect(screen.queryByTestId('income-arrow')).not.toBeInTheDocument()
    })

    it('should display income arrow for inflow (negative amount under PFCv2)', () => {
      const inflowTransaction: Transaction = {
        ...mockTransaction,
        amount: -25.50, // PFCv2: negative = inflow/refund
      }
      render(<TransactionCard transaction={inflowTransaction} />)

      expect(screen.getByTestId('income-arrow')).toBeInTheDocument()
      expect(screen.queryByTestId('expense-arrow')).not.toBeInTheDocument()
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

// ============================================================================
// Issue #1058: PFC fallback tests (D5)
// ============================================================================

describe('TransactionCard — PFC v2 source fields (Issue #1073)', () => {
  const baseTxn: Transaction = {
    id: 'txn-pfc',
    user_id: 'user-1',
    account_id: 'acc-1',
    linked_item_id: 'item-1',
    source: 'plaid',
    external_transaction_id: 'plaid-ext-1',
    amount: -12.45,
    date: '2024-01-15',
    name: 'KFC',
    pending: false,
    tags: [],
    created_at: '2024-01-15T00:00:00Z',
    updated_at: '2024-01-15T00:00:00Z',
    category_source: 'plaid',
  }

  it('shows humanized PFC primary when only pfc_primary is set (no display fields)', () => {
    const txn: Transaction = {
      ...baseTxn,
      pfc_primary: 'FOOD_AND_DRINK',
      pfc_confidence_level: 'VERY_HIGH',
      // No display field — formatPFCEnum fallback
    }

    render(<TransactionCard transaction={txn} />)

    expect(screen.getByText('Food And Drink')).toBeTruthy()
  })

  it('does not show the uncategorized warning when pfc_primary is set', () => {
    const txn: Transaction = {
      ...baseTxn,
      pfc_primary: 'FOOD_AND_DRINK',
    }

    render(<TransactionCard transaction={txn} />)

    const warnings = screen.queryAllByTitle('Uncategorized')
    expect(warnings).toHaveLength(0)
  })

  it('shows Uncategorized when pfc_primary is null even though source is plaid', () => {
    const txn: Transaction = {
      ...baseTxn,
      pfc_primary: null,
      category_source: 'plaid',
    }

    render(<TransactionCard transaction={txn} />)

    expect(screen.getByText('Uncategorized')).toBeTruthy()
  })
})
