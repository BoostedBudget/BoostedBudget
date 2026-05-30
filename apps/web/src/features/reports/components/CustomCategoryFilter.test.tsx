/**
 * CustomCategoryFilter tests (Issue #1203).
 */
import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { mockUseGetUserCategories } from '@/test/mocks/categories-api'
import { CustomCategoryFilter } from './CustomCategoryFilter'
import type { CustomCategory } from '@boostedbudget/api-client/models'

function setCategories(data: unknown) {
  mockUseGetUserCategories.mockReturnValue({
    data,
    isLoading: false,
    error: null,
    refetch: mock(() => Promise.resolve()),
  } as unknown as ReturnType<typeof mockUseGetUserCategories>)
}

const cats = [
  { id: 'p', name: 'Food', user_id: 'u1', usage_count: 0, created_at: '', updated_at: '' },
  { id: 'c', name: 'Fast Food', parent_id: 'p', user_id: 'u1', usage_count: 0, created_at: '', updated_at: '' },
] as CustomCategory[]

describe('CustomCategoryFilter', () => {
  beforeEach(() => setCategories(cats))

  it('renders a toggle chip per custom category, parent before child', () => {
    render(<CustomCategoryFilter selectedIds={[]} onChange={() => {}} />)
    const food = screen.getByRole('button', { name: /^Food$/i })
    const fast = screen.getByRole('button', { name: /Fast Food/i })
    expect(food).toBeTruthy()
    expect(fast).toBeTruthy()
    expect(food.compareDocumentPosition(fast) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('toggles a category id on click (add then remove)', () => {
    const onChange = mock(() => {})
    const { rerender } = render(<CustomCategoryFilter selectedIds={[]} onChange={onChange} />)
    fireEvent.click(screen.getByRole('button', { name: /^Food$/i }))
    expect(onChange).toHaveBeenCalledWith(['p'])

    rerender(<CustomCategoryFilter selectedIds={['p']} onChange={onChange} />)
    fireEvent.click(screen.getByRole('button', { name: /^Food$/i }))
    expect(onChange).toHaveBeenLastCalledWith([])
  })

  it('shows a Clear control that resets the selection', () => {
    const onChange = mock(() => {})
    render(<CustomCategoryFilter selectedIds={['p', 'c']} onChange={onChange} />)
    fireEvent.click(screen.getByRole('button', { name: /clear/i }))
    expect(onChange).toHaveBeenCalledWith([])
  })

  it('renders nothing when there are no custom categories', () => {
    setCategories([])
    const { container } = render(<CustomCategoryFilter selectedIds={[]} onChange={() => {}} />)
    expect(container.firstChild).toBeNull()
  })

  it('is safe when the query returns a non-array value', () => {
    setCategories({})
    const { container } = render(<CustomCategoryFilter selectedIds={[]} onChange={() => {}} />)
    expect(container.firstChild).toBeNull()
  })
})
