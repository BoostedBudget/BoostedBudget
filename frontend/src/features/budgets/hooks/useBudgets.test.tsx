/**
 * useBudgets Hook Tests
 * Issue #575: Migrated to bun:test with centralized API mocks
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useBudgets } from './useBudgets'
import type { Budget, CreateBudgetRequest, UpdateBudgetRequest } from '../types'
import {
  mockUseGetBudgets,
  mockRefetchBudgets,
  mockCreateBudgetMutateAsync,
  mockUpdateBudgetMutateAsync,
  mockDeleteBudgetMutateAsync,
  mockAddBudgetCategoryMutateAsync,
  mockUpdateBudgetCategoryMutateAsync,
  mockDeleteBudgetCategoryMutateAsync,
  mockGetAvailableCategoriesFn,
} from '@/test/mocks/budgets-api'

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

describe('useBudgets', () => {
  const mockBudgets: Budget[] = [
    {
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
    },
    {
      id: 'budget-2',
      user_id: 'user-123',
      name: 'Weekly Groceries',
      period: 'weekly',
      start_date: '2025-01-06T00:00:00Z',
      amount: 200,
      currency_code: 'USD',
      is_active: true,
      categories: [],
      created_at: '2025-01-06T00:00:00Z',
      updated_at: '2025-01-06T00:00:00Z',
      income_amount: 0,
      income_sources: [],
    },
  ]

  beforeEach(() => {
    mockUseGetBudgets.mockClear()
    mockRefetchBudgets.mockClear()
    mockCreateBudgetMutateAsync.mockClear()
    mockUpdateBudgetMutateAsync.mockClear()
    mockDeleteBudgetMutateAsync.mockClear()
    mockAddBudgetCategoryMutateAsync.mockClear()
    mockUpdateBudgetCategoryMutateAsync.mockClear()
    mockDeleteBudgetCategoryMutateAsync.mockClear()
    mockGetAvailableCategoriesFn.mockClear()
  })

  function configureQuery(config: {
    data?: Budget[] | null
    isLoading?: boolean
    error?: Error | null
  }) {
    mockUseGetBudgets.mockReturnValue({
      data: config.data ?? undefined,
      isLoading: config.isLoading ?? false,
      error: config.error ?? null,
      refetch: mockRefetchBudgets,
    })
  }

  describe('Initial State', () => {
    it('should return loading state initially', () => {
      configureQuery({ isLoading: true })

      const { result } = renderHook(() => useBudgets(), {
        wrapper: createWrapper(),
      })

      expect(result.current.loading).toBe(true)
      expect(result.current.budgets).toEqual([])
      expect(result.current.error).toBeNull()
    })

    it('should fetch and return budgets on mount', async () => {
      configureQuery({ data: mockBudgets })

      const { result } = renderHook(() => useBudgets(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.budgets).toEqual(mockBudgets)
      expect(result.current.error).toBeNull()
    })

    it('should handle fetch errors gracefully', async () => {
      const error = new Error('Failed to fetch budgets')
      configureQuery({ error })

      const { result } = renderHook(() => useBudgets(), {
        wrapper: createWrapper(),
      })

      expect(result.current.loading).toBe(false)
      expect(result.current.error).toEqual(error)
      expect(result.current.budgets).toEqual([])
    })

    it('should return empty array when no budgets exist', async () => {
      configureQuery({ data: [] })

      const { result } = renderHook(() => useBudgets(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.budgets).toEqual([])
      expect(result.current.error).toBeNull()
    })
  })

  describe('Refetch', () => {
    it('should provide refetch function to reload data', async () => {
      configureQuery({ data: mockBudgets })

      const { result } = renderHook(() => useBudgets(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      await act(async () => {
        await result.current.refetch()
      })

      expect(mockRefetchBudgets).toHaveBeenCalled()
    })
  })

  describe('Create Budget', () => {
    it('should create a new budget', async () => {
      configureQuery({ data: [] })
      mockCreateBudgetMutateAsync.mockResolvedValue(mockBudgets[0])

      const createRequest: CreateBudgetRequest = {
        name: 'Monthly Budget',
        period: 'monthly',
        start_date: '2025-01-01T00:00:00Z',
        amount: 2000,
      }

      const { result } = renderHook(() => useBudgets(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      let createdBudget: Budget | undefined
      await act(async () => {
        createdBudget = await result.current.createBudget(createRequest)
      })

      expect(mockCreateBudgetMutateAsync).toHaveBeenCalledWith({ data: createRequest })
      expect(createdBudget).toEqual(mockBudgets[0])
    })

    it('should handle create errors', async () => {
      const error = new Error('Failed to create budget')
      configureQuery({ data: [] })
      mockCreateBudgetMutateAsync.mockRejectedValue(error)

      const createRequest: CreateBudgetRequest = {
        name: 'Monthly Budget',
        period: 'monthly',
        start_date: '2025-01-01T00:00:00Z',
        amount: 2000,
      }

      const { result } = renderHook(() => useBudgets(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      await act(async () => {
        await expect(result.current.createBudget(createRequest)).rejects.toThrow(error)
      })
    })
  })

  describe('Update Budget', () => {
    it('should update an existing budget', async () => {
      const updatedBudget: Budget = {
        ...mockBudgets[0],
        name: 'Updated Monthly Budget',
        amount: 2500,
      }
      configureQuery({ data: mockBudgets })
      mockUpdateBudgetMutateAsync.mockResolvedValue(updatedBudget)

      const updateRequest: UpdateBudgetRequest = {
        name: 'Updated Monthly Budget',
        amount: 2500,
      }

      const { result } = renderHook(() => useBudgets(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      let returnedBudget: Budget | undefined
      await act(async () => {
        returnedBudget = await result.current.updateBudget('budget-1', updateRequest)
      })

      expect(mockUpdateBudgetMutateAsync).toHaveBeenCalledWith({ id: 'budget-1', data: updateRequest })
      expect(returnedBudget).toEqual(updatedBudget)
    })

    it('should handle update errors', async () => {
      const error = new Error('Failed to update budget')
      configureQuery({ data: mockBudgets })
      mockUpdateBudgetMutateAsync.mockRejectedValue(error)

      const updateRequest: UpdateBudgetRequest = {
        name: 'Updated Monthly Budget',
      }

      const { result } = renderHook(() => useBudgets(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      await act(async () => {
        await expect(result.current.updateBudget('budget-1', updateRequest)).rejects.toThrow(error)
      })
    })
  })

  describe('Delete Budget', () => {
    it('should delete a budget', async () => {
      configureQuery({ data: mockBudgets })
      mockDeleteBudgetMutateAsync.mockResolvedValue(undefined)

      const { result } = renderHook(() => useBudgets(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      await act(async () => {
        await result.current.deleteBudget('budget-1')
      })

      expect(mockDeleteBudgetMutateAsync).toHaveBeenCalledWith({ id: 'budget-1' })
    })

    it('should handle delete errors', async () => {
      const error = new Error('Failed to delete budget')
      configureQuery({ data: mockBudgets })
      mockDeleteBudgetMutateAsync.mockRejectedValue(error)

      const { result } = renderHook(() => useBudgets(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      await act(async () => {
        await expect(result.current.deleteBudget('budget-1')).rejects.toThrow(error)
      })
    })
  })

  describe('Get Available Categories', () => {
    it('should fetch available categories', async () => {
      const categories = ['Food and Drink', 'Transportation', 'Shopping']
      configureQuery({ data: [] })
      mockGetAvailableCategoriesFn.mockResolvedValue(categories)

      const { result } = renderHook(() => useBudgets(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      let availableCategories: string[] | undefined
      await act(async () => {
        availableCategories = await result.current.getAvailableCategories()
      })

      expect(mockGetAvailableCategoriesFn).toHaveBeenCalled()
      expect(availableCategories).toEqual(categories)
    })
  })

  describe('Budget Categories', () => {
    it('should add a category to a budget', async () => {
      const newCategory = {
        id: 'cat-2',
        budget_id: 'budget-1',
        category: ['Transportation'],
        allocated_amount: 300,
        spent_amount: 0,
        created_at: '2025-01-15T00:00:00Z',
        updated_at: '2025-01-15T00:00:00Z',
        tags: [],
        match_logic: 'category_only' as const,
      }
      configureQuery({ data: mockBudgets })
      mockAddBudgetCategoryMutateAsync.mockResolvedValue(newCategory)

      const { result } = renderHook(() => useBudgets(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      await act(async () => {
        await result.current.addCategory('budget-1', {
          category: ['Transportation'],
          allocated_amount: 300,
        })
      })

      expect(mockAddBudgetCategoryMutateAsync).toHaveBeenCalledWith({
        id: 'budget-1',
        data: {
          category: ['Transportation'],
          allocated_amount: 300,
        },
      })
    })

    it('should update a category in a budget', async () => {
      const updatedCategory = {
        ...mockBudgets[0].categories![0],
        allocated_amount: 600,
      }
      configureQuery({ data: mockBudgets })
      mockUpdateBudgetCategoryMutateAsync.mockResolvedValue(updatedCategory)

      const { result } = renderHook(() => useBudgets(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      await act(async () => {
        await result.current.updateCategory('budget-1', 'cat-1', {
          allocated_amount: 600,
        })
      })

      expect(mockUpdateBudgetCategoryMutateAsync).toHaveBeenCalledWith({
        id: 'budget-1',
        categoryId: 'cat-1',
        data: {
          allocated_amount: 600,
        },
      })
    })

    it('should delete a category from a budget', async () => {
      configureQuery({ data: mockBudgets })
      mockDeleteBudgetCategoryMutateAsync.mockResolvedValue(undefined)

      const { result } = renderHook(() => useBudgets(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      await act(async () => {
        await result.current.deleteCategory('budget-1', 'cat-1')
      })

      expect(mockDeleteBudgetCategoryMutateAsync).toHaveBeenCalledWith({
        id: 'budget-1',
        categoryId: 'cat-1',
      })
    })
  })
})
