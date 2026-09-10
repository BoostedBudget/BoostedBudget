import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { BrowserRouter } from 'react-router'
import type { BudgetCategory } from '../../types'
import { MATCH_LOGIC_LABELS } from '@boostedbudget/shared-types/matching'
import { scanForUnmaskedPII, type PIISentinel } from '@/test/pii-masking'

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
    category_kind: 'pfc',
    pfc_primary: 'FOOD_AND_DRINK',
    pfc_detailed: 'FOOD_AND_DRINK_RESTAURANTS',
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

      // No display fields — falls back to formatPFCEnum
      expect(screen.getByText('Food And Drink › Restaurants')).toBeInTheDocument()
    })

    it('should render single-level category path correctly', () => {
      const category = createMockCategory({ pfc_primary: 'GENERAL_MERCHANDISE', pfc_detailed: undefined })
      render(<BudgetCategoryCard category={category} />)

      // No display fields — falls back to formatPFCEnum
      expect(screen.getByText('General Merchandise')).toBeInTheDocument()
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

    it('shows an "Earmarked" indicator for the child (sub-allocation) variant (#1203)', () => {
      render(<BudgetCategoryCard category={createMockCategory()} variant="child" />)
      expect(screen.getByText(/earmarked/i)).toBeInTheDocument()
    })

    it('includes the parent name in the earmark label when provided (#1203)', () => {
      render(
        <BudgetCategoryCard category={createMockCategory()} variant="child" parentLabel="Food" />
      )
      expect(screen.getByText(/earmarked from food/i)).toBeInTheDocument()
    })

    it('does not show an earmark indicator for standalone or parent variants', () => {
      const { rerender } = render(
        <BudgetCategoryCard category={createMockCategory()} variant="standalone" />
      )
      expect(screen.queryByText(/earmarked/i)).toBeNull()
      rerender(<BudgetCategoryCard category={createMockCategory()} variant="parent" />)
      expect(screen.queryByText(/earmarked/i)).toBeNull()
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
      // No display fields — heading shows formatPFCEnum fallback
      expect(screen.getByRole('heading')).toHaveTextContent('Food And Drink › Restaurants')
      expect(screen.getByRole('button', { name: /edit/i })).toHaveAttribute('aria-label')
      expect(screen.getByRole('button', { name: /delete/i })).toHaveAttribute('aria-label')
    })
  })

  describe('Variant prop — visual hierarchy', () => {
    it('defaults to standalone variant when no variant is provided', () => {
      const category = createMockCategory()
      render(<BudgetCategoryCard category={category} />)
      const card = screen.getByRole('article')
      // standalone: full rounded corners, no visual connection
      expect(card.className).toContain('rounded-lg')
      expect(card.className).not.toContain('rounded-b-none')
      expect(card.className).not.toContain('rounded-t-none')
    })

    it('parent variant removes bottom rounding and bottom border to connect to children', () => {
      const category = createMockCategory()
      render(<BudgetCategoryCard category={category} variant="parent" />)
      const card = screen.getByRole('article')
      expect(card.className).toContain('rounded-b-none')
      expect(card.className).toContain('border-b-0')
    })

    it('child variant applies left indent, top-none rounding, and left border accent', () => {
      const category = createMockCategory({
        pfc_primary: 'FOOD_AND_DRINK',
        pfc_detailed: 'FOOD_AND_DRINK_RESTAURANTS',
        pfc_primary_display: 'Food & Drink',
        pfc_detailed_display: 'Food & Drink › Restaurants',
      })
      render(<BudgetCategoryCard category={category} variant="child" />)
      const card = screen.getByRole('article')
      expect(card.className).toContain('ml-6')
      expect(card.className).toContain('rounded-t-none')
      expect(card.className).toContain('border-l-2')
    })

    it('child variant strips primary prefix from display label', () => {
      const category = createMockCategory({
        pfc_primary: 'FOOD_AND_DRINK',
        pfc_detailed: 'FOOD_AND_DRINK_RESTAURANTS',
        pfc_primary_display: 'Food & Drink',
        pfc_detailed_display: 'Food & Drink › Restaurants',
      })
      render(<BudgetCategoryCard category={category} variant="child" />)
      // Should display only 'Restaurants', not 'Food & Drink › Restaurants'
      expect(screen.getByRole('heading')).toHaveTextContent('Restaurants')
      expect(screen.queryByText('Food & Drink › Restaurants')).not.toBeInTheDocument()
    })

    it('child variant strips primary prefix using › separator', () => {
      const category = createMockCategory({
        pfc_primary: 'FOOD_AND_DRINK',
        pfc_detailed: 'FOOD_AND_DRINK_FAST_FOOD',
        pfc_primary_display: 'Food & Drink',
        pfc_detailed_display: 'Food & Drink › Fast Food',
      })
      render(<BudgetCategoryCard category={category} variant="child" />)
      expect(screen.getByRole('heading')).toHaveTextContent('Fast Food')
    })

    it('child variant shows full label when label has no › separator', () => {
      // Fallback: if formatBudgetCategoryLabel returns a flat string with no separator
      const category = createMockCategory({
        pfc_primary: 'FOOD_AND_DRINK',
        pfc_detailed: 'FOOD_AND_DRINK_RESTAURANTS',
        // no display fields — falls back to formatPFCEnum which uses ›
        pfc_primary_display: undefined,
        pfc_detailed_display: undefined,
      })
      render(<BudgetCategoryCard category={category} variant="child" />)
      // formatPFCEnum('FOOD_AND_DRINK_RESTAURANTS') → 'Food And Drink › Restaurants'
      // After stripping: 'Restaurants'
      expect(screen.getByRole('heading')).toHaveTextContent('Restaurants')
    })

    it('standalone variant renders normally (no strip)', () => {
      const category = createMockCategory({
        pfc_primary: 'FOOD_AND_DRINK',
        pfc_detailed: 'FOOD_AND_DRINK_RESTAURANTS',
        pfc_primary_display: 'Food & Drink',
        pfc_detailed_display: 'Food & Drink › Restaurants',
      })
      render(<BudgetCategoryCard category={category} variant="standalone" />)
      // standalone should NOT strip the prefix
      expect(screen.getByRole('heading')).toHaveTextContent('Food & Drink › Restaurants')
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

  describe('customCategoryName prop (Gap 2)', () => {
    it('displays customCategoryName when category_kind is custom', () => {
      const category = createMockCategory({
        category_kind: 'custom',
        pfc_primary: undefined,
        pfc_detailed: undefined,
        custom_category_id: 'some-uuid',
      })
      render(<BudgetCategoryCard category={category} customCategoryName="My Restaurants" />)
      expect(screen.getByRole('heading')).toHaveTextContent('My Restaurants')
    })

    it('does not show UUID in heading when customCategoryName is provided', () => {
      const category = createMockCategory({
        category_kind: 'custom',
        pfc_primary: undefined,
        pfc_detailed: undefined,
        custom_category_id: 'a1b2c3d4-e5f6-7890-abcd-ef1234567890',
      })
      render(<BudgetCategoryCard category={category} customCategoryName="Dining Out" />)
      expect(screen.getByRole('heading')).not.toHaveTextContent('a1b2c3d4')
      expect(screen.getByRole('heading')).toHaveTextContent('Dining Out')
    })

    it('falls back gracefully when customCategoryName is undefined for custom category', () => {
      const category = createMockCategory({
        category_kind: 'custom',
        pfc_primary: undefined,
        pfc_detailed: undefined,
        custom_category_id: 'some-uuid',
      })
      // No customCategoryName prop — should render without crashing
      render(<BudgetCategoryCard category={category} />)
      expect(screen.getByRole('article')).toBeInTheDocument()
    })

    it('strips primary prefix from customCategoryName in child variant', () => {
      const category = createMockCategory({
        category_kind: 'custom',
        pfc_primary: undefined,
        pfc_detailed: undefined,
        custom_category_id: 'some-uuid',
      })
      // customCategoryName without a separator — no stripping should occur
      render(
        <BudgetCategoryCard
          category={category}
          customCategoryName="Dining Out"
          variant="child"
        />
      )
      expect(screen.getByRole('heading')).toHaveTextContent('Dining Out')
    })
  })

  describe('Savings bucket variant (#1300)', () => {
    const createSavingsCategory = (overrides?: Partial<BudgetCategory>): BudgetCategory =>
      createMockCategory({
        category_kind: 'savings',
        pfc_primary: undefined,
        pfc_detailed: undefined,
        savings_goal_id: 'goal-1',
        savings_goal_name: 'House Down Payment',
        allocated_amount: 200,
        spent_amount: 50,
        ...overrides,
      })

    it('renders the goal name as the heading', () => {
      render(<BudgetCategoryCard category={createSavingsCategory()} />)
      expect(screen.getByRole('heading')).toHaveTextContent('House Down Payment')
    })

    it('falls back to "Savings Goal" when savings_goal_name is absent', () => {
      render(
        <BudgetCategoryCard
          category={createSavingsCategory({ savings_goal_name: undefined })}
        />
      )
      expect(screen.getByRole('heading')).toHaveTextContent('Savings Goal')
    })

    it('shows "Savings Bucket" badge instead of match logic label', () => {
      render(<BudgetCategoryCard category={createSavingsCategory()} />)
      expect(screen.getByText(/savings bucket/i)).toBeTruthy()
    })

    it('renders allocated and spent amounts', () => {
      render(<BudgetCategoryCard category={createSavingsCategory()} />)
      expect(screen.getByText('$200.00')).toBeTruthy()
      expect(screen.getByText('$50.00')).toBeTruthy()
    })
  })

  describe('Parent remainder and over-allocation warning (#1630 Task 4)', () => {
    const utilitiesParent = (): BudgetCategory =>
      createMockCategory({
        id: 'utilities',
        category_kind: 'pfc',
        pfc_primary: 'RENT_AND_UTILITIES',
        pfc_detailed: undefined,
        allocated_amount: 500,
        spent_amount: 480,
      })

    const electricChild = (overrides?: Partial<BudgetCategory>): BudgetCategory =>
      createMockCategory({
        id: 'electric',
        category_kind: 'pfc',
        pfc_primary: 'RENT_AND_UTILITIES',
        pfc_detailed: 'RENT_AND_UTILITIES_ELECTRIC',
        allocated_amount: 300,
        spent_amount: 300,
        ...overrides,
      })

    const cableChild = (overrides?: Partial<BudgetCategory>): BudgetCategory =>
      createMockCategory({
        id: 'cable',
        category_kind: 'pfc',
        pfc_primary: 'RENT_AND_UTILITIES',
        pfc_detailed: 'RENT_AND_UTILITIES_CABLE',
        allocated_amount: 200,
        spent_amount: 150,
        ...overrides,
      })

    it("shows the parent's own unallocated remainder, distinct from its children's allocations", () => {
      render(
        <BudgetCategoryCard
          category={utilitiesParent()}
          variant="parent"
          childCategories={[electricChild(), cableChild()]}
        />
      )
      // $500 utilities contains $300 electric + $200 cable, leaving $0 remainder.
      expect(screen.getByText(/remaining after children/i)).toBeInTheDocument()
      expect(screen.getByText('$0.00')).toBeInTheDocument()
    })

    it("shows the parent's own direct spending, distinct from its children's spend", () => {
      render(
        <BudgetCategoryCard
          category={utilitiesParent()}
          variant="parent"
          childCategories={[electricChild(), cableChild()]}
        />
      )
      // parent's own rolled spend ($480) already contains electric+cable's $450 — the water
      // bill itself only cost $30.
      expect(screen.getByText(/direct spending/i)).toBeInTheDocument()
      expect(screen.getByText('$30.00')).toBeInTheDocument()
    })

    it('renders a NEGATIVE remainder (children over-allocate the parent) without blocking anything', () => {
      render(
        <BudgetCategoryCard
          category={utilitiesParent()}
          variant="parent"
          childCategories={[
            electricChild({ allocated_amount: 400 }),
            cableChild({ allocated_amount: 200 }),
          ]}
        />
      )
      // $600 of children under a $500 parent -> remainder is -$100.
      expect(screen.getByText('-$100.00')).toBeInTheDocument()
    })

    it('shows a visible, non-blocking warning when children over-allocate the parent', () => {
      render(
        <BudgetCategoryCard
          category={utilitiesParent()}
          variant="parent"
          childCategories={[
            electricChild({ allocated_amount: 400 }),
            cableChild({ allocated_amount: 200 }),
          ]}
        />
      )
      expect(screen.getByRole('status')).toHaveTextContent(/allocate.*more than/i)
      // No disabled controls anywhere — warn, never block (#1630 Task 4).
      expect(screen.queryByRole('button', { name: /disabled/i })).not.toBeInTheDocument()
    })

    it('shows no over-allocation warning when children are within the parent', () => {
      render(
        <BudgetCategoryCard
          category={utilitiesParent()}
          variant="parent"
          childCategories={[electricChild(), cableChild()]}
        />
      )
      expect(screen.queryByRole('status')).not.toBeInTheDocument()
    })

    it('does not render a remainder/direct-spend section for non-parent variants', () => {
      render(<BudgetCategoryCard category={createMockCategory()} variant="standalone" />)
      expect(screen.queryByText(/remaining after children/i)).not.toBeInTheDocument()
      expect(screen.queryByText(/direct spending/i)).not.toBeInTheDocument()
    })

    it('does not render a remainder section for a parent with no children passed', () => {
      render(<BudgetCategoryCard category={utilitiesParent()} variant="parent" />)
      expect(screen.queryByText(/remaining after children/i)).not.toBeInTheDocument()
    })

    it('masks every money figure in the parent summary and warning, never in an attribute', () => {
      render(
        <BudgetCategoryCard
          category={utilitiesParent()}
          variant="parent"
          childCategories={[
            electricChild({ allocated_amount: 400 }),
            cableChild({ allocated_amount: 200 }),
          ]}
        />
      )
      const card = screen.getByRole('article')
      const sentinels: PIISentinel[] = [
        { value: '-$100.00', fieldDescription: 'parent allocation remainder' },
        { value: '$500.00', fieldDescription: "parent's own allocated_amount in warning copy" },
      ]
      expect(scanForUnmaskedPII(card, sentinels)).toEqual([])
    })
  })
})
