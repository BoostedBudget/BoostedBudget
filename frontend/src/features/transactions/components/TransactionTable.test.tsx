/**
 * TransactionTable Component Tests
 * Issue #584: Unified Card/Table View System
 *
 * Tests for the transaction table display component.
 * TDD RED: Writing tests before implementation.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { TransactionTable } from './TransactionTable'
import type { Transaction } from '../types'
import type { Account } from '../../accounts/types'

const mockAccounts: Record<string, Account> = {
  'acc-1': {
    id: 'acc-1',
    user_id: 'user-1',
    linked_item_id: 'item-1',
    name: 'Primary Checking',
    nickname: 'My Checking',
    official_name: 'Chase Checking',
    type: 'depository',
    subtype: 'checking',
    mask: '1234',
    current_balance: 1000,
    available_balance: 950,
    currency_code: 'USD',
    plaid_account_id: 'plaid-acc-1',
    institution_name: 'Chase',
    custom_color: '#3B82F6',
    custom_emoji: '💰',
    is_hidden: false,
    status: 'active',
    display_order: 0,
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
  },
  'acc-2': {
    id: 'acc-2',
    user_id: 'user-1',
    linked_item_id: 'item-1',
    name: 'Savings',
    type: 'depository',
    subtype: 'savings',
    mask: '5678',
    current_balance: 5000,
    currency_code: 'USD',
    plaid_account_id: 'plaid-acc-2',
    institution_name: 'Chase',
    is_hidden: false,
    status: 'active',
    display_order: 1,
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
  },
}

const mockTransactions: Transaction[] = [
  {
    id: 'txn-1',
    user_id: 'user-1',
    account_id: 'acc-1',
    linked_item_id: 'item-1',
    source: 'plaid',
    amount: -45.99,
    date: '2025-01-15',
    authorized_date: '2025-01-14',
    name: 'AMAZON.COM',
    merchant_name: 'Amazon',
    category: '["Shopping", "Online"]',
    pending: false,
    tags: ['subscription', 'household'],
    created_at: '2025-01-15T00:00:00Z',
    updated_at: '2025-01-15T00:00:00Z',
  },
  {
    id: 'txn-2',
    user_id: 'user-1',
    account_id: 'acc-2',
    linked_item_id: 'item-1',
    source: 'plaid',
    amount: 2500.0,
    date: '2025-01-10',
    name: 'DIRECT DEPOSIT - PAYROLL',
    merchant_name: null,
    category: '["Income", "Salary"]',
    pending: false,
    created_at: '2025-01-10T00:00:00Z',
    updated_at: '2025-01-10T00:00:00Z',
  },
  {
    id: 'txn-3',
    user_id: 'user-1',
    account_id: 'acc-1',
    linked_item_id: 'item-1',
    source: 'plaid',
    amount: -12.5,
    date: '2025-01-12',
    name: 'UBER EATS',
    merchant_name: 'Uber Eats',
    category: '["Food and Drink", "Restaurants"]',
    pending: true,
    created_at: '2025-01-12T00:00:00Z',
    updated_at: '2025-01-12T00:00:00Z',
  },
]

describe('TransactionTable', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Rendering', () => {
    it('should render a table', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
        />
      )

      expect(screen.getByRole('table')).toBeInTheDocument()
    })

    it('should render table headers', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
        />
      )

      expect(screen.getByText('Date')).toBeInTheDocument()
      expect(screen.getByText('Description')).toBeInTheDocument()
      expect(screen.getByText('Account')).toBeInTheDocument()
      expect(screen.getByText('Category')).toBeInTheDocument()
      expect(screen.getByText('Amount')).toBeInTheDocument()
    })

    it('should render all transactions', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
        />
      )

      expect(screen.getByText('Amazon')).toBeInTheDocument()
      expect(screen.getByText('DIRECT DEPOSIT - PAYROLL')).toBeInTheDocument()
      expect(screen.getByText('Uber Eats')).toBeInTheDocument()
    })

    it('should display merchant name when available, otherwise transaction name', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
        />
      )

      // txn-1 has merchant_name "Amazon"
      expect(screen.getByText('Amazon')).toBeInTheDocument()
      // txn-2 has no merchant_name, should show transaction name
      expect(screen.getByText('DIRECT DEPOSIT - PAYROLL')).toBeInTheDocument()
    })

    it('should display account information from accountsMap', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
        />
      )

      // Should use nickname if available (appears twice for acc-1)
      const checkingBadges = screen.getAllByText('My Checking')
      expect(checkingBadges.length).toBeGreaterThan(0)
      // Should use name if nickname not available
      expect(screen.getByText('Savings')).toBeInTheDocument()
    })

    it('should display formatted amounts with proper signs', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
        />
      )

      // Expense should have minus sign
      expect(screen.getByText('-$45.99')).toBeInTheDocument()
      // Income should have plus sign
      expect(screen.getByText('+$2,500.00')).toBeInTheDocument()
    })

    it('should display category labels', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
        />
      )

      expect(screen.getByText('Shopping')).toBeInTheDocument()
      expect(screen.getByText('Income')).toBeInTheDocument()
      expect(screen.getByText('Food and Drink')).toBeInTheDocument()
    })

    it('should display pending badge for pending transactions', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
        />
      )

      const pendingBadges = screen.getAllByText('Pending')
      expect(pendingBadges.length).toBe(1)
    })

    it('should display tags when present', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
        />
      )

      expect(screen.getByText('subscription')).toBeInTheDocument()
      expect(screen.getByText('household')).toBeInTheDocument()
    })

    it('should show empty state when no transactions', () => {
      render(<TransactionTable transactions={[]} accountsMap={mockAccounts} />)

      expect(screen.getByText(/no transactions/i)).toBeInTheDocument()
    })

    it('should apply loading class when loading', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
          loading
        />
      )

      expect(screen.getByRole('table')).toHaveClass('transaction-table--loading')
    })
  })

  describe('Date Formatting', () => {
    it('should use authorized_date when available', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
        />
      )

      // txn-1 has authorized_date '2025-01-14'
      expect(screen.getByText('Jan 14, 2025')).toBeInTheDocument()
    })

    it('should fall back to date when authorized_date not available', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
        />
      )

      // txn-2 has no authorized_date, should use date '2025-01-10'
      expect(screen.getByText('Jan 10, 2025')).toBeInTheDocument()
    })
  })

  describe('Amount Styling', () => {
    it('should apply credit class to positive amounts', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
        />
      )

      const incomeAmount = screen.getByText('+$2,500.00')
      expect(incomeAmount).toHaveClass('transaction-table__amount--credit')
    })

    it('should apply debit class to negative amounts', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
        />
      )

      const expenseAmount = screen.getByText('-$45.99')
      expect(expenseAmount).toHaveClass('transaction-table__amount--debit')
    })
  })

  describe('Row Actions', () => {
    it('should call onEdit when edit button is clicked', () => {
      const onEdit = vi.fn()
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
          onEdit={onEdit}
        />
      )

      const editButtons = screen.getAllByRole('button', { name: /edit/i })
      fireEvent.click(editButtons[0])

      expect(onEdit).toHaveBeenCalledWith(mockTransactions[0])
    })

    it('should call onCategorize when categorize button is clicked', () => {
      const onCategorize = vi.fn()
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
          onCategorize={onCategorize}
        />
      )

      const categorizeButtons = screen.getAllByRole('button', { name: /categorize/i })
      fireEvent.click(categorizeButtons[0])

      expect(onCategorize).toHaveBeenCalledWith(mockTransactions[0])
    })

    it('should not show actions column when no handlers provided', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
        />
      )

      expect(screen.queryByText('Actions')).not.toBeInTheDocument()
    })

    it('should show actions column when handlers provided', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
          onEdit={() => {}}
        />
      )

      expect(screen.getByText('Actions')).toBeInTheDocument()
    })

    it('should disable actions when loading', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
          onEdit={() => {}}
          loading
        />
      )

      const editButtons = screen.getAllByRole('button', { name: /edit/i })
      editButtons.forEach((btn) => expect(btn).toBeDisabled())
    })
  })

  describe('Row Click', () => {
    it('should call onRowClick when row is clicked', () => {
      const onRowClick = vi.fn()
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
          onRowClick={onRowClick}
        />
      )

      const rows = screen.getAllByRole('row')
      // Skip header row
      fireEvent.click(rows[1])

      expect(onRowClick).toHaveBeenCalledWith(mockTransactions[0])
    })

    it('should make rows clickable when onRowClick provided', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
          onRowClick={() => {}}
        />
      )

      const rows = screen.getAllByRole('row')
      expect(rows[1]).toHaveClass('transaction-table__row--clickable')
    })
  })

  describe('Sorting', () => {
    it('should make date column sortable', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
          sortable
        />
      )

      const dateHeader = screen.getByText('Date')
      expect(dateHeader.closest('button')).toBeInTheDocument()
    })

    it('should call onSort when clicking sortable header', () => {
      const onSort = vi.fn()
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
          sortable
          onSort={onSort}
        />
      )

      const dateHeader = screen.getByText('Date').closest('button')!
      fireEvent.click(dateHeader)

      expect(onSort).toHaveBeenCalledWith('date', 'asc')
    })

    it('should toggle sort direction on second click', () => {
      const onSort = vi.fn()
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
          sortable
          onSort={onSort}
          sortColumn="date"
          sortDirection="asc"
        />
      )

      const dateHeader = screen.getByText('Date').closest('button')!
      fireEvent.click(dateHeader)

      expect(onSort).toHaveBeenCalledWith('date', 'desc')
    })

    it('should show sort indicator on sorted column', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
          sortable
          sortColumn="date"
          sortDirection="asc"
        />
      )

      const dateHeader = screen.getByText('Date').closest('button')!
      expect(dateHeader).toHaveAttribute('aria-sort', 'ascending')
    })
  })

  describe('Accessibility', () => {
    it('should have accessible table structure', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
        />
      )

      expect(screen.getByRole('table')).toBeInTheDocument()
      // Table has two rowgroups: thead and tbody
      const rowgroups = screen.getAllByRole('rowgroup')
      expect(rowgroups.length).toBe(2)
      expect(screen.getAllByRole('row').length).toBe(4) // 1 header + 3 data rows
    })

    it('should have accessible column headers', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
        />
      )

      const headers = screen.getAllByRole('columnheader')
      expect(headers.length).toBeGreaterThan(0)
    })

    it('should have aria-label on action buttons', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
          onEdit={() => {}}
          onCategorize={() => {}}
        />
      )

      const editButtons = screen.getAllByRole('button', { name: /edit transaction/i })
      const categorizeButtons = screen.getAllByRole('button', { name: /categorize transaction/i })

      expect(editButtons.length).toBe(3)
      expect(categorizeButtons.length).toBe(3)
    })
  })

  describe('Action Button Tooltips', () => {
    it('should have tooltip on edit button', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
          onEdit={vi.fn()}
        />
      )

      const editButtons = screen.getAllByRole('button', { name: /edit transaction/i })
      editButtons.forEach((btn) => {
        expect(btn).toHaveAttribute('title', 'Edit transaction')
      })
    })

    it('should have tooltip on categorize button', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
          onCategorize={vi.fn()}
        />
      )

      const categorizeButtons = screen.getAllByRole('button', { name: /categorize transaction/i })
      categorizeButtons.forEach((btn) => {
        expect(btn).toHaveAttribute('title', 'Categorize transaction')
      })
    })
  })

  // Issue #590: Inline Column Filtering Tests
  describe('Inline Filtering', () => {
    const accountOptions = [
      { value: 'acc-1', label: 'My Checking' },
      { value: 'acc-2', label: 'Savings' },
    ]

    const categoryOptions = [
      { value: 'Shopping', label: 'Shopping' },
      { value: 'Income', label: 'Income' },
      { value: 'Food and Drink', label: 'Food and Drink' },
    ]

    it('should render account filter when accountOptions provided', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
          accountOptions={accountOptions}
          onAccountFilterChange={() => {}}
        />
      )

      // Should have a filter button in the Account column header
      const filterButtons = screen.getAllByRole('button', { name: /filter by account/i })
      expect(filterButtons.length).toBeGreaterThan(0)
    })

    it('should render category filter when categoryOptions provided', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
          categoryOptions={categoryOptions}
          onCategoryFilterChange={() => {}}
        />
      )

      const filterButtons = screen.getAllByRole('button', { name: /filter by category/i })
      expect(filterButtons.length).toBeGreaterThan(0)
    })

    it('should render amount range filter when onAmountRangeChange provided', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
          onAmountRangeChange={() => {}}
        />
      )

      const filterButtons = screen.getAllByRole('button', { name: /filter by amount/i })
      expect(filterButtons.length).toBeGreaterThan(0)
    })

    it('should call onAccountFilterChange when account filter changed', async () => {
      const onAccountFilterChange = vi.fn()
      const userEvent = await import('@testing-library/user-event')
      const user = userEvent.default.setup()

      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
          accountOptions={accountOptions}
          onAccountFilterChange={onAccountFilterChange}
        />
      )

      const filterButton = screen.getByRole('button', { name: /filter by account/i })
      await user.click(filterButton)

      // For multi-select dropdowns, use checkboxes
      const checkbox = screen.getByRole('checkbox', { name: /my checking/i })
      await user.click(checkbox)

      expect(onAccountFilterChange).toHaveBeenCalledWith(['acc-1'])
    })

    it('should call onCategoryFilterChange when category filter changed', async () => {
      const onCategoryFilterChange = vi.fn()
      const userEvent = await import('@testing-library/user-event')
      const user = userEvent.default.setup()

      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
          categoryOptions={categoryOptions}
          onCategoryFilterChange={onCategoryFilterChange}
        />
      )

      const filterButton = screen.getByRole('button', { name: /filter by category/i })
      await user.click(filterButton)

      // For multi-select dropdowns, use checkboxes
      const checkbox = screen.getByRole('checkbox', { name: /shopping/i })
      await user.click(checkbox)

      expect(onCategoryFilterChange).toHaveBeenCalledWith(['Shopping'])
    })

    it('should show active filter indicator when account filter has value', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
          accountOptions={accountOptions}
          accountFilter={['acc-1']}
          onAccountFilterChange={() => {}}
        />
      )

      const filterButton = screen.getByRole('button', { name: /filter by account/i })
      expect(filterButton).toHaveClass('column-filter__trigger--active')
    })

    it('should show active filter indicator when category filter has value', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
          categoryOptions={categoryOptions}
          categoryFilter={['Shopping']}
          onCategoryFilterChange={() => {}}
        />
      )

      const filterButton = screen.getByRole('button', { name: /filter by category/i })
      expect(filterButton).toHaveClass('column-filter__trigger--active')
    })

    it('should not show filter icons when filter props not provided', () => {
      render(
        <TransactionTable
          transactions={mockTransactions}
          accountsMap={mockAccounts}
        />
      )

      expect(screen.queryByRole('button', { name: /filter by account/i })).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /filter by category/i })).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /filter by amount/i })).not.toBeInTheDocument()
    })
  })
})
