import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { TransactionBudgetBadge } from './TransactionBudgetBadge'
import type { BudgetCategoryMatch } from '../../utils/transactionBudgetMatcher'
import type { BudgetCategory } from '../../types'

function createBudgetCategory(
  overrides: Partial<BudgetCategory> = {}
): BudgetCategory {
  return {
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
    ...overrides,
  }
}

function createMatch(
  matchReason: 'category' | 'tag' | 'both' = 'category',
  categoryOverrides: Partial<BudgetCategory> = {}
): BudgetCategoryMatch {
  return {
    category: createBudgetCategory(categoryOverrides),
    matchReason,
  }
}

function renderWithRouter(
  ui: React.ReactElement,
  { route = '/' } = {}
) {
  return render(
    <MemoryRouter initialEntries={[route]}>
      {ui}
    </MemoryRouter>
  )
}

describe('TransactionBudgetBadge - rendering', () => {
  it('should not render when match is null', () => {
    const { container } = renderWithRouter(
      <TransactionBudgetBadge match={null} />
    )
    expect(container).toBeEmptyDOMElement()
  })

  it('should not render when match is undefined', () => {
    const { container } = renderWithRouter(
      <TransactionBudgetBadge match={undefined} />
    )
    expect(container).toBeEmptyDOMElement()
  })

  it('should render category name when match provided', () => {
    const match = createMatch('category')
    renderWithRouter(<TransactionBudgetBadge match={match} />)

    // Shows formatted display string for pfc_detailed (no display fields → formatPFCEnum fallback)
    expect(screen.getByText('Food And Drink › Restaurants')).toBeInTheDocument()
  })

  it('should show pfc_primary when pfc_detailed is absent', () => {
    const match = createMatch('category', {
      pfc_primary: 'FOOD_AND_DRINK',
      pfc_detailed: undefined,
    })
    renderWithRouter(<TransactionBudgetBadge match={match} />)

    expect(screen.getByText('Food And Drink')).toBeInTheDocument()
  })

  it('should handle pfc_primary-only category', () => {
    const match = createMatch('category', {
      pfc_primary: 'GENERAL_MERCHANDISE',
      pfc_detailed: undefined,
    })
    renderWithRouter(<TransactionBudgetBadge match={match} />)

    expect(screen.getByText('General Merchandise')).toBeInTheDocument()
  })

  it('should handle missing pfc fields gracefully', () => {
    const match = createMatch('category', {
      category_kind: 'pfc',
      pfc_primary: undefined,
      pfc_detailed: undefined,
    })
    renderWithRouter(<TransactionBudgetBadge match={match} />)

    // Should render fallback text
    expect(screen.getByText('Uncategorized')).toBeInTheDocument()
  })

  it('should apply correct CSS class', () => {
    const match = createMatch('category')
    renderWithRouter(<TransactionBudgetBadge match={match} />)

    const badge = screen.getByRole('link')
    expect(badge).toHaveClass('transaction-budget-badge')
  })
})

describe('TransactionBudgetBadge - match reason icons', () => {
  it('should have title showing category match reason when matchReason is "category"', () => {
    const match = createMatch('category')
    renderWithRouter(<TransactionBudgetBadge match={match} />)

    const badge = screen.getByRole('link')
    expect(badge).toHaveAttribute('title', 'Matched by category')
  })

  it('should have title showing tag match reason when matchReason is "tag"', () => {
    const match = createMatch('tag')
    renderWithRouter(<TransactionBudgetBadge match={match} />)

    const badge = screen.getByRole('link')
    expect(badge).toHaveAttribute('title', 'Matched by tag')
  })

  it('should have title showing both match reason when matchReason is "both"', () => {
    const match = createMatch('both')
    renderWithRouter(<TransactionBudgetBadge match={match} />)

    const badge = screen.getByRole('link')
    expect(badge).toHaveAttribute('title', 'Matched by category and tag')
  })

  it('should render an icon element', () => {
    const match = createMatch('category')
    renderWithRouter(<TransactionBudgetBadge match={match} />)

    const icon = document.querySelector('.transaction-budget-badge__icon')
    expect(icon).toBeInTheDocument()
  })
})

describe('TransactionBudgetBadge - interaction', () => {
  it('should be a link to budget detail page', () => {
    const match = createMatch('category', { budget_id: 'budget-123' })
    renderWithRouter(<TransactionBudgetBadge match={match} />)

    const link = screen.getByRole('link')
    expect(link).toHaveAttribute('href', '/budgets/budget-123')
  })

  it('should have aria-label describing the badge', () => {
    const match = createMatch('category')
    renderWithRouter(<TransactionBudgetBadge match={match} />)

    const badge = screen.getByRole('link')
    expect(badge).toHaveAttribute(
      'aria-label',
      'Budget match: Matched by category'
    )
  })

  it('should be keyboard accessible', async () => {
    const user = userEvent.setup()
    const match = createMatch('category', { budget_id: 'budget-456' })

    render(
      <MemoryRouter initialEntries={['/transactions']}>
        <TransactionBudgetBadge match={match} />
      </MemoryRouter>
    )

    const link = screen.getByRole('link')
    expect(link).toBeVisible()

    await user.tab()
    expect(link).toHaveFocus()
  })
})

describe('TransactionBudgetBadge - edge cases', () => {
  it('should handle very long pfc enum names', () => {
    const match = createMatch('category', {
      category_kind: 'pfc',
      pfc_primary: 'FOOD_AND_DRINK',
      pfc_detailed: 'FOOD_AND_DRINK_COFFEE',
    })
    renderWithRouter(<TransactionBudgetBadge match={match} />)

    const badge = screen.getByRole('link')
    expect(badge).toBeInTheDocument()
    // No display fields → formatPFCEnum fallback
    expect(screen.getByText('Food And Drink › Coffee')).toBeInTheDocument()
  })

  it('should work with custom category', () => {
    const match = createMatch('category', {
      category_kind: 'custom',
      custom_category_id: 'my-custom-uuid',
      pfc_primary: undefined,
      pfc_detailed: undefined,
    })
    renderWithRouter(<TransactionBudgetBadge match={match} />)

    expect(screen.getByText('my-custom-uuid')).toBeInTheDocument()
  })
})
