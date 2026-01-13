import { describe, it, expect, mock, beforeEach, setSystemTime } from 'bun:test'
import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'

const mockMutateAsync = mock(() => Promise.resolve({ transaction_count: 0, total_amount: 0 }))

mock.module('@boostedbudget/api-client/endpoints/budget/budget', () => ({
  usePreviewCategoryMatches: mock(() => ({
    mutateAsync: mockMutateAsync,
    mutate: mock(() => {}),
    isLoading: false,
    isError: false,
    error: null,
    data: undefined,
    reset: mock(() => {}),
  })),
}))

import { usePreviewCategories } from './usePreviewCategories'

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

describe('usePreviewCategories', () => {
  beforeEach(() => {
    mockMutateAsync.mockClear()
    mockMutateAsync.mockReset()
  })

  const defaultParams = {
    categories: [['Food and Drink', 'Restaurants']],
    tags: [] as string[],
    matchLogic: 'category_only' as const,
    startDate: '2024-01-01',
    endDate: '2024-01-31',
    enabled: true,
  }

  it('should return loading=true initially when enabled', async () => {
    mockMutateAsync.mockResolvedValue({ transaction_count: 5, total_amount: 250 })

    const { result } = renderHook(
      () =>
        usePreviewCategories(
          defaultParams.categories,
          defaultParams.tags,
          defaultParams.matchLogic,
          defaultParams.startDate,
          defaultParams.endDate,
          defaultParams.enabled
        ),
      { wrapper: createWrapper() }
    )

    expect(result.current.preview).toBeNull()
  })

  it('should debounce requests by 300ms', async () => {
    setSystemTime(new Date('2024-01-15'))
    mockMutateAsync.mockResolvedValue({ transaction_count: 5, total_amount: 250 })

    const { rerender } = renderHook(
      ({ categories }) =>
        usePreviewCategories(
          categories,
          defaultParams.tags,
          defaultParams.matchLogic,
          defaultParams.startDate,
          defaultParams.endDate,
          defaultParams.enabled
        ),
      { initialProps: { categories: [['Food', 'A']] }, wrapper: createWrapper() }
    )

    rerender({ categories: [['Food', 'B']] })
    rerender({ categories: [['Food', 'C']] })
    rerender({ categories: [['Food', 'D']] })

    expect(mockMutateAsync).not.toHaveBeenCalled()

    await act(async () => {
      await new Promise(resolve => setTimeout(resolve, 400))
    })

    expect(mockMutateAsync).toHaveBeenCalledTimes(1)
    expect(mockMutateAsync).toHaveBeenCalledWith(
      expect.objectContaining({
        data: expect.objectContaining({
          categories: [['Food', 'D']],
        }),
      })
    )

    setSystemTime()
  })

  it('should return count and amount on success', async () => {
    mockMutateAsync.mockResolvedValue({ transaction_count: 10, total_amount: 500.5 })

    const { result } = renderHook(
      () =>
        usePreviewCategories(
          defaultParams.categories,
          defaultParams.tags,
          defaultParams.matchLogic,
          defaultParams.startDate,
          defaultParams.endDate,
          defaultParams.enabled
        ),
      { wrapper: createWrapper() }
    )

    await waitFor(() => {
      expect(result.current.preview).toEqual({
        transaction_count: 10,
        total_amount: 500.5,
      })
      expect(result.current.loading).toBe(false)
      expect(result.current.error).toBeNull()
    })
  })

  it('should handle API errors gracefully', async () => {
    mockMutateAsync.mockRejectedValue(new Error('Network error'))

    const { result } = renderHook(
      () =>
        usePreviewCategories(
          defaultParams.categories,
          defaultParams.tags,
          defaultParams.matchLogic,
          defaultParams.startDate,
          defaultParams.endDate,
          defaultParams.enabled
        ),
      { wrapper: createWrapper() }
    )

    await waitFor(() => {
      expect(result.current.error).toBeTruthy()
      expect(result.current.error?.message).toBe('Network error')
      expect(result.current.loading).toBe(false)
      expect(result.current.preview).toBeNull()
    })
  })

  it('should not make API call when enabled=false', async () => {
    mockMutateAsync.mockResolvedValue({ transaction_count: 5, total_amount: 250 })

    renderHook(
      () =>
        usePreviewCategories(
          defaultParams.categories,
          defaultParams.tags,
          defaultParams.matchLogic,
          defaultParams.startDate,
          defaultParams.endDate,
          false
        ),
      { wrapper: createWrapper() }
    )

    await act(async () => {
      await new Promise(resolve => setTimeout(resolve, 400))
    })

    expect(mockMutateAsync).not.toHaveBeenCalled()
  })

  it('should not make API call when categories are empty', async () => {
    mockMutateAsync.mockResolvedValue({ transaction_count: 5, total_amount: 250 })

    renderHook(
      () =>
        usePreviewCategories(
          [],
          defaultParams.tags,
          defaultParams.matchLogic,
          defaultParams.startDate,
          defaultParams.endDate,
          defaultParams.enabled
        ),
      { wrapper: createWrapper() }
    )

    await act(async () => {
      await new Promise(resolve => setTimeout(resolve, 400))
    })

    expect(mockMutateAsync).not.toHaveBeenCalled()
  })

  it('should cancel pending request on param change', async () => {
    mockMutateAsync
      .mockResolvedValueOnce({ transaction_count: 5, total_amount: 250 })
      .mockResolvedValueOnce({ transaction_count: 20, total_amount: 1000 })

    const { result, rerender } = renderHook(
      ({ categories }) =>
        usePreviewCategories(
          categories,
          defaultParams.tags,
          defaultParams.matchLogic,
          defaultParams.startDate,
          defaultParams.endDate,
          defaultParams.enabled
        ),
      { initialProps: { categories: [['Food', 'A']] }, wrapper: createWrapper() }
    )

    await waitFor(() => {
      expect(mockMutateAsync).toHaveBeenCalledTimes(1)
    })

    rerender({ categories: [['Food', 'B']] })

    await waitFor(() => {
      expect(mockMutateAsync).toHaveBeenCalledTimes(2)
    })

    await waitFor(() => {
      expect(result.current.preview?.transaction_count).toBe(20)
    })
  })

  it('should reset error when params change', async () => {
    mockMutateAsync
      .mockRejectedValueOnce(new Error('First error'))
      .mockResolvedValueOnce({ transaction_count: 5, total_amount: 250 })

    const { result, rerender } = renderHook(
      ({ categories }) =>
        usePreviewCategories(
          categories,
          defaultParams.tags,
          defaultParams.matchLogic,
          defaultParams.startDate,
          defaultParams.endDate,
          defaultParams.enabled
        ),
      { initialProps: { categories: [['Food', 'A']] }, wrapper: createWrapper() }
    )

    await waitFor(() => {
      expect(result.current.error).toBeTruthy()
    }, { timeout: 5000 })

    rerender({ categories: [['Food', 'B']] })

    await waitFor(() => {
      expect(result.current.error).toBeNull()
      expect(result.current.preview?.transaction_count).toBe(5)
    }, { timeout: 5000 })
  }, 15000)

  it('should pass correct params to API', async () => {
    mockMutateAsync.mockResolvedValue({ transaction_count: 5, total_amount: 250 })

    renderHook(
      () =>
        usePreviewCategories(
          [
            ['Food', 'Restaurants'],
            ['Shopping', 'Clothing'],
          ],
          ['Essential', 'Discretionary'],
          'and',
          '2024-02-01',
          '2024-02-29',
          true
        ),
      { wrapper: createWrapper() }
    )

    await waitFor(() => {
      expect(mockMutateAsync).toHaveBeenCalledWith({
        data: {
          categories: [
            ['Food', 'Restaurants'],
            ['Shopping', 'Clothing'],
          ],
          tags: ['Essential', 'Discretionary'],
          match_logic: 'and',
          start_date: '2024-02-01',
          end_date: '2024-02-29',
        },
      })
    })
  })
})
