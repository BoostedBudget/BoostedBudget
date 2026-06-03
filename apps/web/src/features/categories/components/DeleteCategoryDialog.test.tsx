/**
 * DeleteCategoryDialog tests (Issue #1203).
 */
import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { mockUseGetCategoryUsage } from '@/test/mocks/categories-api'
import { DeleteCategoryDialog } from './DeleteCategoryDialog'
import type { CustomCategory } from '../types'

const category: CustomCategory = {
  id: 'cat-1',
  user_id: 'u1',
  name: 'Coffee',
  usage_count: 0,
  created_at: '',
  updated_at: '',
}

describe('DeleteCategoryDialog', () => {
  beforeEach(() => {
    mockUseGetCategoryUsage.mockReturnValue({
      data: { transaction_count: 2, budget_count: 1, rule_count: 0, child_count: 1 },
      isLoading: false,
      error: null,
    } as unknown as ReturnType<typeof mockUseGetCategoryUsage>)
  })

  it('renders the impact counts in the warning', () => {
    render(
      <DeleteCategoryDialog
        category={category}
        open
        onOpenChange={() => {}}
        onConfirm={() => {}}
      />
    )
    expect(screen.getByText(/2 transactions/i)).toBeTruthy()
    expect(screen.getByText(/1 budget/i)).toBeTruthy()
    expect(screen.getByText(/1 sub-category/i)).toBeTruthy()
  })

  it('falls back to a generic warning when usage fails to load', () => {
    mockUseGetCategoryUsage.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error('boom'),
    } as unknown as ReturnType<typeof mockUseGetCategoryUsage>)

    render(
      <DeleteCategoryDialog category={category} open onOpenChange={() => {}} onConfirm={() => {}} />
    )
    expect(screen.getByText(/revert to their bank category/i)).toBeTruthy()
  })
})
