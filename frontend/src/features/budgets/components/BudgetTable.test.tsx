/**
 * BudgetTable Component Tests
 * Issue #584: Unified Card/Table View System
 *
 * Tests for the budget table display component.
 * TDD RED: Writing tests before implementation.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
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
    income_amount: 0,
    income_sources: [],
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
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Rendering', () => {
    it('should render a table', () => {
      render(<BudgetTable budgets={mockBudgets} />)

      expect(screen.getByRole('table')).toBeInTheDocument()
    })

    it('should render table headers', () => {
      render(<BudgetTable budgets={mockBudgets} />)

      expect(screen.getByText('Name')).toBeInTheDocument()
      expect(screen.getByText('Period')).toBeInTheDocument()
      expect(screen.getByText('Amount')).toBeInTheDocument()
      expect(screen.getByText('Progress')).toBeInTheDocument()
      expect(screen.getByText('Remaining')).toBeInTheDocument()
      expect(screen.getByText('Categories')).toBeInTheDocument()
    })

    it('should render all budgets', () => {
      render(<BudgetTable budgets={mockBudgets} />)

      expect(screen.getByText('Monthly Groceries')).toBeInTheDocument()
      expect(screen.getByText('Entertainment')).toBeInTheDocument()
      expect(screen.getByText('Vacation Savings')).toBeInTheDocument()
    })

    it('should display period with proper label', () => {
      render(<BudgetTable budgets={mockBudgets} />)

      expect(screen.getByText('Monthly')).toBeInTheDocument()
      expect(screen.getByText('Weekly')).toBeInTheDocument()
      expect(screen.getByText('Yearly')).toBeInTheDocument()
    })

    it('should display formatted amount', () => {
      render(<BudgetTable budgets={mockBudgets} />)

      expect(screen.getByText('$500.00')).toBeInTheDocument()
      // $100.00 appears twice (amount and remaining for Entertainment)
      const hundredDollarCells = screen.getAllByText('$100.00')
      expect(hundredDollarCells.length).toBeGreaterThan(0)
      expect(screen.getByText('$2,000.00')).toBeInTheDocument()
    })

    it('should display category count', () => {
      render(<BudgetTable budgets={mockBudgets} />)

      // budget-1 has 2 categories, budget-2 has 0, budget-3 has 1
      expect(screen.getByText('2')).toBeInTheDocument()
      expect(screen.getByText('0')).toBeInTheDocument()
      expect(screen.getByText('1')).toBeInTheDocument()
    })

    it('should show Active badge for active budgets', () => {
      render(<BudgetTable budgets={mockBudgets} />)

      const activeBadges = screen.getAllByText('Active')
      expect(activeBadges.length).toBe(2) // budget-1 and budget-2 are active
    })

    it('should show Inactive badge for inactive budgets', () => {
      render(<BudgetTable budgets={mockBudgets} />)

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

  describe('Progress Display', () => {
    it('should display progress bar', () => {
      render(<BudgetTable budgets={mockBudgets} />)

      // Check for progress bar elements
      const progressBars = screen.getAllByRole('progressbar')
      expect(progressBars.length).toBe(3)
    })

    it('should show correct progress percentage', () => {
      render(<BudgetTable budgets={mockBudgets} />)

      // Monthly Groceries is 65% used
      expect(screen.getByText('65%')).toBeInTheDocument()
      // Entertainment is 0% used
      expect(screen.getByText('0%')).toBeInTheDocument()
    })

    it('should apply over-budget styling for budgets exceeding limit', () => {
      render(<BudgetTable budgets={mockBudgets} />)

      // Vacation Savings is over budget (125%)
      expect(screen.getByText('125%')).toBeInTheDocument()
    })
  })

  describe('Remaining Amount', () => {
    it('should display formatted remaining amount', () => {
      render(<BudgetTable budgets={mockBudgets} />)

      expect(screen.getByText('$175.00')).toBeInTheDocument() // Monthly Groceries
    })

    it('should apply negative class to negative remaining', () => {
      render(<BudgetTable budgets={mockBudgets} />)

      // Vacation Savings has -$500 remaining
      const negativeRemaining = screen.getByText('-$500.00')
      expect(negativeRemaining).toHaveClass('budget-table__remaining--negative')
    })

    it('should apply positive class to positive remaining', () => {
      render(<BudgetTable budgets={mockBudgets} />)

      const positiveRemaining = screen.getByText('$175.00')
      expect(positiveRemaining).toHaveClass('budget-table__remaining--positive')
    })
  })

  describe('Row Actions', () => {
    it('should call onManage when manage button is clicked', () => {
      const onManage = vi.fn()
      render(<BudgetTable budgets={mockBudgets} onManage={onManage} />)

      const manageButtons = screen.getAllByRole('button', { name: /manage/i })
      fireEvent.click(manageButtons[0])

      expect(onManage).toHaveBeenCalledWith(mockBudgets[0])
    })

    it('should call onDelete when delete button is clicked', () => {
      const onDelete = vi.fn()
      render(<BudgetTable budgets={mockBudgets} onDelete={onDelete} />)

      const deleteButtons = screen.getAllByRole('button', { name: /delete/i })
      fireEvent.click(deleteButtons[0])

      expect(onDelete).toHaveBeenCalledWith(mockBudgets[0].id)
    })

    it('should not show actions column when no handlers provided', () => {
      render(<BudgetTable budgets={mockBudgets} />)

      expect(screen.queryByText('Actions')).not.toBeInTheDocument()
    })

    it('should show actions column when handlers provided', () => {
      render(<BudgetTable budgets={mockBudgets} onManage={() => {}} />)

      expect(screen.getByText('Actions')).toBeInTheDocument()
    })

    it('should disable actions when loading', () => {
      render(
        <BudgetTable
          budgets={mockBudgets}
          onManage={() => {}}
          onDelete={() => {}}
          loading
        />
      )

      const manageButtons = screen.getAllByRole('button', { name: /manage/i })
      const deleteButtons = screen.getAllByRole('button', { name: /delete/i })

      manageButtons.forEach((btn) => expect(btn).toBeDisabled())
      deleteButtons.forEach((btn) => expect(btn).toBeDisabled())
    })
  })

  describe('Row Click', () => {
    it('should call onRowClick when row is clicked', () => {
      const onRowClick = vi.fn()
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
    it('should make name column sortable', () => {
      render(<BudgetTable budgets={mockBudgets} sortable />)

      const nameHeader = screen.getByText('Name')
      expect(nameHeader.closest('button')).toBeInTheDocument()
    })

    it('should call onSort when clicking sortable header', () => {
      const onSort = vi.fn()
      render(<BudgetTable budgets={mockBudgets} sortable onSort={onSort} />)

      const nameHeader = screen.getByText('Name').closest('button')!
      fireEvent.click(nameHeader)

      expect(onSort).toHaveBeenCalledWith('name', 'asc')
    })

    it('should toggle sort direction on second click', () => {
      const onSort = vi.fn()
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
      fireEvent.click(nameHeader)

      expect(onSort).toHaveBeenCalledWith('name', 'desc')
    })

    it('should show sort indicator on sorted column', () => {
      render(
        <BudgetTable
          budgets={mockBudgets}
          sortable
          sortColumn="name"
          sortDirection="asc"
        />
      )

      const nameHeader = screen.getByText('Name').closest('button')!
      expect(nameHeader).toHaveAttribute('aria-sort', 'ascending')
    })
  })

  describe('Accessibility', () => {
    it('should have accessible table structure', () => {
      render(<BudgetTable budgets={mockBudgets} />)

      expect(screen.getByRole('table')).toBeInTheDocument()
      const rowgroups = screen.getAllByRole('rowgroup')
      expect(rowgroups.length).toBe(2) // thead and tbody
      expect(screen.getAllByRole('row').length).toBe(4) // 1 header + 3 data rows
    })

    it('should have accessible column headers', () => {
      render(<BudgetTable budgets={mockBudgets} />)

      const headers = screen.getAllByRole('columnheader')
      expect(headers.length).toBeGreaterThan(0)
    })

    it('should have aria-label on action buttons', () => {
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
    })

    it('should have aria-valuenow on progress bars', () => {
      render(<BudgetTable budgets={mockBudgets} />)

      const progressBars = screen.getAllByRole('progressbar')
      expect(progressBars[0]).toHaveAttribute('aria-valuenow', '65')
      expect(progressBars[1]).toHaveAttribute('aria-valuenow', '0')
      expect(progressBars[2]).toHaveAttribute('aria-valuenow', '125')
    })
  })

  describe('Action Button Tooltips', () => {
    it('should have tooltip on manage button', () => {
      render(<BudgetTable budgets={mockBudgets} onManage={vi.fn()} />)

      const manageButtons = screen.getAllByRole('button', { name: /manage budget/i })
      manageButtons.forEach((btn) => {
        expect(btn).toHaveAttribute('title', 'Manage budget')
      })
    })

    it('should have tooltip on delete button', () => {
      render(<BudgetTable budgets={mockBudgets} onDelete={vi.fn()} />)

      const deleteButtons = screen.getAllByRole('button', { name: /delete budget/i })
      deleteButtons.forEach((btn) => {
        expect(btn).toHaveAttribute('title', 'Delete budget')
      })
    })
  })
})
