import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { BudgetCard } from './BudgetCard'
import type { Budget } from '../types'

function renderWithRouter(ui: React.ReactElement) {
  return render(
    <MemoryRouter initialEntries={['/budgets']}>
      <Routes>
        <Route path="/budgets" element={ui} />
        <Route path="/budgets/:budgetId" element={<div data-testid="budget-detail-page">Budget Detail</div>} />
      </Routes>
    </MemoryRouter>
  )
}

describe('BudgetCard', () => {
  const mockBudget: Budget = {
    id: 'budget-1',
    user_id: 'user-123',
    name: 'Monthly Budget',
    description: 'My monthly budget',
    period: 'monthly',
    start_date: '2025-01-01T00:00:00Z',
    end_date: '2025-01-31T23:59:59Z',
    amount: 2000,
    currency_code: 'USD',
    is_active: true,
    categories: [
      {
        id: 'cat-1',
        budget_id: 'budget-1',
        category: ['Food and Drink', 'Restaurants'],
        allocated_amount: 500,
        spent_amount: 150,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T00:00:00Z',
        tags: [],
        match_logic: 'category_only',
      },
      {
        id: 'cat-2',
        budget_id: 'budget-1',
        category: ['Transportation'],
        allocated_amount: 300,
        spent_amount: 100,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T00:00:00Z',
        tags: [],
        match_logic: 'category_only',
      },
    ],
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
    income_amount: 0,
    income_sources: [],
  }

  describe('Rendering', () => {
    it('should render budget details', () => {
      renderWithRouter(<BudgetCard budget={mockBudget} />)

      expect(screen.getByText('Monthly Budget')).toBeInTheDocument()
      expect(screen.getByText('My monthly budget')).toBeInTheDocument()
      expect(screen.getByText('Monthly')).toBeInTheDocument()
      expect(screen.getByText('$2,000.00')).toBeInTheDocument()
      expect(screen.getByText('Active')).toBeInTheDocument()
    })

    it('should not show active badge when budget is inactive', () => {
      const inactiveBudget = { ...mockBudget, is_active: false }
      renderWithRouter(<BudgetCard budget={inactiveBudget} />)

      expect(screen.queryByText('Active')).not.toBeInTheDocument()
    })
  })

  describe('Progress Display', () => {
    it('should display budget progress information', () => {
      renderWithRouter(<BudgetCard budget={mockBudget} />)

      expect(screen.getByText('$250.00')).toBeInTheDocument()
      expect(screen.getByText('13%')).toBeInTheDocument()
      expect(screen.getByText(/\$1,750\.00.*remaining/i)).toBeInTheDocument()
    })
  })

  describe('Categories', () => {
    it('should show category count', () => {
      renderWithRouter(<BudgetCard budget={mockBudget} />)

      expect(screen.getByText(/2 categories/i)).toBeInTheDocument()
    })

    it('should handle budget with no categories', () => {
      const budgetNoCategories = { ...mockBudget, categories: [] }
      renderWithRouter(<BudgetCard budget={budgetNoCategories} />)

      expect(screen.getByText(/0 categories/i)).toBeInTheDocument()
    })
  })

  describe('Date Display', () => {
    it('should show date range', () => {
      renderWithRouter(<BudgetCard budget={mockBudget} />)

      const datesElement = document.querySelector('.budget-card__dates')
      expect(datesElement).toBeInTheDocument()
      expect(datesElement?.textContent).toMatch(/\w{3}\s+\d+.*\w{3}\s+\d+/)
    })
  })

  describe('Interactions', () => {
    it('should call onEdit when edit button is clicked', () => {
      const onEdit = mock(() => {})
      renderWithRouter(<BudgetCard budget={mockBudget} onEdit={onEdit} />)

      fireEvent.click(screen.getByRole('button', { name: /edit/i }))

      expect(onEdit).toHaveBeenCalledWith(mockBudget)
    })

    it('should call onDelete when delete button is clicked', () => {
      const onDelete = mock(() => {})
      renderWithRouter(<BudgetCard budget={mockBudget} onDelete={onDelete} />)

      fireEvent.click(screen.getByRole('button', { name: /delete/i }))

      expect(onDelete).toHaveBeenCalledWith('budget-1')
    })

    it('should not show action buttons when callbacks not provided', () => {
      renderWithRouter(<BudgetCard budget={mockBudget} />)

      expect(screen.queryByRole('button', { name: /edit/i })).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /delete/i })).not.toBeInTheDocument()
    })
  })

  describe('Navigation', () => {
    it('should link to budget detail page and navigate when clicked', async () => {
      renderWithRouter(<BudgetCard budget={mockBudget} />)

      const link = screen.getByRole('link', { name: /manage budget/i })
      expect(link).toBeInTheDocument()
      expect(link).toHaveAttribute('href', '/budgets/budget-1')

      fireEvent.click(link)

      expect(screen.getByTestId('budget-detail-page')).toBeInTheDocument()
    })
  })

  describe('Income Progress', () => {
    it('renders income progress row when expected_income is set', () => {
      const budgetWithIncome = { ...mockBudget, expected_income: 2000, income_amount: 800 }
      renderWithRouter(<BudgetCard budget={budgetWithIncome} />)

      expect(screen.getByText('Income')).toBeInTheDocument()
      // The income amounts are rendered inside a single span with currency formatting
      expect(screen.getByText('$800.00 of $2,000.00 expected')).toBeInTheDocument()

    })

    it('does not render income progress row when expected_income is 0', () => {
      const budgetNoIncome = { ...mockBudget, expected_income: 0, income_amount: 0 }
      renderWithRouter(<BudgetCard budget={budgetNoIncome} />)

      expect(screen.queryByText('Income')).not.toBeInTheDocument()
    })

    it('does not render income progress row when expected_income is absent', () => {
      // mockBudget has no expected_income field
      renderWithRouter(<BudgetCard budget={mockBudget} />)

      expect(screen.queryByText('Income')).not.toBeInTheDocument()
    })
  })
})
