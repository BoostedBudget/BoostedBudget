import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { BudgetTable } from './BudgetTable'
import type { BudgetWithMetrics } from '../types'

const mockBudgets: BudgetWithMetrics[] = [
  {
    id: 'budget-1',
    user_id: 'user-1',
    name: 'Monthly Groceries',
    description: 'Food budget for the month',
    period: 'monthly',
    start_date: '2024-01-01T00:00:00Z',
    amount: 500,
    currency_code: 'USD',
    is_active: true,
    categories: [
      {
        id: 'cat-1',
        budget_id: 'budget-1',
        category: ['Food and Drink', 'Groceries'],
        allocated_amount: 400,
        spent_amount: 250,
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z',
        tags: [],
        match_logic: 'or',
      },
      {
        id: 'cat-2',
        budget_id: 'budget-1',
        category: ['Food and Drink', 'Restaurants'],
        allocated_amount: 100,
        spent_amount: 75,
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z',
        tags: [],
        match_logic: 'or',
      },
    ],
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    income_amount: 800,
    income_sources: [],
    expected_income: 2000,
    // Computed metrics
    total_allocated: 500,
    total_spent: 325,
    remaining_amount: 175,
    percent_used: 65,
    is_over_budget: false,
    unallocated_funds: 0,
    is_fully_allocated: true,
  },
  {
    id: 'budget-2',
    user_id: 'user-1',
    name: 'Entertainment',
    period: 'weekly',
    start_date: '2024-01-01T00:00:00Z',
    amount: 100,
    currency_code: 'USD',
    is_active: true,
    categories: [],
    created_at: '2024-01-02T00:00:00Z',
    updated_at: '2024-01-02T00:00:00Z',
    income_amount: 0,
    income_sources: [],
    // Computed metrics
    total_allocated: 0,
    total_spent: 0,
    remaining_amount: 100,
    percent_used: 0,
    is_over_budget: false,
    unallocated_funds: 0,
    is_fully_allocated: false,
  },
  {
    id: 'budget-3',
    user_id: 'user-1',
    name: 'Vacation Savings',
    period: 'yearly',
    start_date: '2024-01-01T00:00:00Z',
    amount: 2000,
    currency_code: 'USD',
    is_active: false,
    categories: [
      {
        id: 'cat-3',
        budget_id: 'budget-3',
        category: ['Travel'],
        allocated_amount: 2000,
        spent_amount: 2500,
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z',
        tags: [],
        match_logic: 'or',
      },
    ],
    created_at: '2024-01-03T00:00:00Z',
    updated_at: '2024-01-03T00:00:00Z',
    income_amount: 0,
    income_sources: [],
    // Computed metrics
    total_allocated: 2000,
    total_spent: 2500,
    remaining_amount: -500,
    percent_used: 125,
    is_over_budget: true,
    unallocated_funds: 0,
    is_fully_allocated: true,
  },
]

describe('BudgetTable', () => {

  describe('Rendering', () => {
    it('should render table with headers and all budget rows', () => {
      render(<BudgetTable budgets={mockBudgets} />)

      expect(screen.getByRole('table')).toBeInTheDocument()
      expect(screen.getByText('Name')).toBeInTheDocument()
      expect(screen.getByText('Period')).toBeInTheDocument()
      expect(screen.getByText('Amount')).toBeInTheDocument()
      expect(screen.getByText('Progress')).toBeInTheDocument()
      expect(screen.getByText('Remaining')).toBeInTheDocument()
      expect(screen.getByText('Categories')).toBeInTheDocument()
      expect(screen.getByText('Income')).toBeInTheDocument()

      expect(screen.getByText('Monthly Groceries')).toBeInTheDocument()
      expect(screen.getByText('Entertainment')).toBeInTheDocument()
      expect(screen.getByText('Vacation Savings')).toBeInTheDocument()
    })

    it('should display formatted periods and amounts', () => {
      render(<BudgetTable budgets={mockBudgets} />)

      expect(screen.getByText('Monthly')).toBeInTheDocument()
      expect(screen.getByText('Weekly')).toBeInTheDocument()
      expect(screen.getByText('Yearly')).toBeInTheDocument()

      // Amount column shows expected_income when set, otherwise falls back to amount.
      // budget-1 has expected_income: 2000, budget-2 has no expected_income (amount 100),
      // budget-3 has no expected_income (amount 2000).
      expect(screen.getAllByText('$2,000.00').length).toBeGreaterThan(0)
      expect(screen.getAllByText('$100.00').length).toBeGreaterThan(0)
    })

    it('should display category counts and status badges', () => {
      render(<BudgetTable budgets={mockBudgets} />)

      expect(screen.getByText('2')).toBeInTheDocument()
      expect(screen.getByText('0')).toBeInTheDocument()
      expect(screen.getByText('1')).toBeInTheDocument()

      const activeBadges = screen.getAllByText('Active')
      expect(activeBadges.length).toBe(2)
      expect(screen.getByText('Inactive')).toBeInTheDocument()
    })

    it('should show empty state when no budgets', () => {
      render(<BudgetTable budgets={[]} />)
      expect(screen.getByText(/no budgets found/i)).toBeInTheDocument()
    })

    it('should apply loading class when loading', () => {
      render(<BudgetTable budgets={mockBudgets} loading />)
      expect(screen.getByRole('table')).toHaveClass('budget-table--loading')
    })
  })

  describe('Income Column', () => {
    it('renders Income column header', () => {
      render(<BudgetTable budgets={mockBudgets} />)
      expect(screen.getByText('Income')).toBeInTheDocument()
    })

    it('renders income amount in income column for budget with expected_income', () => {
      render(<BudgetTable budgets={mockBudgets} />)
      // budget-1 has income_amount: 800, expected_income: 2000
      expect(screen.getByText('$800.00 / $2,000.00')).toBeInTheDocument()
    })

    it('renders dash for budget without expected_income', () => {
      render(<BudgetTable budgets={mockBudgets} />)
      // budget-2 and budget-3 have no expected_income; should show '—'
      const dashes = screen.getAllByText('—')
      expect(dashes.length).toBeGreaterThanOrEqual(1)
    })
  })

  describe('Progress & Remaining', () => {
    it('should display progress bars with correct percentages', () => {
      render(<BudgetTable budgets={mockBudgets} />)

      const progressBars = screen.getAllByRole('progressbar')
      expect(progressBars).toHaveLength(3)

      expect(screen.getByText('65%')).toBeInTheDocument()
      expect(screen.getByText('0%')).toBeInTheDocument()
      expect(screen.getByText('125%')).toBeInTheDocument()
    })

    it('should display and style remaining amounts correctly', () => {
      render(<BudgetTable budgets={mockBudgets} />)

      const positiveRemaining = screen.getByText('$175.00')
      expect(positiveRemaining).toHaveClass('budget-table__remaining--positive')

      const negativeRemaining = screen.getByText('-$500.00')
      expect(negativeRemaining).toHaveClass('budget-table__remaining--negative')
    })
  })

  describe('Row Actions', () => {
    it('should toggle actions column visibility based on handlers', () => {
      const { rerender } = render(<BudgetTable budgets={mockBudgets} />)
      expect(screen.queryByText('Actions')).not.toBeInTheDocument()

      rerender(<BudgetTable budgets={mockBudgets} onManage={() => {}} />)
      expect(screen.getByText('Actions')).toBeInTheDocument()
    })

    it('should call action handlers when buttons are clicked', () => {
      const onManage = mock(() => {})
      const onDelete = mock(() => {})
      render(<BudgetTable budgets={mockBudgets} onManage={onManage} onDelete={onDelete} />)

      fireEvent.click(screen.getAllByRole('button', { name: /manage/i })[0])
      expect(onManage).toHaveBeenCalledWith(mockBudgets[0])

      fireEvent.click(screen.getAllByRole('button', { name: /delete/i })[0])
      expect(onDelete).toHaveBeenCalledWith(mockBudgets[0].id)
    })

    it('should disable action buttons when loading', () => {
      render(
        <BudgetTable
          budgets={mockBudgets}
          onManage={() => {}}
          onDelete={() => {}}
          loading
        />
      )

      screen.getAllByRole('button', { name: /manage|delete/i }).forEach((btn) => {
        expect(btn).toBeDisabled()
      })
    })
  })

  describe('Row Click', () => {
    it('should call onRowClick when row is clicked', () => {
      const onRowClick = mock(() => {})
      render(<BudgetTable budgets={mockBudgets} onRowClick={onRowClick} />)

      const rows = screen.getAllByRole('row')
      // Skip header row
      fireEvent.click(rows[1])

      expect(onRowClick).toHaveBeenCalledWith(mockBudgets[0])
    })

    it('should make rows clickable when onRowClick provided', () => {
      render(<BudgetTable budgets={mockBudgets} onRowClick={() => {}} />)

      const rows = screen.getAllByRole('row')
      expect(rows[1]).toHaveClass('budget-table__row--clickable')
    })
  })

  describe('Sorting', () => {
    it('should make headers clickable and handle sort callbacks', () => {
      const onSort = mock(() => {})
      render(<BudgetTable budgets={mockBudgets} sortable onSort={onSort} />)

      const nameHeader = screen.getByText('Name').closest('button')!
      expect(nameHeader).toBeInTheDocument()

      fireEvent.click(nameHeader)
      expect(onSort).toHaveBeenCalledWith('name', 'asc')
    })

    it('should toggle sort direction and show aria-sort indicator', () => {
      const onSort = mock(() => {})
      render(
        <BudgetTable
          budgets={mockBudgets}
          sortable
          onSort={onSort}
          sortColumn="name"
          sortDirection="asc"
        />
      )

      const nameHeader = screen.getByText('Name').closest('button')!
      expect(nameHeader).toHaveAttribute('aria-sort', 'ascending')

      fireEvent.click(nameHeader)
      expect(onSort).toHaveBeenCalledWith('name', 'desc')
    })
  })

  describe('Accessibility', () => {
    it('should have proper table structure and headers', () => {
      render(<BudgetTable budgets={mockBudgets} />)

      expect(screen.getByRole('table')).toBeInTheDocument()
      expect(screen.getAllByRole('rowgroup')).toHaveLength(2)
      expect(screen.getAllByRole('row')).toHaveLength(4)
      expect(screen.getAllByRole('columnheader').length).toBeGreaterThan(0)
    })

    it('should have proper aria attributes on interactive elements', () => {
      render(
        <BudgetTable
          budgets={mockBudgets}
          onManage={() => {}}
          onDelete={() => {}}
        />
      )

      const manageButtons = screen.getAllByRole('button', { name: /manage budget/i })
      const deleteButtons = screen.getAllByRole('button', { name: /delete budget/i })
      expect(manageButtons.length).toBe(3)
      expect(deleteButtons.length).toBe(3)

      manageButtons.forEach((btn) => {
        expect(btn).toHaveAttribute('title', 'Manage budget')
      })
      deleteButtons.forEach((btn) => {
        expect(btn).toHaveAttribute('title', 'Delete budget')
      })

      const progressBars = screen.getAllByRole('progressbar')
      expect(progressBars[0]).toHaveAttribute('aria-valuenow', '65')
      expect(progressBars[1]).toHaveAttribute('aria-valuenow', '0')
      expect(progressBars[2]).toHaveAttribute('aria-valuenow', '125')
    })
  })
})
