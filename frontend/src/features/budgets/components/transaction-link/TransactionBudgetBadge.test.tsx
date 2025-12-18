/**
 * TransactionBudgetBadge Component Tests
 * Issue #499: Transaction-Budget Linking UI
 *
 * TDD RED Phase: Write comprehensive tests before implementation
 */

import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { TransactionBudgetBadge } from './TransactionBudgetBadge'
import type { BudgetCategoryMatch } from '../../utils/transactionBudgetMatcher'
import type { BudgetCategory } from '../../types'

// =============================================================================
// Test Helpers
// =============================================================================

function createBudgetCategory(
  overrides: Partial<BudgetCategory> = {}
): BudgetCategory {
  return {
    id: 'cat-1',
    budget_id: 'budget-1',
    category: ['Food and Drink', 'Restaurants'],
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

// =============================================================================
// Rendering Tests
// =============================================================================

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

    // Should show primary category
    expect(screen.getByText('Food and Drink')).toBeInTheDocument()
  })

  it('should show primary category only (not full path)', () => {
    const match = createMatch('category', {
      category: ['Food and Drink', 'Restaurants', 'Fast Food'],
    })
    renderWithRouter(<TransactionBudgetBadge match={match} />)

    // Should show only primary category
    expect(screen.getByText('Food and Drink')).toBeInTheDocument()
    // Should NOT show full path
    expect(screen.queryByText(/Restaurants/)).not.toBeInTheDocument()
  })

  it('should handle single-element category', () => {
    const match = createMatch('category', {
      category: ['Shopping'],
    })
    renderWithRouter(<TransactionBudgetBadge match={match} />)

    expect(screen.getByText('Shopping')).toBeInTheDocument()
  })

  it('should handle empty category array gracefully', () => {
    const match = createMatch('category', {
      category: [],
    })
    renderWithRouter(<TransactionBudgetBadge match={match} />)

    // Should render fallback text
    expect(screen.getByText('Budget')).toBeInTheDocument()
  })

  it('should apply correct CSS class', () => {
    const match = createMatch('category')
    renderWithRouter(<TransactionBudgetBadge match={match} />)

    const badge = screen.getByRole('link')
    expect(badge).toHaveClass('transaction-budget-badge')
  })
})

// =============================================================================
// Match Reason Icons Tests
// =============================================================================

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

// =============================================================================
// Interaction Tests
// =============================================================================

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
      'Budget: Food and Drink (Matched by category)'
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

    // Focus should work
    await user.tab()
    expect(link).toHaveFocus()
  })
})

// =============================================================================
// Edge Cases
// =============================================================================

describe('TransactionBudgetBadge - edge cases', () => {
  it('should handle very long category names with truncation', () => {
    const match = createMatch('category', {
      category: [
        'Very Long Category Name That Should Be Truncated',
        'Subcategory',
      ],
    })
    renderWithRouter(<TransactionBudgetBadge match={match} />)

    const badge = screen.getByRole('link')
    // Badge should still render (CSS handles truncation)
    expect(badge).toBeInTheDocument()
    expect(
      screen.getByText('Very Long Category Name That Should Be Truncated')
    ).toBeInTheDocument()
  })

  it('should work with special characters in category name', () => {
    const match = createMatch('category', {
      category: ['Food & Drink', "Sam's Club"],
    })
    renderWithRouter(<TransactionBudgetBadge match={match} />)

    expect(screen.getByText('Food & Drink')).toBeInTheDocument()
  })
})
