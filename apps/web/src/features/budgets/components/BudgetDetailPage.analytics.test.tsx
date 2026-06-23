/**
 * BudgetDetailPage Analytics Tests
 *
 * Issue #1200: Multi-Level Category Hierarchy
 *
 * Dedicated test file for budget category CRUD analytics events.
 * All hooks mocked via bunfig.toml preloads (budgets-api.ts, budget-detail-hooks.ts,
 * track-event.ts, tags-hooks.ts).
 *
 * UI interactions:
 * - Add: Click "Add Category" → PFC tree picker → select primary → fill amount → submit
 * - Update: Click "Edit category" → fill amount (category pre-filled/locked) → submit
 * - Delete: Click "Delete category" → confirm alertdialog
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { mockTrackEvent } from '@/test/mocks/track-event'

import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BudgetDetailPage } from './BudgetDetailPage'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import {
  mockUseBudgetDetail,
  mockBudgetDetailRefetch,
  mockUseAvailableCategories,
} from '@/test/mocks/budget-detail-hooks'
import { mockUseTags } from '@/test/mocks/tags-hooks'
import {
  mockUseAddBudgetCategory,
  mockAddBudgetCategoryMutateAsync,
  mockUseUpdateBudgetCategory,
  mockUpdateBudgetCategoryMutateAsync,
  mockUseDeleteBudgetCategory,
  mockDeleteBudgetCategoryMutateAsync,
} from '@/test/mocks/budgets-api'
import type { BudgetCategory } from '../types'

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/budgets/budget-1']}>
          <Routes>
            <Route path="/budgets/:budgetId" element={<>{children}</>} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>
    )
  }
}

function renderPage() {
  const Wrapper = createWrapper()
  return render(<BudgetDetailPage />, { wrapper: Wrapper })
}

const mockCategory: BudgetCategory = {
  id: 'cat-1',
  budget_id: 'budget-1',
  category_kind: 'pfc',
  pfc_primary: 'FOOD_AND_DRINK',
  pfc_detailed: 'FOOD_AND_DRINK_RESTAURANTS',
  allocated_amount: 500,
  spent_amount: 250,
  tags: [],
  match_logic: 'category_only',
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-01T00:00:00Z',
}

const mockBudget = {
  id: 'budget-1',
  user_id: 'user-1',
  name: 'Test Budget',
  description: '',
  amount: 5000,
  expected_income: 5000,
  income_amount: 0,
  period: 'monthly',
  start_date: '2025-01-01',
  end_date: '2025-01-31',
  is_active: true,
  currency_code: 'USD',
  categories: [mockCategory],
  income_sources: [],
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-01T00:00:00Z',
}

describe('Budget category CRUD analytics', () => {
  beforeEach(() => {
    mockTrackEvent.mockClear()
    mockBudgetDetailRefetch.mockClear()
    mockUseBudgetDetail.mockClear()
    mockUseAvailableCategories.mockClear()
    mockUseTags.mockClear()
    mockUseAddBudgetCategory.mockClear()
    mockAddBudgetCategoryMutateAsync.mockClear()
    mockUseUpdateBudgetCategory.mockClear()
    mockUpdateBudgetCategoryMutateAsync.mockClear()
    mockUseDeleteBudgetCategory.mockClear()
    mockDeleteBudgetCategoryMutateAsync.mockClear()

    mockUseBudgetDetail.mockReturnValue({
      budget: mockBudget,
      loading: false,
      error: null,
      refetch: mockBudgetDetailRefetch,
    })
    mockUseAvailableCategories.mockReturnValue({ categories: [] })
    mockUseTags.mockReturnValue({ tags: [] })
    mockUseAddBudgetCategory.mockReturnValue({ mutateAsync: mockAddBudgetCategoryMutateAsync })
    mockUpdateBudgetCategoryMutateAsync.mockResolvedValue({ ...mockCategory, allocated_amount: 200 })
    mockUseUpdateBudgetCategory.mockReturnValue({ mutateAsync: mockUpdateBudgetCategoryMutateAsync })
    mockDeleteBudgetCategoryMutateAsync.mockResolvedValue(undefined)
    mockUseDeleteBudgetCategory.mockReturnValue({ mutateAsync: mockDeleteBudgetCategoryMutateAsync })
    mockAddBudgetCategoryMutateAsync.mockResolvedValue({ ...mockCategory, id: 'cat-new' })
    mockBudgetDetailRefetch.mockResolvedValue(undefined)
  })

  it('fires BUDGET_CATEGORY_ADDED when a category is added', async () => {
    const user = userEvent.setup()
    renderPage()

    // Open the add category form
    await user.click(screen.getByRole('button', { name: /add.*category/i }))

    // Wait for the dialog to open
    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })

    // Open the PFC tree picker (the trigger button has aria-label "Select PFC category")
    const treeSelectBtn = screen.getByRole('button', { name: /select pfc category/i })
    fireEvent.click(treeSelectBtn)

    // Select a primary category by clicking the FOOD_AND_DRINK row
    const primaryRow = await screen.findByTestId('primary-row-FOOD_AND_DRINK')
    fireEvent.click(primaryRow)

    // Fill in the allocated amount
    const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
    fireEvent.change(amountInput, { target: { value: '100' } })

    // Submit the form
    const form = document.querySelector('form')
    fireEvent.submit(form!)

    await waitFor(() => {
      expect(mockTrackEvent).toHaveBeenCalledWith(ANALYTICS_EVENTS.BUDGET_CATEGORY_ADDED)
    })
  })

  it('fires BUDGET_CATEGORY_UPDATED (not BUDGET_EDITED) when a category is updated', async () => {
    const user = userEvent.setup()
    renderPage()

    // Click the Edit button on the existing category card
    await user.click(screen.getByRole('button', { name: /^Edit category$/i }))

    // Wait for the edit dialog to open with the category pre-filled
    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
      expect(screen.getByRole('heading', { name: /edit category/i })).toBeInTheDocument()
    })

    // The category is pre-filled; update the allocated amount
    const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
    fireEvent.change(amountInput, { target: { value: '200' } })

    // Submit the form
    const form = document.querySelector('form')
    fireEvent.submit(form!)

    await waitFor(() => {
      expect(mockTrackEvent).toHaveBeenCalledWith(ANALYTICS_EVENTS.BUDGET_CATEGORY_UPDATED)
    })
    // Ensure the old (wrong) event is NOT fired
    expect(mockTrackEvent).not.toHaveBeenCalledWith('budget_edited')
  })

  it('fires BUDGET_CATEGORY_DELETED when a category is deleted', async () => {
    const user = userEvent.setup()
    renderPage()

    // Click the Delete button on the category card
    await user.click(screen.getByRole('button', { name: /^Delete category$/i }))

    // Wait for the confirmation dialog
    await waitFor(() => {
      expect(screen.getByRole('alertdialog')).toBeInTheDocument()
    })

    // Click the Confirm delete button
    await user.click(screen.getByRole('button', { name: /^delete$/i }))

    await waitFor(() => {
      expect(mockTrackEvent).toHaveBeenCalledWith(ANALYTICS_EVENTS.BUDGET_CATEGORY_DELETED)
    })
  })
})
