import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { BrowserRouter } from 'react-router'
import type { BudgetCategory } from '../../types'
import { MATCH_LOGIC_LABELS } from '@/shared/types/matching'

mock.module('../BudgetProgressBar', () => ({
  BudgetProgressBar: ({ spent, total }: { spent: number; total: number }) => (
    <div data-testid="budget-progress-bar" data-spent={spent} data-total={total}>
      Progress: {spent}/{total}
    </div>
  ),
}))

mock.module('../transaction-link/BudgetTransactionList', () => ({
  BudgetTransactionList: ({ budgetId, categoryId }: { budgetId: string; categoryId: string }) => (
    <div data-testid="budget-transaction-list" data-budget-id={budgetId} data-category-id={categoryId}>
      Transaction List
    </div>
  ),
}))

import { BudgetCategoryCard } from './BudgetCategoryCard'

const mockOnEdit = mock(() => {})
const mockOnDelete = mock(() => {})

describe('BudgetCategoryCard', () => {
  const createMockCategory = (overrides?: Partial<BudgetCategory>): BudgetCategory => ({
    id: 'cat-1',
    budget_id: 'budget-1',
    category: ['Food and Drink', 'Restaurants'],
    allocated_amount: 500,
    spent_amount: 250,
    tags: [],
    match_logic: 'category_only',
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    ...overrides,
  })

  beforeEach(() => {
    mockOnEdit.mockClear()
    mockOnDelete.mockClear()
  })

  describe('Rendering', () => {
    it('should render category path as hierarchical string', () => {
      const category = createMockCategory()
      render(<BudgetCategoryCard category={category} />)

      expect(screen.getByText('Food and Drink > Restaurants')).toBeInTheDocument()
    })

    it('should render single-level category path correctly', () => {
      const category = createMockCategory({ category: ['Shopping'] })
      render(<BudgetCategoryCard category={category} />)

      expect(screen.getByText('Shopping')).toBeInTheDocument()
    })

    it('should render allocated amount formatted as currency', () => {
      const category = createMockCategory({ allocated_amount: 1234.56 })
      render(<BudgetCategoryCard category={category} />)

      expect(screen.getByText('$1,234.56')).toBeInTheDocument()
    })

    it('should render spent amount with progress bar', () => {
      const category = createMockCategory({ spent_amount: 250, allocated_amount: 500 })
      render(<BudgetCategoryCard category={category} />)

      const progressBar = screen.getByTestId('budget-progress-bar')
      expect(progressBar).toHaveAttribute('data-spent', '250')
      expect(progressBar).toHaveAttribute('data-total', '500')
    })

    it('should render match logic badges correctly', () => {
      render(<BudgetCategoryCard category={createMockCategory({ match_logic: 'tag_only' })} />)
      expect(screen.getByText(MATCH_LOGIC_LABELS.tag_only)).toBeInTheDocument()
    })

    it('should render tags list when tags are present', () => {
      const category = createMockCategory({ tags: ['groceries', 'essential'] })
      render(<BudgetCategoryCard category={category} />)

      expect(screen.getByText('groceries')).toBeInTheDocument()
      expect(screen.getByText('essential')).toBeInTheDocument()
    })

    it('should not render tags section when tags array is empty', () => {
      const category = createMockCategory({ tags: [] })
      render(<BudgetCategoryCard category={category} />)

      expect(screen.queryByText('Tags:')).not.toBeInTheDocument()
    })
  })

  describe('Actions', () => {
    it('should call onEdit callback when edit button is clicked', async () => {
      const user = userEvent.setup({ delay: null })
      const category = createMockCategory()
      render(<BudgetCategoryCard category={category} onEdit={mockOnEdit} />)

      await user.click(screen.getByRole('button', { name: /edit/i }))

      expect(mockOnEdit).toHaveBeenCalledTimes(1)
    })

    it('should call onDelete callback when delete button is clicked', async () => {
      const user = userEvent.setup({ delay: null })
      const category = createMockCategory()
      render(<BudgetCategoryCard category={category} onDelete={mockOnDelete} />)

      await user.click(screen.getByRole('button', { name: /delete/i }))

      expect(mockOnDelete).toHaveBeenCalledTimes(1)
    })

    it('should hide buttons when callbacks not provided', () => {
      const category = createMockCategory()
      render(<BudgetCategoryCard category={category} />)

      expect(screen.queryByRole('button', { name: /edit/i })).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /delete/i })).not.toBeInTheDocument()
    })
  })

  describe('States', () => {
    it('should disable action buttons when disabled=true', () => {
      const category = createMockCategory()
      render(
        <BudgetCategoryCard
          category={category}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
          disabled={true}
        />
      )

      expect(screen.getByRole('button', { name: /edit/i })).toBeDisabled()
      expect(screen.getByRole('button', { name: /delete/i })).toBeDisabled()
    })

    it('should show over-budget warning when spent exceeds allocated', () => {
      const category = createMockCategory({ spent_amount: 600, allocated_amount: 500 })
      render(<BudgetCategoryCard category={category} />)

      const card = screen.getByRole('article')
      expect(card.className).toContain('over-budget')
    })

    it('should not show over-budget warning when under budget', () => {
      const category = createMockCategory({ spent_amount: 250, allocated_amount: 500 })
      render(<BudgetCategoryCard category={category} />)

      const card = screen.getByRole('article')
      expect(card.className).not.toContain('over-budget')
    })
  })

  describe('Accessibility', () => {
    it('should have proper semantic structure', () => {
      const category = createMockCategory()
      render(
        <BudgetCategoryCard
          category={category}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      expect(screen.getByRole('article')).toBeInTheDocument()
      expect(screen.getByRole('heading')).toHaveTextContent('Food and Drink > Restaurants')
      expect(screen.getByRole('button', { name: /edit/i })).toHaveAttribute('aria-label')
      expect(screen.getByRole('button', { name: /delete/i })).toHaveAttribute('aria-label')
    })
  })

  describe('Collapsible Transactions', () => {
    it('should render View Transactions button when budgetId is provided', () => {
      const category = createMockCategory()
      render(
        <BrowserRouter>
          <BudgetCategoryCard category={category} budgetId="budget-1" />
        </BrowserRouter>
      )

      expect(screen.getByRole('button', { name: /view transactions/i })).toBeInTheDocument()
    })

    it('should not render View Transactions button when budgetId is not provided', () => {
      const category = createMockCategory()
      render(<BudgetCategoryCard category={category} />)

      expect(screen.queryByRole('button', { name: /view transactions/i })).not.toBeInTheDocument()
    })

    it('should toggle transaction list visibility', async () => {
      const user = userEvent.setup({ delay: null })
      const category = createMockCategory()
      render(
        <BrowserRouter>
          <BudgetCategoryCard category={category} budgetId="budget-1" />
        </BrowserRouter>
      )

      expect(screen.queryByTestId('budget-transaction-list')).not.toBeInTheDocument()

      await user.click(screen.getByRole('button', { name: /view transactions/i }))
      expect(screen.getByTestId('budget-transaction-list')).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /hide transactions/i })).toBeInTheDocument()

      await user.click(screen.getByRole('button', { name: /hide transactions/i }))
      expect(screen.queryByTestId('budget-transaction-list')).not.toBeInTheDocument()
    })

    it('should pass correct props to BudgetTransactionList', async () => {
      const user = userEvent.setup({ delay: null })
      const category = createMockCategory({ id: 'my-cat-id' })
      render(
        <BrowserRouter>
          <BudgetCategoryCard category={category} budgetId="my-budget-id" />
        </BrowserRouter>
      )

      await user.click(screen.getByRole('button', { name: /view transactions/i }))

      const list = screen.getByTestId('budget-transaction-list')
      expect(list).toHaveAttribute('data-budget-id', 'my-budget-id')
      expect(list).toHaveAttribute('data-category-id', 'my-cat-id')
    })

    it('should have aria-expanded attribute on toggle button', async () => {
      const user = userEvent.setup({ delay: null })
      const category = createMockCategory()
      render(
        <BrowserRouter>
          <BudgetCategoryCard category={category} budgetId="budget-1" />
        </BrowserRouter>
      )

      const button = screen.getByRole('button', { name: /view transactions/i })
      expect(button).toHaveAttribute('aria-expanded', 'false')

      await user.click(button)

      const expandedButton = screen.getByRole('button', { name: /hide transactions/i })
      expect(expandedButton).toHaveAttribute('aria-expanded', 'true')
    })
  })
})
