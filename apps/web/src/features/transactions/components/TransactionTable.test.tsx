import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { TransactionTable } from './TransactionTable'
import type { Transaction, BankingTransaction, InvestmentActivity, FeedRow } from '../types'
import type { Account } from '../../accounts/types'

// NOTE: Do NOT mock HierarchicalCategoryTreeSelect with mock.module() here.
// bun:test mock.module() is process-global — it would leak into HierarchicalCategoryTreeSelect.test.tsx
// and break those tests. The real component renders fine in tests; useCuratedPfcDetailed()
// returns a static constant and Radix Popover works in jsdom.

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
    exclude_investment_sync: false,
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
    exclude_investment_sync: false,
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
  // T2: TransactionTable.transactions is now FeedRow[]; mock fixtures use Transaction (source: 'plaid').
  // Single cast at the test boundary — real production data flows through the toFeedRow mapper.
  const renderTable = (props: Partial<Parameters<typeof TransactionTable>[0]> = {}) =>
    render(
      <TransactionTable
        transactions={mockTransactions as unknown as FeedRow[]}
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

      // Category cells show the hierarchy root; details have their own column.
      expect(screen.getByText('Shopping')).toBeInTheDocument()
      expect(screen.getByText('Income')).toBeInTheDocument()
      expect(screen.getByText('Food & Drink')).toBeInTheDocument()
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

  describe('PII Masking', () => {
    it('should mask amount, merchant, account and category hierarchy cells', () => {
      const { container } = renderTable()
      // Five potentially sensitive columns × three rows.
      const maskedTds = container.querySelectorAll('td[data-ph-mask]')
      expect(maskedTds.length).toBe(15)
    })

    it('leaves dates unmasked and masks category columns that can contain custom names', () => {
      const { container } = renderTable()
      // Custom category names are user-entered free text.
      const ths = Array.from(container.querySelectorAll('th'))
      const dateColIdx = ths.findIndex((th) => th.textContent?.includes('Date'))
      const categoryColIdx = ths.findIndex((th) => th.textContent?.includes('Category'))
      const rows = Array.from(container.querySelectorAll('tbody tr'))
      rows.forEach((row) => {
        const tds = Array.from(row.querySelectorAll('td'))
        if (dateColIdx >= 0) expect(tds[dateColIdx]?.hasAttribute('data-ph-mask')).toBe(false)
        if (categoryColIdx >= 0) expect(tds[categoryColIdx]?.hasAttribute('data-ph-mask')).toBe(true)
      })
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
    it('should make date column sortable when onSortChange is provided', () => {
      renderTable({ onSortChange: mock(() => {}) })

      const dateHeader = screen.getByText('Date')
      expect(dateHeader.closest('button')).toBeInTheDocument()
    })

    it('calls onSortChange with asc on first click of an unsorted column', () => {
      const onSortChange = mock(() => {})
      renderTable({ onSortChange })

      const dateHeader = screen.getByText('Date').closest('button')!
      fireEvent.click(dateHeader)

      expect(onSortChange).toHaveBeenCalledWith({ columnId: 'date', direction: 'asc' })
    })

    it('calls onSortChange with desc on second click (currently sorted asc)', () => {
      const onSortChange = mock(() => {})
      renderTable({
        onSortChange,
        sort: { columnId: 'date', direction: 'asc' },
      })

      const dateHeader = screen.getByText('Date').closest('button')!
      fireEvent.click(dateHeader)

      expect(onSortChange).toHaveBeenCalledWith({ columnId: 'date', direction: 'desc' })
    })

    it('calls onSortChange(null) on third click (currently sorted desc) — clears sort', () => {
      const onSortChange = mock(() => {})
      renderTable({
        onSortChange,
        sort: { columnId: 'date', direction: 'desc' },
      })

      const dateHeader = screen.getByText('Date').closest('button')!
      fireEvent.click(dateHeader)

      expect(onSortChange).toHaveBeenCalledWith(null)
    })

    it('should show sort indicator on sorted column', () => {
      renderTable({
        onSortChange: mock(() => {}),
        sort: { columnId: 'date', direction: 'asc' },
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

  describe('Phase 5 – DataTable opt-in features', () => {
    it('PII mask retention: amount, merchant, account columns have data-ph-mask after feature wiring', () => {
      const { container } = renderTable()
      const maskedTds = container.querySelectorAll('td[data-ph-mask]')
      // Five masked columns × three rows.
      expect(maskedTds.length).toBe(15)
    })

    it('Resize handles render on columns with resizable:true', () => {
      const { container } = renderTable()
      const resizeHandles = container.querySelectorAll('[data-testid="resize-handle"]')
      // date, merchant, account, category, amount all have resizable:true → 5 handles
      expect(resizeHandles.length).toBeGreaterThan(0)
    })

    it('Drag handles render on columns with reorderable:true', () => {
      const { container } = renderTable()
      const dragHandles = container.querySelectorAll('[data-testid="drag-handle"]')
      // date, merchant, account, category, amount are reorderable (not actions) → 5 handles
      expect(dragHandles.length).toBeGreaterThan(0)
    })

    it('Amount td has data-pinned="right" because amount column has defaultPinned:right', () => {
      const { container } = renderTable()
      const pinnedTds = container.querySelectorAll('td[data-pinned="right"]')
      // One per data row (3 transactions)
      expect(pinnedTds.length).toBe(3)
    })

    it('Hideable columns (account, category, sub_category) appear in ColumnVisibilityMenu', async () => {
      const userEvent = await import('@testing-library/user-event')
      const user = userEvent.default.setup()
      renderTable()

      // The "Columns" button is rendered when hideable columns exist and onColumnVisibilityChange is provided
      const columnsButton = screen.getByRole('button', { name: /columns/i })
      await user.click(columnsButton)

      // account, category, and sub_category should appear as menu items
      expect(screen.getByRole('menuitemcheckbox', { name: /^account$/i })).toBeInTheDocument()
      expect(screen.getByRole('menuitemcheckbox', { name: /^category$/i })).toBeInTheDocument()
      expect(screen.getByRole('menuitemcheckbox', { name: /^sub-category$/i })).toBeInTheDocument()
    })
  })

  describe('Inline Filtering', () => {
    const accountOptions = [
      { value: 'acc-1', label: 'My Checking' },
      { value: 'acc-2', label: 'Savings' },
    ]

    it('should render account filter when accountOptions provided', () => {
      renderTable({ accountOptions, onAccountFilterChange: () => {} })

      const filterButtons = screen.getAllByRole('button', { name: /filter by account/i })
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

    it('should show active filter indicator when account filter has value', () => {
      renderTable({
        accountOptions,
        accountFilter: ['acc-1'],
        onAccountFilterChange: () => {},
      })

      const filterButton = screen.getByRole('button', { name: /filter by account/i })
      expect(filterButton).toHaveClass('column-filter__trigger--active')
    })

    it('should not show filter icons when filter props not provided', () => {
      renderTable()

      expect(screen.queryByRole('button', { name: /filter by account/i })).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /filter by amount/i })).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /filter by sub-category/i })).not.toBeInTheDocument()
    })

    it('renders sub-category filter button when onDetailedsChange is provided', () => {
      renderTable({ selectedDetaileds: [], onDetailedsChange: mock(() => {}) })

      expect(screen.getByRole('button', { name: /filter by sub-category/i })).toBeInTheDocument()
    })

    it('calls onDetailedsChange when a sub-category filter option is selected', async () => {
      const onDetailedsChange = mock(() => {})
      const userEvent = await import('@testing-library/user-event')
      const user = userEvent.default.setup()

      renderTable({ selectedDetaileds: [], onDetailedsChange })

      await user.click(screen.getByRole('button', { name: /filter by sub-category/i }))
      await user.click(screen.getByText('Coffee'))
      expect(onDetailedsChange).toHaveBeenCalledWith(['FOOD_AND_DRINK_COFFEE'])
    })

    it('shows sub-category filter button as active when selectedDetaileds is non-empty', () => {
      renderTable({
        selectedDetaileds: ['FOOD_AND_DRINK_COFFEE'],
        onDetailedsChange: mock(() => {}),
      })

      const btn = screen.getByRole('button', { name: /filter by sub-category/i })
      expect(btn).toHaveClass('column-filter__trigger--active')
    })
  })

  describe('Sub-category column', () => {
    it('renders detailed label for a transaction with a known detailed PFC value', () => {
      const txnWithDetailed: Transaction = {
        ...mockTransactions[1], // INCOME, INCOME_WAGES
        id: 'txn-detailed',
        pfc_primary: 'INCOME',
        pfc_detailed: 'INCOME_WAGES',
        category_override_kind: null,
      }
      render(
        <TransactionTable
          transactions={[txnWithDetailed] as unknown as FeedRow[]}
          accountsMap={mockAccounts}
        />
      )
      // getDetailedLabel('INCOME_WAGES') = 'Wages'
      expect(screen.getByText('Wages')).toBeInTheDocument()
    })

    it('renders "—" placeholder when transaction has no detailed category', () => {
      const txnNoDetailed: Transaction = {
        ...mockTransactions[0],
        id: 'txn-no-detailed',
        pfc_primary: 'FOOD_AND_DRINK',
        pfc_detailed: null,
        pfc_detailed_override: null,
        category_override_kind: null,
      }
      render(
        <TransactionTable
          transactions={[txnNoDetailed] as unknown as FeedRow[]}
          accountsMap={mockAccounts}
        />
      )
      expect(screen.getByText('—')).toBeInTheDocument()
    })

    // #1631 C4: the investment branch of the Sub-category renderer had no coverage at all.
    it('renders "Dividend Reinvestment" for a reinvestment row whose raw subtype is "withdrawal"', () => {
      const reinvestRow: InvestmentActivity = {
        id: 'inv-reinvest',
        user_id: 'user-1',
        account_id: 'acc-1',
        source: 'investment',
        amount: -50,
        date: '2025-03-10',
        name: 'Reinvest AAPL',
        activity_type: 'reinvest',
        investment_subtype: 'withdrawal',
        created_at: '2025-03-10T00:00:00Z',
        updated_at: '2025-03-10T00:00:00Z',
      }
      render(<TransactionTable transactions={[reinvestRow]} accountsMap={mockAccounts} />)
      expect(screen.getByText('Dividend Reinvestment')).toBeInTheDocument()
    })

    it('renders the humanised provider subtype for a non-structural investment row', () => {
      const feeRow: InvestmentActivity = {
        id: 'inv-fee',
        user_id: 'user-1',
        account_id: 'acc-1',
        source: 'investment',
        amount: -5,
        date: '2025-03-10',
        name: 'Management fee',
        activity_type: 'fee',
        investment_subtype: 'management fee',
        created_at: '2025-03-10T00:00:00Z',
        updated_at: '2025-03-10T00:00:00Z',
      }
      render(<TransactionTable transactions={[feeRow]} accountsMap={mockAccounts} />)
      expect(screen.getByText('Management Fee')).toBeInTheDocument()
    })
  })

  describe('Hierarchical category filter', () => {
    it('renders HierarchicalCategoryTreeSelect when selectedPrimaries and onPrimariesChange are provided', () => {
      renderTable({
        selectedPrimaries: [],
        onPrimariesChange: () => {},
        selectedDetaileds: [],
        onDetailedsChange: () => {},
      })
      // The mocked component renders a button with accessible name "Filter by category"
      expect(screen.getByRole('button', { name: /filter by category/i })).toBeInTheDocument()
    })

    it('does not render HierarchicalCategoryTreeSelect when hierarchical props are absent', () => {
      renderTable()
      expect(screen.queryByRole('button', { name: /filter by category/i })).not.toBeInTheDocument()
    })

    it('shows active-dot indicator when selectedPrimaries is non-empty', () => {
      renderTable({
        selectedPrimaries: ['FOOD_AND_DRINK'],
        onPrimariesChange: () => {},
        selectedDetaileds: [],
        onDetailedsChange: () => {},
      })
      // Real HierarchicalCategoryTreeSelect renders a span[data-testid="active-dot"] inside the trigger
      expect(screen.getByTestId('active-dot')).toBeInTheDocument()
    })
  })

  // ─── Investment row rendering (Task 4) ──────────────────────────────────────
  const mockInvestmentRow: InvestmentActivity = {
    id: 'inv-1',
    user_id: 'user-1',
    account_id: 'acc-1',
    source: 'investment',
    amount: -1500.0,
    date: '2025-03-10',
    name: 'Buy AAPL',
    ticker_symbol: 'AAPL',
    security_name: 'Apple Inc.',
    activity_type: 'buy',
    investment_quantity: 10,
    investment_price: 150.0,
    investment_fees: 0.0,
    created_at: '2025-03-10T00:00:00Z',
    updated_at: '2025-03-10T00:00:00Z',
  }

  const mockBankingRow: BankingTransaction = {
    id: 'bank-1',
    user_id: 'user-1',
    account_id: 'acc-1',
    source: 'banking',
    amount: 45.99,
    date: '2025-03-11',
    name: 'AMAZON.COM',
    merchant_name: 'Amazon',
    pending: false,
    created_at: '2025-03-11T00:00:00Z',
    updated_at: '2025-03-11T00:00:00Z',
  }

  const renderFeedTable = (props: Partial<Parameters<typeof TransactionTable>[0]> = {}) =>
    render(
      <TransactionTable
        transactions={[mockInvestmentRow, mockBankingRow]}
        accountsMap={mockAccounts}
        {...props}
      />
    )

  describe('Investment row — merchant (Description) cell', () => {
    it('shows ticker · security_name for investment rows instead of merchant_name', () => {
      renderFeedTable()
      expect(screen.getByText('AAPL · Apple Inc.')).toBeInTheDocument()
    })

    it('does NOT show "Buy AAPL" (name field) in the description cell for investment rows', () => {
      renderFeedTable()
      // The name field should not appear in the description column
      // (it appears in the row name but not as the primary description)
      const descriptionCells = screen.getAllByText('AAPL · Apple Inc.')
      expect(descriptionCells.length).toBeGreaterThan(0)
    })

    it('still shows merchant_name for banking rows', () => {
      renderFeedTable()
      expect(screen.getByText('Amazon')).toBeInTheDocument()
    })

    it('does NOT render tag badges for investment rows', () => {
      // Investment rows have no tags — even if tags array is present, no badge spam
      renderFeedTable()
      // The investment row has no tags property, so no tag badges should render for it
      // Banking row also has no tags in this fixture, so zero tag badges total
      // If investment had tags, they should not appear (investment rows have no tags semantics)
    })
  })

  describe('Investment row — category cell', () => {
    it('renders ActivityTypeChip for investment rows', () => {
      renderFeedTable()
      // ActivityTypeChip for 'buy' renders "Buy" label
      expect(screen.getByText('Buy')).toBeInTheDocument()
    })

    it('does NOT render a color dot (category dot) for investment rows', () => {
      const { container } = renderFeedTable()
      // Find the investment row's category cell and verify no color-dot span is present
      // The banking row has a category dot; investment row uses ActivityTypeChip instead
      // We check that only one color dot exists (for the banking row)
      const colorDots = container.querySelectorAll('span.h-2.w-2.shrink-0.rounded-full')
      // Banking row has one dot; investment row should have none (uses chip instead)
      expect(colorDots.length).toBeLessThanOrEqual(1)
    })
  })

  describe('Investment row — amount cell', () => {
    it('shows "Investment" badge for investment rows', () => {
      renderFeedTable()
      expect(screen.getByText('Investment')).toBeInTheDocument()
    })

    it('does NOT show "Pending" badge for investment rows', () => {
      renderFeedTable()
      // Investment rows are never pending
      expect(screen.queryByText('Pending')).not.toBeInTheDocument()
    })
  })

  describe('Investment row — row actions', () => {
    it('shows NO edit button for investment rows when onEdit is provided', () => {
      const onEdit = mock(() => {})
      renderFeedTable({ onEdit })
      // Banking row gets an edit button; investment row should NOT
      const editButtons = screen.getAllByRole('button', { name: /edit transaction/i })
      // Only 1 edit button (for the banking row), not 2
      expect(editButtons.length).toBe(1)
    })

    it('shows NO categorize button for investment rows when onCategorize is provided', () => {
      const onCategorize = mock(() => {})
      renderFeedTable({ onCategorize })
      const categorizeButtons = screen.getAllByRole('button', { name: /categorize transaction/i })
      // Only 1 categorize button (for the banking row), not 2
      expect(categorizeButtons.length).toBe(1)
    })

    it('shows edit and categorize for banking rows but not investment rows', () => {
      const onEdit = mock(() => {})
      const onCategorize = mock(() => {})
      renderFeedTable({ onEdit, onCategorize })
      const editButtons = screen.getAllByRole('button', { name: /edit transaction/i })
      const categorizeButtons = screen.getAllByRole('button', { name: /categorize transaction/i })
      expect(editButtons.length).toBe(1)
      expect(categorizeButtons.length).toBe(1)
    })
  })

  describe('Investment row — PII masking', () => {
    it('investment row merchant cell (ticker·name) still has data-ph-mask', () => {
      const { container } = renderFeedTable()
      const maskedTds = container.querySelectorAll('td[data-ph-mask]')
      // Two rows × five masked columns.
      expect(maskedTds.length).toBe(10)
    })
  })

  describe('Investment row — onRowClick routing (#1299)', () => {
    it('calls onRowClick with the investment row object when investment row is clicked', () => {
      const onRowClick = mock(() => {})
      renderFeedTable({ onRowClick })

      // Click the investment row's ticker·name cell (the merchant-column text)
      const investmentCell = screen.getByText('AAPL · Apple Inc.')
      fireEvent.click(investmentCell)

      expect(onRowClick).toHaveBeenCalledTimes(1)
      // The argument passed to onRowClick should be the investment row (source: 'investment')
      const calledWith = (onRowClick.mock.calls[0] as unknown[])[0] as { source: string }
      expect(calledWith.source).toBe('investment')
    })

    it('calls onRowClick with the banking row object when banking row is clicked', () => {
      const onRowClick = mock(() => {})
      renderFeedTable({ onRowClick })

      // Click the banking row's merchant cell
      const bankingCell = screen.getByText('Amazon')
      fireEvent.click(bankingCell)

      expect(onRowClick).toHaveBeenCalledTimes(1)
      const calledWith = (onRowClick.mock.calls[0] as unknown[])[0] as { source: string }
      expect(calledWith.source).toBe('banking')
    })
  })

  // R3 — type-soundness: sparse investment row (API fields may be null pointers)
  describe('Investment row — sparse fields graceful rendering (R3)', () => {
    const sparseRow: InvestmentActivity = {
      id: 'inv-sparse',
      user_id: 'user-1',
      account_id: 'acc-1',
      source: 'investment',
      amount: 0,
      date: '2025-03-10',
      name: 'Unknown investment',
      // investment-specific fields absent (simulates sparse API response)
      ticker_symbol: undefined,
      security_name: undefined,
      activity_type: undefined,
      investment_quantity: undefined,
      investment_price: undefined,
      investment_fees: undefined,
      created_at: '2025-03-10T00:00:00Z',
      updated_at: '2025-03-10T00:00:00Z',
    }

    it('renders "—" placeholders and never renders the literal string "undefined"', () => {
      render(
        <TransactionTable
          transactions={[sparseRow]}
          accountsMap={mockAccounts}
        />
      )
      // The literal string "undefined" must never appear in the DOM
      expect(screen.queryByText(/\bundefined\b/)).not.toBeInTheDocument()
    })

    // #1631: when ticker_symbol AND security_name are both blank, the transaction's own name
    // is the only field with content — falling back to it means a blank security can never
    // produce an empty (or "— · —") cell. Before #1631 this rendered the literal "— · —".
    it('falls back to row.name when both ticker_symbol and security_name are blank', () => {
      render(
        <TransactionTable
          transactions={[sparseRow]}
          accountsMap={mockAccounts}
        />
      )
      expect(screen.getByText('Unknown investment')).toBeInTheDocument()
      expect(screen.queryByText('— · —')).not.toBeInTheDocument()
    })

    it('falls back to "—" when row.name is also blank', () => {
      const blankNameRow: InvestmentActivity = { ...sparseRow, id: 'inv-sparse-2', name: '' }
      render(
        <TransactionTable
          transactions={[blankNameRow]}
          accountsMap={mockAccounts}
        />
      )
      // Other cells in the row (e.g. amount) may independently render '—', so scope to the
      // Description cell specifically rather than asserting a single '—' in the whole table.
      const row = screen.getByText('—', { selector: '.font-medium.text-foreground' })
      expect(row).toBeInTheDocument()
    })
  })

  // R4 — read-only: mutation callbacks (onEdit/onCategorize) never fire for investment rows
  describe('Investment row — read-only: mutation callbacks not triggered (R4)', () => {
    it('onEdit and onCategorize are NOT called when an investment row is clicked', () => {
      const onEdit = mock(() => {})
      const onCategorize = mock(() => {})
      const onRowClick = mock(() => {})
      renderFeedTable({ onEdit, onCategorize, onRowClick })

      fireEvent.click(screen.getByText('AAPL · Apple Inc.'))

      // onRowClick fires (page routes to drawer)
      expect(onRowClick).toHaveBeenCalledTimes(1)
      // onEdit and onCategorize must NEVER be triggered for an investment row
      expect(onEdit).not.toHaveBeenCalled()
      expect(onCategorize).not.toHaveBeenCalled()
    })
  })
  // ── #1569 T12 gap 1: reversed transactions ────────────────────────────────────────────
  //
  // The API can express "the bank withdrew this after it posted" and, until now, nothing in
  // apps/web consumed it. An unlabelled reversal sitting in the list is WORSE than a hidden one
  // — it reads as a live transaction. WCAG 2.1 AA: the state carries a text label, not a tint.
  describe('reversed transactions (#1569 T12)', () => {
    const reversedRow: BankingTransaction = {
      ...mockBankingRow,
      id: 'bank-reversed',
      name: 'DUPLICATE CHARGE',
      merchant_name: 'Contoso',
      reversed: true,
      reversed_at: '2025-03-14T09:00:00Z',
    }

    const renderRows = (rows: FeedRow[]) =>
      render(<TransactionTable transactions={rows} accountsMap={mockAccounts} />)

    it('labels a reversed row in words, not by colour alone', () => {
      renderRows([reversedRow])
      expect(screen.getByText('Reversed')).toBeInTheDocument()
    })

    it('does not label an ordinary posted row', () => {
      renderRows([mockBankingRow])
      expect(screen.queryByText('Reversed')).not.toBeInTheDocument()
    })

    it('does not label a row whose reversed flag is explicitly false', () => {
      renderRows([{ ...mockBankingRow, reversed: false }])
      expect(screen.queryByText('Reversed')).not.toBeInTheDocument()
    })

    it('gives the row an accessible reversed state for screen readers', () => {
      const { container } = renderRows([reversedRow])
      expect(container.querySelector('[data-reversed="true"]')).not.toBeNull()
    })

    // The amount is struck through so the row reads as undone at a glance. The strike is
    // ADDITIONAL to the label, never instead of it.
    it('strikes through the amount of a reversed row', () => {
      const { container } = renderRows([reversedRow])
      expect(container.querySelector('.line-through')).not.toBeNull()
    })

    it('does not strike through an ordinary row', () => {
      const { container } = renderRows([mockBankingRow])
      expect(container.querySelector('.line-through')).toBeNull()
    })

    // ⚠ DataTable masks only the columns named in maskedColumnIds, and NOTHING masks an
    // attribute — session replay records those verbatim. The reversed marker must therefore be
    // structural (a boolean) and must never carry the amount or the merchant.
    it('leaks no financial detail into the attributes session replay records verbatim', () => {
      const { container } = renderRows([reversedRow])
      const marked = container.querySelector('[data-reversed="true"]')
      expect(marked).not.toBeNull()
      if (!marked) return
      for (const attr of Array.from(marked.attributes)) {
        expect(attr.value).not.toContain('45.99')
        expect(attr.value).not.toContain('Contoso')
        expect(attr.value).not.toContain('DUPLICATE CHARGE')
      }
    })

    // ── C1: contrast ────────────────────────────────────────────────────────────────────
    //
    // The first version of this dimmed the row with `opacity-70` and the amount with
    // `text-muted-foreground`. Rows have no background of their own (DataTable is bg-card), so
    // the opacity composited straight onto the ground and took a 14px BOLD amount — not WCAG
    // "large text", so the 4.5:1 floor applies — to 2.71:1 in light and 4.02:1 on dark card.
    // Computed from src/index.css tokens; see the report for the full table.
    //
    // The fix is to remove the opacity entirely rather than tune it: a reversed row now
    // composites on exactly the same ground as every other row, so no ratio anywhere can regress.
    // The strike-through and the badge carry "undone"; the number stays fully legible, because
    // the most important thing on a reversed row should not be the least readable thing on it.
    it('does not dim the row, which would composite every cell below the AA floor', () => {
      const { container } = renderRows([reversedRow])
      expect(container.querySelector('tr.opacity-70')).toBeNull()
      expect(container.querySelector('[class*="opacity-"]')).toBeNull()
    })

    it('renders the struck amount at full foreground contrast, not muted', () => {
      const { container } = renderRows([reversedRow])
      const struck = container.querySelector('.line-through')
      expect(struck).not.toBeNull()
      expect(struck?.className).toContain('text-foreground')
      expect(struck?.className).not.toContain('text-muted-foreground')
    })

    // ── I2: a settled pending must render NOWHERE ───────────────────────────────────────
    //
    // The backend gates this twice (the query excludes TxnDeletedProviderRemovedPending, and
    // Transaction.Reversed() returns false for it), and TestTombstone_SettledPendingIsNeverSurfaced
    // pins it. This is the third gate, and it exists because of what was measured when one was
    // fed in anyway: REVERSED BADGE true, PENDING BADGE false, AMOUNT RENDERED true — a settled
    // pending rendered as an ordinary posted reversal, next to the posted twin it duplicates.
    //
    // Double-counting real money is the one failure mode worth a redundant client-side check,
    // so `pending && reversed` is treated as unrenderable rather than as a reversal.
    it('renders a pending+reversed row nowhere — it is a settled pending, not a reversal', () => {
      const { container } = renderRows([{ ...reversedRow, pending: true }])
      expect(screen.queryByText('Reversed')).not.toBeInTheDocument()
      expect(screen.queryByText('Pending')).not.toBeInTheDocument()
      expect(container.textContent).not.toContain('45.99')
      expect(container.textContent).not.toContain('DUPLICATE CHARGE')
    })

    it('does not swallow the other rows when a settled pending is filtered out', () => {
      renderRows([{ ...reversedRow, pending: true }, mockBankingRow])
      expect(screen.getByText('Amazon')).toBeInTheDocument()
    })

    it('still renders an ordinary pending row, which is not a settled pending', () => {
      renderRows([{ ...mockBankingRow, pending: true }])
      expect(screen.getByText('Pending')).toBeInTheDocument()
    })

    // Tightened: td[data-ph-mask] alone matches ANY masked cell (merchant, account), so it
    // passed even when the amount column was not in maskedColumnIds. Key on the cell that
    // actually contains the amount.
    it('keeps the amount cell inside the masked column set', () => {
      const { container } = renderRows([reversedRow])
      const maskedCells = Array.from(container.querySelectorAll('td[data-ph-mask]'))
      const amountCell = maskedCells.find((td) => td.querySelector('[data-reversed="true"]'))
      expect(amountCell).toBeDefined()
      expect(amountCell?.textContent).toContain('45.99')
    })
  })
})
