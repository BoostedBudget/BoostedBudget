/**
 * useCategoryUsage tests (Issue #1203).
 */
import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook } from '@testing-library/react'
import { mockUseGetCategoryUsage } from '@/test/mocks/categories-api'
import { useCategoryUsage } from './useCategoryUsage'

describe('useCategoryUsage', () => {
  beforeEach(() => mockUseGetCategoryUsage.mockClear())

  it('disables the query when there is no category id', () => {
    renderHook(() => useCategoryUsage(null))
    const lastCall = mockUseGetCategoryUsage.mock.calls.at(-1) as unknown as [string, { query?: { enabled?: boolean } }]
    expect(lastCall?.[1]?.query?.enabled).toBe(false)
  })

  it('disables the query when enabled=false even with an id', () => {
    renderHook(() => useCategoryUsage('c1', false))
    const lastCall = mockUseGetCategoryUsage.mock.calls.at(-1) as unknown as [string, { query?: { enabled?: boolean } }]
    expect(lastCall?.[1]?.query?.enabled).toBe(false)
  })

  it('maps the usage payload and null error', () => {
    mockUseGetCategoryUsage.mockReturnValue({
      data: { transaction_count: 2, budget_count: 1, rule_count: 0, child_count: 3 },
      isLoading: false,
      error: null,
    } as unknown as ReturnType<typeof mockUseGetCategoryUsage>)
    const { result } = renderHook(() => useCategoryUsage('c1'))
    expect(result.current.usage?.transaction_count).toBe(2)
    expect(result.current.usage?.child_count).toBe(3)
    expect(result.current.error).toBeNull()
  })
})
