/**
 * CustomCategoryManager tests (Issue #1203).
 */
import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import {
  mockUseGetUserCategories,
  mockUseGetCategoryUsage,
} from '@/test/mocks/categories-api'

// useCuratedPfcDetailed/getPrimaryLabel are used by the manager + form.
mock.module('@/shared/categories/pfcOptions', () => ({
  useCuratedPfcDetailed: () => ({ data: [{ primary: 'FOOD_AND_DRINK', primaryLabel: 'Food & Drink' }] }),
  getPrimaryLabel: (p: string) => (p === 'FOOD_AND_DRINK' ? 'Food & Drink' : p),
}))

import { CustomCategoryManager } from './CustomCategoryManager'
import type { CustomCategory } from '../types'

function cat(p: Partial<CustomCategory> & { id: string; name: string }): CustomCategory {
  return { user_id: 'u1', usage_count: 0, created_at: '', updated_at: '', ...p }
}

function wrapper() {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        {children}
      </QueryClientProvider>
    )
  }
}

describe('CustomCategoryManager', () => {
  beforeEach(() => {
    mockUseGetUserCategories.mockReturnValue({
      data: [
        cat({ id: 'p', name: 'Food' }),
        cat({ id: 'c', name: 'Fast Food', parent_id: 'p' }),
        cat({ id: 'm', name: 'Meal Prep', parent_pfc_primary: 'FOOD_AND_DRINK' }),
      ],
      isLoading: false,
      error: null,
      refetch: mock(() => Promise.resolve()),
    } as unknown as ReturnType<typeof mockUseGetUserCategories>)
    mockUseGetCategoryUsage.mockReturnValue({
      data: { transaction_count: 2, budget_count: 1, rule_count: 0, child_count: 1 },
      isLoading: false,
      error: null,
    } as unknown as ReturnType<typeof mockUseGetCategoryUsage>)
  })

  it('renders categories grouped by hierarchy', () => {
    render(<CustomCategoryManager />, { wrapper: wrapper() })
    expect(screen.getByText('Food')).toBeTruthy()
    expect(screen.getByText('Fast Food')).toBeTruthy()
    expect(screen.getByText('Meal Prep')).toBeTruthy()
    // PFC group header
    expect(screen.getByText('Food & Drink')).toBeTruthy()
  })

  it('opens the create form when Add category is clicked', async () => {
    render(<CustomCategoryManager />, { wrapper: wrapper() })
    fireEvent.click(screen.getByRole('button', { name: /add category/i }))
    await waitFor(() => {
      expect(screen.getByText('New category')).toBeTruthy()
    })
  })

  it('opens the delete dialog with impact counts', async () => {
    render(<CustomCategoryManager />, { wrapper: wrapper() })
    fireEvent.click(screen.getByRole('button', { name: /delete food$/i }))
    await waitFor(() => {
      expect(screen.getByText(/Delete category/i)).toBeTruthy()
      // Impact counts from the mocked usage
      expect(screen.getByText(/2 transactions/i)).toBeTruthy()
    })
  })
})
