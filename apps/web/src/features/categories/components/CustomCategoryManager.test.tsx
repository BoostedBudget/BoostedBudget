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

// pfcOptions (useCuratedPfcDetailed/getPrimaryLabel) read static data — no mock needed,
// and mocking it would pollute other tests that share the module (mock.module is global).
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

  it('renders a delete control for each category', () => {
    // The delete dialog's content/impact warning is covered by DeleteCategoryDialog.test.tsx;
    // here we only assert the manager exposes a delete affordance per category.
    render(<CustomCategoryManager />, { wrapper: wrapper() })
    expect(screen.getByRole('button', { name: /delete food$/i })).toBeTruthy()
    expect(screen.getByRole('button', { name: /delete fast food$/i })).toBeTruthy()
  })

  it('supports arrow-key roving navigation between category rows', () => {
    render(<CustomCategoryManager />, { wrapper: wrapper() })
    const rows = screen.getAllByRole('listitem')
    // DOM order: Food (parent), Fast Food (child), Meal Prep (PFC group)
    rows[0].focus()
    expect(document.activeElement).toBe(rows[0])
    fireEvent.keyDown(rows[0], { key: 'ArrowDown' })
    expect(document.activeElement).toBe(rows[1])
    fireEvent.keyDown(rows[1], { key: 'ArrowUp' })
    expect(document.activeElement).toBe(rows[0])
  })

  it('opens the edit form when Enter is pressed on a focused row', async () => {
    render(<CustomCategoryManager />, { wrapper: wrapper() })
    const rows = screen.getAllByRole('listitem')
    rows[0].focus()
    fireEvent.keyDown(rows[0], { key: 'Enter' })
    await waitFor(() => expect(screen.getByText('Edit category')).toBeTruthy())
  })

  it('opens the delete dialog when Delete is pressed on a focused row', async () => {
    render(<CustomCategoryManager />, { wrapper: wrapper() })
    const rows = screen.getAllByRole('listitem')
    rows[0].focus()
    fireEvent.keyDown(rows[0], { key: 'Delete' })
    await waitFor(() => expect(screen.getByRole('alertdialog')).toBeTruthy())
  })
})
