/**
 * useCustomCategories tests (Issue #1203).
 */
import { describe, it, expect, mock } from 'bun:test'
import { renderHook } from '@testing-library/react'
import { mockUseGetUserCategories } from '@/test/mocks/categories-api'
import { useCustomCategories } from './useCustomCategories'

function setData(data: unknown) {
  mockUseGetUserCategories.mockReturnValue({
    data,
    isLoading: false,
    error: null,
    refetch: mock(() => Promise.resolve()),
  } as unknown as ReturnType<typeof mockUseGetUserCategories>)
}

describe('useCustomCategories', () => {
  it('returns [] and an empty hierarchy when the query data is not an array', () => {
    setData({})
    const { result } = renderHook(() => useCustomCategories())
    expect(result.current.categories).toEqual([])
    expect(result.current.hierarchy.topLevel).toEqual([])
    expect(result.current.hierarchy.pfcGroups).toEqual([])
  })

  it('derives the 2-level hierarchy from the categories', () => {
    setData([
      { id: 'p', name: 'Food', user_id: 'u', usage_count: 0, created_at: '', updated_at: '' },
      { id: 'c', name: 'Fast Food', parent_id: 'p', user_id: 'u', usage_count: 0, created_at: '', updated_at: '' },
    ])
    const { result } = renderHook(() => useCustomCategories())
    expect(result.current.categories).toHaveLength(2)
    expect(result.current.hierarchy.topLevel).toHaveLength(1)
    expect(result.current.hierarchy.topLevel[0].parent.id).toBe('p')
    expect(result.current.hierarchy.topLevel[0].children).toHaveLength(1)
    expect(result.current.hierarchy.topLevel[0].children[0].id).toBe('c')
  })
})
