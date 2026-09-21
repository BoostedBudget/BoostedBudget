/**
 * BudgetDetailPage — persisted incomplete-actuals reasons
 * Issue #1685: the reasons the last actuals publish persisted must be surfaced on the page and
 * must survive a reload.
 *
 * TDD RED: written before BudgetDetailPage mounts the notice.
 *
 * The reload case is driven the only way a reload can be: a FRESH mount against the same server
 * payload. `useBudgetDetail` is mocked at the wrapper boundary, so the second render is exactly
 * what a reload produces — the persisted field is read again from the server.
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BudgetDetailPage } from './BudgetDetailPage'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import {
  mockUseBudgetDetail,
  mockUseAvailableCategories,
  mockBudgetDetailRefetch,
} from '@/test/mocks/budget-detail-hooks'
import { mockUseTags } from '@/test/mocks/tags-hooks'
import { mockUseGetBudgetPlanVsActual, mockRefetchBudgetPlan } from '@/test/mocks/budgets-api'
import { mockTrackEvent } from '@/test/mocks/track-event'

const NOTICE = 'budget-actuals-incomplete-notice'
const REASONS = 'budget-actuals-incomplete-reasons'

/** Shapes a financial amount would take if one ever leaked into the notice. */
const CURRENCY_FORMATTED = [
  /\$\s?\d/,
  /\b\d[\d,]*\.\d{2}\b/,
  /\b\d{1,3}(?:,\d{3})+\b/,
  /[€£¥]\s?\d/,
]

function createMockBudget(overrides: Record<string, unknown> = {}) {
  return {
    id: 'budget-1',
    user_id: 'user-1',
    name: 'January 2025 Budget',
    description: 'Monthly household budget',
    amount: 5000,
    expected_income: 5000,
    income_amount: 6000,
    period: 'monthly' as const,
    start_date: '2025-01-01',
    end_date: '2025-01-31',
    is_active: true,
    currency_code: 'USD',
    categories: [
      {
        id: 'cat-1',
        budget_id: 'budget-1',
        category_kind: 'pfc' as const,
        category: ['Food and Drink', 'Groceries'],
        allocated_amount: 500,
        spent_amount: 250,
        tags: [],
        match_logic: 'category_only' as const,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T00:00:00Z',
      },
    ],
    income_sources: [],
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
    ...overrides,
  }
}

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })

  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/budgets/budget-1']}>
        <Routes>
          <Route path="/budgets/:budgetId" element={<BudgetDetailPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  )
}

function mockBudget(budget: ReturnType<typeof createMockBudget>) {
  mockUseBudgetDetail.mockReturnValue({
    budget,
    loading: false,
    error: null,
    refetch: mockBudgetDetailRefetch,
  })
}

describe('BudgetDetailPage incomplete-actuals reasons', () => {
  beforeEach(() => {
    mockTrackEvent.mockClear()
    mockBudgetDetailRefetch.mockClear()
    mockUseBudgetDetail.mockClear()
    mockUseAvailableCategories.mockClear()
    mockUseTags.mockClear()
    mockUseAvailableCategories.mockReturnValue({ categories: [] })
    mockUseGetBudgetPlanVsActual.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
      refetch: mockRefetchBudgetPlan,
    })
    mockBudget(createMockBudget())
  })

  it('renders every persisted reason reported by the server', () => {
    mockBudget(
      createMockBudget({
        actuals_incomplete_reasons: ['overlapping_individual_lines', 'currency_mismatch'],
      })
    )

    renderPage()

    expect(screen.getByTestId(NOTICE)).toBeInTheDocument()
    expect(screen.getByText('overlapping_individual_lines')).toBeInTheDocument()
    expect(screen.getByText('currency_mismatch')).toBeInTheDocument()
  })

  it('renders no notice when the server reports no incomplete reasons', () => {
    mockBudget(createMockBudget({ actuals_incomplete_reasons: [] }))

    renderPage()

    expect(screen.queryByTestId(NOTICE)).toBeNull()
    // Positive control: the page rendered.
    expect(screen.getByText('January 2025 Budget')).toBeInTheDocument()
  })

  it('renders no notice when the field is absent from the response', () => {
    renderPage()

    expect(screen.queryByTestId(NOTICE)).toBeNull()
    expect(screen.getByText('January 2025 Budget')).toBeInTheDocument()
  })

  it('renders the notice again after a reload', () => {
    mockBudget(
      createMockBudget({ actuals_incomplete_reasons: ['overlapping_individual_lines'] })
    )

    const first = renderPage()
    expect(screen.getByTestId(NOTICE)).toBeInTheDocument()
    first.unmount()

    // A reload mounts the page again against the same persisted server state.
    renderPage()
    expect(screen.getByTestId(NOTICE)).toBeInTheDocument()
    expect(screen.getByText('overlapping_individual_lines')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /dismiss/i })).toBeNull()
  })

  it('renders no financial amount in the notice', () => {
    mockBudget(
      createMockBudget({
        actuals_incomplete_reasons: ['currency_mismatch', 'savings_currency_mismatch'],
      })
    )

    renderPage()

    const notice = screen.getByTestId(NOTICE)
    const text = notice.textContent ?? ''
    for (const pattern of CURRENCY_FORMATTED) {
      expect(text).not.toMatch(pattern)
    }
    expect(screen.getByTestId(REASONS)).toHaveAttribute('data-ph-mask')
  })

  it('fires the notice analytics event on display with a bounded count', () => {
    mockBudget(
      createMockBudget({
        actuals_incomplete_reasons: ['overlapping_individual_lines', 'currency_mismatch'],
      })
    )

    renderPage()

    expect(mockTrackEvent).toHaveBeenCalledWith(
      ANALYTICS_EVENTS.BUDGET_ACTUALS_INCOMPLETE_REASONS_VIEWED,
      { reason_count: 2 }
    )
  })

  it('does not fire the notice event when there is nothing to display', () => {
    renderPage()

    expect(mockTrackEvent).not.toHaveBeenCalledWith(
      ANALYTICS_EVENTS.BUDGET_ACTUALS_INCOMPLETE_REASONS_VIEWED,
      expect.anything()
    )
  })
})
