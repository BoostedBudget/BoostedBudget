import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import type { Budget } from '../types'

const mockRefetch = mock(() => {})
const mockUseGetCurrentBudget = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockRefetch,
}))
const mockGetQueryKey = mock(() => ['budgets', 'current'])

mock.module('@/api/generated/endpoints/budgets/budgets', () => ({
  useGetCurrentBudget: mockUseGetCurrentBudget,
  getGetCurrentBudgetQueryKey: mockGetQueryKey,
}))

import { useCurrentBudget } from './useCurrentBudget'

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
}

describe('useCurrentBudget', () => {
  const mockBudget: Budget = {
    id: 'budget-1',
    user_id: 'user-123',
    name: 'Monthly Budget',
    description: 'My monthly budget',
    period: 'monthly',
    start_date: '2025-01-01T00:00:00Z',
    end_date: '2025-01-31T23:59:59Z',
    amount: 2000,
    currency_code: 'USD',
    is_active: true,
    categories: [
      {
        id: 'cat-1',
        budget_id: 'budget-1',
        category: ['Food and Drink', 'Restaurants'],
        allocated_amount: 500,
        spent_amount: 150,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T00:00:00Z',
        tags: [],
        match_logic: 'category_only',
      },
    ],
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
    income_amount: 0,
    income_sources: [],
  };

  beforeEach(() => {
    mockUseGetCurrentBudget.mockClear()
    mockRefetch.mockClear()
    mockGetQueryKey.mockClear()
  })

  function mockState(
    data?: Budget | null,
    isLoading = false,
    error: unknown = null
  ) {
    mockUseGetCurrentBudget.mockReturnValue({
      data,
      isLoading,
      error,
      refetch: mockRefetch,
    })
  }

  describe('Initial State', () => {
    it('should return loading state initially', () => {
      mockState(undefined, true)

      const { result } = renderHook(() => useCurrentBudget(), {
        wrapper: createWrapper(),
      })

      expect(result.current.loading).toBe(true)
      expect(result.current.budget).toBeNull()
      expect(result.current.error).toBeNull()
    })

    it('should return current budget on successful fetch', () => {
      mockState(mockBudget)

      const { result } = renderHook(() => useCurrentBudget(), {
        wrapper: createWrapper(),
      })

      expect(result.current.loading).toBe(false)
      expect(result.current.budget).toEqual(mockBudget)
      expect(result.current.error).toBeNull()
    })

    it('should handle 404 when no current budget exists', () => {
      mockState(undefined, false, { response: { status: 404 } })

      const { result } = renderHook(() => useCurrentBudget(), {
        wrapper: createWrapper(),
      })

      expect(result.current.loading).toBe(false)
      expect(result.current.budget).toBeNull()
      expect(result.current.hasNoBudget).toBe(true)
      expect(result.current.error).toBeNull()
    })

    it('should handle fetch errors gracefully', () => {
      const error = new Error('Failed to fetch current budget')
      mockState(undefined, false, error)

      const { result } = renderHook(() => useCurrentBudget(), {
        wrapper: createWrapper(),
      })

      expect(result.current.loading).toBe(false)
      expect(result.current.error).toEqual(error)
      expect(result.current.budget).toBeNull()
    })
  })

  describe('Computed Values', () => {
    it('should compute budget metrics correctly', () => {
      const budgetWithCategories: Budget = {
        ...mockBudget,
        amount: 1000,
        categories: [
          {
            id: 'cat-1',
            budget_id: 'budget-1',
            category: ['Food'],
            allocated_amount: 400,
            spent_amount: 200,
            created_at: '2025-01-01T00:00:00Z',
            updated_at: '2025-01-01T00:00:00Z',
            tags: [],
            match_logic: 'category_only',
          },
          {
            id: 'cat-2',
            budget_id: 'budget-1',
            category: ['Transport'],
            allocated_amount: 300,
            spent_amount: 100,
            created_at: '2025-01-01T00:00:00Z',
            updated_at: '2025-01-01T00:00:00Z',
            tags: [],
            match_logic: 'category_only',
          },
        ],
      }
      mockState(budgetWithCategories)

      const { result } = renderHook(() => useCurrentBudget(), {
        wrapper: createWrapper(),
      })

      expect(result.current.metrics).toBeDefined()
      expect(result.current.metrics?.total_allocated).toBe(700)
      expect(result.current.metrics?.total_spent).toBe(300)
      expect(result.current.metrics?.remaining_amount).toBe(700)
      expect(result.current.metrics?.percent_used).toBe(30)
      expect(result.current.metrics?.is_over_budget).toBe(false)
    })

    it('should detect over-budget status', () => {
      const overBudget: Budget = {
        ...mockBudget,
        amount: 500,
        categories: [
          {
            id: 'cat-1',
            budget_id: 'budget-1',
            category: ['Food'],
            allocated_amount: 600,
            spent_amount: 600,
            created_at: '2025-01-01T00:00:00Z',
            updated_at: '2025-01-01T00:00:00Z',
            tags: [],
            match_logic: 'category_only',
          },
        ],
      }
      mockState(overBudget)

      const { result } = renderHook(() => useCurrentBudget(), {
        wrapper: createWrapper(),
      })

      expect(result.current.metrics?.is_over_budget).toBe(true)
      expect(result.current.metrics?.percent_used).toBe(120)
    })

    it('should return null metrics when no budget exists', () => {
      mockState(undefined, false, { response: { status: 404 } })

      const { result } = renderHook(() => useCurrentBudget(), {
        wrapper: createWrapper(),
      })

      expect(result.current.metrics).toBeNull()
    })
  })

  describe('Refetch', () => {
    it('should provide refetch function to reload data', async () => {
      mockState(mockBudget)

      const { result } = renderHook(() => useCurrentBudget(), {
        wrapper: createWrapper(),
      })

      await act(async () => {
        await result.current.refetch()
      })

      await waitFor(() => {
        expect(mockRefetch).toHaveBeenCalled()
      })
    })
  })
})
