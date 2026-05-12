import { describe, it, expect, mock } from 'bun:test'
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
    amount: 45.99, // PFCv2: positive = outflow (spending)
    date: '2025-01-15',
    authorized_date: '2025-01-14',
    name: 'AMAZON.COM',
    merchant_name: 'Amazon',
    pfc_primary: 'SHOPPING',
    pfc_detailed: 'SHOPPING_GENERAL_MERCHANDISE',
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
    amount: -2500.0, // PFCv2: negative = inflow (payroll income)
    date: '2025-01-10',
    name: 'DIRECT DEPOSIT - PAYROLL',
    merchant_name: null,
    pfc_primary: 'INCOME',
    pfc_detailed: 'INCOME_WAGES',
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
    amount: 12.5, // PFCv2: positive = outflow (food purchase)
    date: '2025-01-12',
    name: 'UBER EATS',
    merchant_name: 'Uber Eats',
    pfc_primary: 'FOOD_AND_DRINK',
    pfc_detailed: 'FOOD_AND_DRINK_RESTAURANTS',
    pending: true,
    created_at: '2025-01-12T00:00:00Z',
    updated_at: '2025-01-12T00:00:00Z',
  },
]

describe('TransactionTable', () => {
  const renderTable = (props = {}) =>
    render(
      <TransactionTable
        transactions={mockTransactions}
        accountsMap={mockAccounts}
        {...props}
      />
    )

  describe('Rendering', () => {
    it('should render a table', () => {
      renderTable()
      expect(screen.getByRole('table')).toBeInTheDocument()
    })

    it('should render table headers', () => {
      renderTable()

      expect(screen.getByText('Date')).toBeInTheDocument()
      expect(screen.getByText('Description')).toBeInTheDocument()
      expect(screen.getByText('Account')).toBeInTheDocument()
      expect(screen.getByText('Category')).toBeInTheDocument()
      expect(screen.getByText('Amount')).toBeInTheDocument()
    })

    it('should render all transactions', () => {
      renderTable()

      expect(screen.getByText('Amazon')).toBeInTheDocument()
      expect(screen.getByText('DIRECT DEPOSIT - PAYROLL')).toBeInTheDocument()
      expect(screen.getByText('Uber Eats')).toBeInTheDocument()
    })

    it('should display merchant name when available, otherwise transaction name', () => {
      renderTable()

      expect(screen.getByText('Amazon')).toBeInTheDocument()
      expect(screen.getByText('DIRECT DEPOSIT - PAYROLL')).toBeInTheDocument()
    })

    it('should display account information from accountsMap', () => {
      renderTable()

      expect(screen.getAllByText('My Checking').length).toBeGreaterThan(0)
      expect(screen.getByText('Savings')).toBeInTheDocument()
    })

    it('should display formatted amounts with proper signs', () => {
      renderTable()

      expect(screen.getByText('-$45.99')).toBeInTheDocument()
      expect(screen.getByText('+$2,500.00')).toBeInTheDocument()
    })

    it('should display category labels', () => {
      renderTable()

      // No display fields on these test transactions — formatPFCEnum fallback is used
      expect(screen.getByText('Shopping › General Merchandise')).toBeInTheDocument()
      expect(screen.getByText('Income › Wages')).toBeInTheDocument()
      expect(screen.getByText('Food And Drink › Restaurants')).toBeInTheDocument()
    })

    it('should display pending badge for pending transactions', () => {
      renderTable()

      expect(screen.getAllByText('Pending').length).toBe(1)
    })

    it('should display tags when present', () => {
      renderTable()

      expect(screen.getByText('subscription')).toBeInTheDocument()
      expect(screen.getByText('household')).toBeInTheDocument()
    })

    it('should show empty state when no transactions', () => {
      render(<TransactionTable transactions={[]} accountsMap={mockAccounts} />)

      expect(screen.getByText(/no transactions/i)).toBeInTheDocument()
    })

    it('should apply loading class when loading', () => {
      renderTable({ loading: true })

      expect(screen.getByRole('table')).toHaveClass('transaction-table--loading')
    })
  })

  describe('Date Formatting', () => {
    it('should use authorized_date when available', () => {
      renderTable()
      expect(screen.getByText('Jan 14, 2025')).toBeInTheDocument()
    })

    it('should fall back to date when authorized_date not available', () => {
      renderTable()
      expect(screen.getByText('Jan 10, 2025')).toBeInTheDocument()
    })
  })

  describe('Amount Styling', () => {
    it('should apply debit class to outflows (positive amounts under PFCv2)', () => {
      renderTable()
      // txn-1 amount=45.99 is an outflow: displays as '-$45.99' with debit styling
      expect(screen.getByText('-$45.99')).toHaveClass('transaction-table__amount--debit')
    })

    it('should apply credit class to inflows (negative amounts under PFCv2)', () => {
      renderTable()
      // txn-2 amount=-2500.0 is an inflow: displays as '+$2,500.00' with credit styling
      expect(screen.getByText('+$2,500.00')).toHaveClass('transaction-table__amount--credit')
    })
  })

  describe('Row Actions', () => {
    it('should call onEdit when edit button is clicked', () => {
      const onEdit = mock(() => {})
      renderTable({ onEdit })

      const editButtons = screen.getAllByRole('button', { name: /edit/i })
      fireEvent.click(editButtons[0])

      expect(onEdit).toHaveBeenCalledWith(mockTransactions[0])
    })

    it('should call onCategorize when categorize button is clicked', () => {
      const onCategorize = mock(() => {})
      renderTable({ onCategorize })

      const categorizeButtons = screen.getAllByRole('button', { name: /categorize/i })
      fireEvent.click(categorizeButtons[0])

      expect(onCategorize).toHaveBeenCalledWith(mockTransactions[0])
    })

    it('should not show actions column when no handlers provided', () => {
      renderTable()
      expect(screen.queryByText('Actions')).not.toBeInTheDocument()
    })

    it('should show actions column when handlers provided', () => {
      renderTable({ onEdit: () => {} })
      expect(screen.getByText('Actions')).toBeInTheDocument()
    })

    it('should disable actions when loading', () => {
      renderTable({ onEdit: () => {}, loading: true })

      const editButtons = screen.getAllByRole('button', { name: /edit/i })
      editButtons.forEach((btn) => expect(btn).toBeDisabled())
    })
  })

  describe('Row Click', () => {
    it('should call onRowClick when row is clicked', () => {
      const onRowClick = mock(() => {})
      renderTable({ onRowClick })

      const rows = screen.getAllByRole('row')
      fireEvent.click(rows[1])

      expect(onRowClick).toHaveBeenCalledWith(mockTransactions[0])
    })

    it('should make rows clickable when onRowClick provided', () => {
      renderTable({ onRowClick: () => {} })

      const rows = screen.getAllByRole('row')
      expect(rows[1]).toHaveClass('transaction-table__row--clickable')
    })
  })

  describe('Sorting', () => {
    it('should make date column sortable', () => {
      renderTable({ sortable: true })

      const dateHeader = screen.getByText('Date')
      expect(dateHeader.closest('button')).toBeInTheDocument()
    })

    it('should call onSort when clicking sortable header', () => {
      const onSort = mock(() => {})
      renderTable({ sortable: true, onSort })

      const dateHeader = screen.getByText('Date').closest('button')!
      fireEvent.click(dateHeader)

      expect(onSort).toHaveBeenCalledWith('date', 'asc')
    })

    it('should toggle sort direction on second click', () => {
      const onSort = mock(() => {})
      renderTable({
        sortable: true,
        onSort,
        sortColumn: 'date',
        sortDirection: 'asc',
      })

      const dateHeader = screen.getByText('Date').closest('button')!
      fireEvent.click(dateHeader)

      expect(onSort).toHaveBeenCalledWith('date', 'desc')
    })

    it('should show sort indicator on sorted column', () => {
      renderTable({
        sortable: true,
        sortColumn: 'date',
        sortDirection: 'asc',
      })

      // aria-sort lives on <th> (correct HTML semantics), not the sort button
      const dateTh = screen.getByRole('columnheader', { name: /date/i })
      expect(dateTh).toHaveAttribute('aria-sort', 'ascending')
    })
  })

  describe('Accessibility', () => {
    it('should have accessible table structure', () => {
      renderTable()

      expect(screen.getByRole('table')).toBeInTheDocument()
      expect(screen.getAllByRole('rowgroup').length).toBe(2)
      expect(screen.getAllByRole('row').length).toBe(4)
    })

    it('should have accessible column headers', () => {
      renderTable()

      const headers = screen.getAllByRole('columnheader')
      expect(headers.length).toBeGreaterThan(0)
    })

    it('should have aria-label on action buttons', () => {
      renderTable({ onEdit: () => {}, onCategorize: () => {} })

      const editButtons = screen.getAllByRole('button', { name: /edit transaction/i })
      const categorizeButtons = screen.getAllByRole('button', { name: /categorize transaction/i })

      expect(editButtons.length).toBe(3)
      expect(categorizeButtons.length).toBe(3)
    })
  })

  describe('Action Button Tooltips', () => {
    it('should have tooltip on edit button', () => {
      renderTable({ onEdit: mock(() => {}) })

      screen.getAllByRole('button', { name: /edit transaction/i }).forEach((btn) => {
        expect(btn).toHaveAttribute('title', 'Edit transaction')
      })
    })

    it('should have tooltip on categorize button', () => {
      renderTable({ onCategorize: mock(() => {}) })

      screen.getAllByRole('button', { name: /categorize transaction/i }).forEach((btn) => {
        expect(btn).toHaveAttribute('title', 'Categorize transaction')
      })
    })
  })

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
      renderTable({ accountOptions, onAccountFilterChange: () => {} })

      const filterButtons = screen.getAllByRole('button', { name: /filter by account/i })
      expect(filterButtons.length).toBeGreaterThan(0)
    })

    it('should render category filter when categoryOptions provided', () => {
      renderTable({ categoryOptions, onCategoryFilterChange: () => {} })

      const filterButtons = screen.getAllByRole('button', { name: /filter by category/i })
      expect(filterButtons.length).toBeGreaterThan(0)
    })

    it('should render amount range filter when onAmountRangeChange provided', () => {
      renderTable({ onAmountRangeChange: () => {} })

      const filterButtons = screen.getAllByRole('button', { name: /filter by amount/i })
      expect(filterButtons.length).toBeGreaterThan(0)
    })

    it('should call onAccountFilterChange when account filter changed', async () => {
      const onAccountFilterChange = mock(() => {})
      const userEvent = await import('@testing-library/user-event')
      const user = userEvent.default.setup()

      renderTable({
        accountOptions,
        onAccountFilterChange,
      })

      const filterButton = screen.getByRole('button', { name: /filter by account/i })
      await user.click(filterButton)

      const checkbox = screen.getByRole('checkbox', { name: /my checking/i })
      await user.click(checkbox)

      expect(onAccountFilterChange).toHaveBeenCalledWith(['acc-1'])
    })

    it('should call onCategoryFilterChange when category filter changed', async () => {
      const onCategoryFilterChange = mock(() => {})
      const userEvent = await import('@testing-library/user-event')
      const user = userEvent.default.setup()

      renderTable({
        categoryOptions,
        onCategoryFilterChange,
      })

      const filterButton = screen.getByRole('button', { name: /filter by category/i })
      await user.click(filterButton)

      const checkbox = screen.getByRole('checkbox', { name: /shopping/i })
      await user.click(checkbox)

      expect(onCategoryFilterChange).toHaveBeenCalledWith(['Shopping'])
    })

    it('should show active filter indicator when account filter has value', () => {
      renderTable({
        accountOptions,
        accountFilter: ['acc-1'],
        onAccountFilterChange: () => {},
      })

      const filterButton = screen.getByRole('button', { name: /filter by account/i })
      expect(filterButton).toHaveClass('column-filter__trigger--active')
    })

    it('should show active filter indicator when category filter has value', () => {
      renderTable({
        categoryOptions,
        categoryFilter: ['Shopping'],
        onCategoryFilterChange: () => {},
      })

      const filterButton = screen.getByRole('button', { name: /filter by category/i })
      expect(filterButton).toHaveClass('column-filter__trigger--active')
    })

    it('should not show filter icons when filter props not provided', () => {
      renderTable()

      expect(screen.queryByRole('button', { name: /filter by account/i })).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /filter by category/i })).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /filter by amount/i })).not.toBeInTheDocument()
    })
  })
})
