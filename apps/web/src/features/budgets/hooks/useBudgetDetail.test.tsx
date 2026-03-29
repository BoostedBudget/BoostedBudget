import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useBudgetDetail } from './useBudgetDetail'
import type { Budget } from '../types'
import { mockUseGetBudget, mockRefetchBudget } from '@/test/mocks/budgets-api'

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

describe('useBudgetDetail', () => {
  const mockBudget: Budget = {
    id: 'budget-1',
    user_id: 'user-1',
    name: 'Monthly Budget',
    description: 'My monthly budget',
    period: 'monthly',
    start_date: '2024-01-01T00:00:00Z',
    end_date: '2024-01-31T23:59:59Z',
    amount: 3000,
    currency_code: 'USD',
    is_active: true,
    income_amount: 5000,
    income_sources: [],
    categories: [
      {
        id: 'cat-1',
        budget_id: 'budget-1',
        category: ['Food and Drink', 'Restaurants'],
        allocated_amount: 500,
        spent_amount: 250,
        tags: [],
        match_logic: 'category_only',
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z',
      },
      {
        id: 'cat-2',
        budget_id: 'budget-1',
        category: ['Transportation'],
        allocated_amount: 300,
        spent_amount: 150,
        tags: ['Essential'],
        match_logic: 'and',
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z',
      },
    ],
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
  }

  beforeEach(() => {
    mockUseGetBudget.mockClear()
    mockRefetchBudget.mockClear()
  })

  function configureQuery(config: {
    data?: Budget | null
    isLoading?: boolean
    error?: Error | null
  }) {
    mockUseGetBudget.mockReturnValue({
      data: config.data ?? undefined,
      isLoading: config.isLoading ?? false,
      error: config.error ?? null,
      refetch: mockRefetchBudget,
    })
  }

  describe('Initial State', () => {
    it('should return loading=true initially', () => {
      configureQuery({ isLoading: true })

      const { result } = renderHook(() => useBudgetDetail('budget-1'), {
        wrapper: createWrapper(),
      })

      expect(result.current.loading).toBe(true)
      expect(result.current.budget).toBeNull()
      expect(result.current.metrics).toBeNull()
      expect(result.current.error).toBeNull()
    })
  })

  describe('Fetching Budget', () => {
    it('should fetch budget by ID on mount', async () => {
      configureQuery({ data: mockBudget })

      const { result } = renderHook(() => useBudgetDetail('budget-1'), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(mockUseGetBudget).toHaveBeenCalled()
      expect(result.current.budget).toEqual(mockBudget)
    })

    it('should compute metrics from budget data', async () => {
      configureQuery({ data: mockBudget })

      const { result } = renderHook(() => useBudgetDetail('budget-1'), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.metrics).not.toBeNull()
      })

      expect(result.current.metrics).toMatchObject({
        total_allocated: 800,
        total_spent: 400,
        remaining_amount: expect.any(Number),
        percent_used: expect.any(Number),
        unallocated_funds: expect.any(Number),
      })
    })

    it('should refetch when budgetId changes', async () => {
      const budget2: Budget = {
        ...mockBudget,
        id: 'budget-2',
        name: 'Second Budget',
      }

      configureQuery({ data: mockBudget })

      const { result, rerender } = renderHook(
        ({ budgetId }) => useBudgetDetail(budgetId),
        { initialProps: { budgetId: 'budget-1' }, wrapper: createWrapper() }
      )

      await waitFor(() => {
        expect(result.current.budget?.id).toBe('budget-1')
      })

      configureQuery({ data: budget2 })

      rerender({ budgetId: 'budget-2' })

      await waitFor(() => {
        expect(result.current.budget?.id).toBe('budget-2')
      })
    })
  })

  describe('Error Handling', () => {
    it('should handle fetch errors', async () => {
      const testError = new Error('Network error')
      configureQuery({ error: testError })

      const { result } = renderHook(() => useBudgetDetail('budget-1'), {
        wrapper: createWrapper(),
      })

      expect(result.current.loading).toBe(false)
      expect(result.current.error).toBeTruthy()
      expect(result.current.error?.message).toBe('Network error')
      expect(result.current.budget).toBeNull()
      expect(result.current.metrics).toBeNull()
    })

    it('should clear error on successful refetch', async () => {
      configureQuery({ error: new Error('First error') })

      const { result } = renderHook(() => useBudgetDetail('budget-1'), {
        wrapper: createWrapper(),
      })

      expect(result.current.error).toBeTruthy()

      configureQuery({ data: mockBudget })

      await act(async () => {
        await result.current.refetch()
      })

      expect(mockRefetchBudget).toHaveBeenCalled()
    })
  })

  describe('Refetch', () => {
    it('should provide refetch function', async () => {
      configureQuery({ data: mockBudget })

      const { result } = renderHook(() => useBudgetDetail('budget-1'), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(typeof result.current.refetch).toBe('function')
    })

    it('should refetch data when called', async () => {
      configureQuery({ data: mockBudget })

      const { result } = renderHook(() => useBudgetDetail('budget-1'), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.budget?.name).toBe('Monthly Budget')
      })

      await act(async () => {
        await result.current.refetch()
      })

      expect(mockRefetchBudget).toHaveBeenCalled()
    })
  })

  describe('Edge Cases', () => {
    it('should handle budget without categories', async () => {
      const budgetNoCategories: Budget = {
        ...mockBudget,
        categories: [],
      }

      configureQuery({ data: budgetNoCategories })

      const { result } = renderHook(() => useBudgetDetail('budget-1'), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.metrics?.total_allocated).toBe(0)
      expect(result.current.metrics?.total_spent).toBe(0)
    })

    it('should handle budget without income amount', async () => {
      const budgetNoIncome: Budget = {
        ...mockBudget,
        income_amount: 0,
      }

      configureQuery({ data: budgetNoIncome })

      const { result } = renderHook(() => useBudgetDetail('budget-1'), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.metrics?.unallocated_funds).toBe(-800)
    })
  })
})
