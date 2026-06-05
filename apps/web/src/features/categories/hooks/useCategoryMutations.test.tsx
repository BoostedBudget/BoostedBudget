/**
 * useCategoryMutations tests (Issue #1203).
 */
import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import {
  mockCreateCategoryMutateAsync,
  mockUpdateCategoryMutateAsync,
  mockDeleteCategoryMutateAsync,
} from '@/test/mocks/categories-api'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import { useCategoryMutations } from './useCategoryMutations'

function wrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  }
}

describe('useCategoryMutations', () => {
  beforeEach(() => {
    mockCreateCategoryMutateAsync.mockClear()
    mockUpdateCategoryMutateAsync.mockClear()
    mockDeleteCategoryMutateAsync.mockClear()
    mockTrackEvent.mockClear()
  })

  it('createCategory sends {data} and tracks CUSTOM_CATEGORY_CREATED', async () => {
    mockCreateCategoryMutateAsync.mockReturnValue(Promise.resolve({ id: 'new-1', name: 'Travel' }))
    const { result } = renderHook(() => useCategoryMutations(), { wrapper: wrapper() })

    const created = await result.current.createCategory({ name: 'Travel' })

    expect(mockCreateCategoryMutateAsync).toHaveBeenCalledWith({ data: { name: 'Travel' } })
    expect(created.id).toBe('new-1')
    expect(mockTrackEvent).toHaveBeenCalledWith(
      ANALYTICS_EVENTS.CUSTOM_CATEGORY_CREATED,
      expect.objectContaining({ parent_kind: 'none' })
    )
  })

  it('createCategory reports parent_kind=custom when nesting under a custom parent', async () => {
    mockCreateCategoryMutateAsync.mockReturnValue(Promise.resolve({ id: 'new-2', name: 'Sub' }))
    const { result } = renderHook(() => useCategoryMutations(), { wrapper: wrapper() })

    await result.current.createCategory({ name: 'Sub', parent_id: 'p1' })

    expect(mockTrackEvent).toHaveBeenCalledWith(
      ANALYTICS_EVENTS.CUSTOM_CATEGORY_CREATED,
      expect.objectContaining({ parent_kind: 'custom' })
    )
  })

  it('updateCategory sends {id,data} and tracks CUSTOM_CATEGORY_UPDATED', async () => {
    mockUpdateCategoryMutateAsync.mockReturnValue(Promise.resolve({ id: 'c1', name: 'Renamed' }))
    const { result } = renderHook(() => useCategoryMutations(), { wrapper: wrapper() })

    await result.current.updateCategory('c1', { name: 'Renamed' })

    expect(mockUpdateCategoryMutateAsync).toHaveBeenCalledWith({ id: 'c1', data: { name: 'Renamed' } })
    expect(mockTrackEvent).toHaveBeenCalledWith(
      ANALYTICS_EVENTS.CUSTOM_CATEGORY_UPDATED,
      expect.objectContaining({ id: 'c1' })
    )
  })

  it('deleteCategory sends {id} and tracks CUSTOM_CATEGORY_DELETED', async () => {
    mockDeleteCategoryMutateAsync.mockReturnValue(Promise.resolve())
    const { result } = renderHook(() => useCategoryMutations(), { wrapper: wrapper() })

    await result.current.deleteCategory('c1')

    expect(mockDeleteCategoryMutateAsync).toHaveBeenCalledWith({ id: 'c1' })
    expect(mockTrackEvent).toHaveBeenCalledWith(
      ANALYTICS_EVENTS.CUSTOM_CATEGORY_DELETED,
      expect.objectContaining({ id: 'c1' })
    )
  })
})
