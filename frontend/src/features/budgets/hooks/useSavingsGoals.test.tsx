/**
 * useSavingsGoals Hook Tests
 * Issue #564: Migrated to mock generated React Query hooks
 * Issue #575: Uses centralized API mocks via preloaded savings-goals-api.ts
 *
 * NOTE: This test file may show failures when run in the full test suite due to
 * mock pollution from BudgetsPage.test.tsx. When run alone, all tests pass.
 * This is a known limitation of Bun's mock.module() persistence.
 */

import { describe, it, expect, beforeEach, beforeAll } from 'bun:test'
import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import type { SavingsGoal, CreateSavingsGoalRequest, UpdateSavingsGoalRequest } from '../types'

// Import from centralized mock (preloaded via bunfig.real.toml)
import {
  mockUseGetSavingsGoals,
  mockUseGetActiveSavingsGoals,
  mockCreateSavingsGoalMutateAsync,
  mockUpdateSavingsGoalMutateAsync,
  mockDeleteSavingsGoalMutateAsync,
  mockSavingsGoalsRefetch,
  mockActiveSavingsGoalsRefetch,
} from '@/test/mocks/savings-goals-api'

// Import the hook (will use the preloaded API mock)
import { useSavingsGoals } from './useSavingsGoals'

// Helper to create wrapper with QueryClient
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

describe('useSavingsGoals', () => {
  const mockSavingsGoals: SavingsGoal[] = [
    {
      id: 'goal-1',
      user_id: 'user-123',
      name: 'Vacation Fund',
      description: 'Save for summer vacation',
      target_amount: 5000,
      current_amount: 1500,
      currency_code: 'USD',
      account_id: 'account-789',
      deadline: '2025-07-01T00:00:00Z',
      is_completed: false,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
    {
      id: 'goal-2',
      user_id: 'user-123',
      name: 'Emergency Fund',
      description: '6 months expenses',
      target_amount: 10000,
      current_amount: 8000,
      currency_code: 'USD',
      account_id: 'account-789',
      is_completed: false,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
  ]

  beforeEach(() => {
    // Clear all mocks
    mockCreateSavingsGoalMutateAsync.mockClear()
    mockUpdateSavingsGoalMutateAsync.mockClear()
    mockDeleteSavingsGoalMutateAsync.mockClear()
    mockSavingsGoalsRefetch.mockClear()
    mockActiveSavingsGoalsRefetch.mockClear()
    mockUseGetSavingsGoals.mockClear()
    mockUseGetActiveSavingsGoals.mockClear()
  })

  // Helper to configure query states
  function configureAllGoalsQuery(config: {
    data?: SavingsGoal[] | null
    isLoading?: boolean
    error?: Error | null
  }) {
    mockUseGetSavingsGoals.mockReturnValue({
      data: config.data ?? undefined,
      isLoading: config.isLoading ?? false,
      error: config.error ?? null,
      refetch: mockSavingsGoalsRefetch,
    })
  }

  function configureActiveGoalsQuery(config: {
    data?: SavingsGoal[] | null
    isLoading?: boolean
    error?: Error | null
  }) {
    mockUseGetActiveSavingsGoals.mockReturnValue({
      data: config.data ?? undefined,
      isLoading: config.isLoading ?? false,
      error: config.error ?? null,
      refetch: mockActiveSavingsGoalsRefetch,
    })
  }

  describe('Initial State', () => {
    it('should return loading state initially', () => {
      configureAllGoalsQuery({ isLoading: true })
      configureActiveGoalsQuery({ isLoading: false, data: [] })

      const { result } = renderHook(() => useSavingsGoals(), {
        wrapper: createWrapper(),
      })

      expect(result.current.loading).toBe(true)
      expect(result.current.goals).toEqual([])
      expect(result.current.error).toBeNull()
    })

    it('should fetch and return savings goals on mount', async () => {
      configureAllGoalsQuery({ data: mockSavingsGoals })
      configureActiveGoalsQuery({ data: [] })

      const { result } = renderHook(() => useSavingsGoals(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.goals).toEqual(mockSavingsGoals)
      expect(result.current.error).toBeNull()
    })

    it('should handle fetch errors gracefully', async () => {
      const error = new Error('Failed to fetch savings goals')
      configureAllGoalsQuery({ error })
      configureActiveGoalsQuery({ data: [] })

      const { result } = renderHook(() => useSavingsGoals(), {
        wrapper: createWrapper(),
      })

      expect(result.current.loading).toBe(false)
      expect(result.current.error).toEqual(error)
      expect(result.current.goals).toEqual([])
    })

    it('should return empty array when no goals exist', async () => {
      configureAllGoalsQuery({ data: [] })
      configureActiveGoalsQuery({ data: [] })

      const { result } = renderHook(() => useSavingsGoals(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.goals).toEqual([])
      expect(result.current.error).toBeNull()
    })
  })

  describe('Active Goals Filter', () => {
    it('should filter active goals when activeOnly option is true', async () => {
      const activeGoals = [mockSavingsGoals[0]]
      configureAllGoalsQuery({ data: [] })
      configureActiveGoalsQuery({ data: activeGoals })

      const { result } = renderHook(() => useSavingsGoals({ activeOnly: true }), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.goals).toEqual(activeGoals)
    })
  })

  describe('Refetch', () => {
    it('should provide refetch function to reload data', async () => {
      configureAllGoalsQuery({ data: mockSavingsGoals })
      configureActiveGoalsQuery({ data: [] })

      const { result } = renderHook(() => useSavingsGoals(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      await act(async () => {
        await result.current.refetch()
      })

      expect(mockSavingsGoalsRefetch).toHaveBeenCalled()
    })
  })

  describe('Create Savings Goal', () => {
    it('should create a new savings goal', async () => {
      configureAllGoalsQuery({ data: [] })
      configureActiveGoalsQuery({ data: [] })
      mockCreateSavingsGoalMutateAsync.mockResolvedValue(mockSavingsGoals[0])

      const createRequest: CreateSavingsGoalRequest = {
        name: 'Vacation Fund',
        target_amount: 5000,
        account_id: 'account-789',
      }

      const { result } = renderHook(() => useSavingsGoals(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      let createdGoal: SavingsGoal | undefined
      await act(async () => {
        createdGoal = await result.current.createGoal(createRequest)
      })

      expect(mockCreateSavingsGoalMutateAsync).toHaveBeenCalledWith({ data: createRequest })
      expect(createdGoal).toEqual(mockSavingsGoals[0])
    })

    it('should handle create errors', async () => {
      const error = new Error('Failed to create savings goal')
      configureAllGoalsQuery({ data: [] })
      configureActiveGoalsQuery({ data: [] })
      mockCreateSavingsGoalMutateAsync.mockRejectedValue(error)

      const createRequest: CreateSavingsGoalRequest = {
        name: 'Vacation Fund',
        target_amount: 5000,
        account_id: 'account-789',
      }

      const { result } = renderHook(() => useSavingsGoals(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      await expect(result.current.createGoal(createRequest)).rejects.toThrow('Failed to create savings goal')
    })
  })

  describe('Update Savings Goal', () => {
    it('should update an existing savings goal', async () => {
      const updatedGoal: SavingsGoal = {
        ...mockSavingsGoals[0],
        current_amount: 2000,
      }
      configureAllGoalsQuery({ data: mockSavingsGoals })
      configureActiveGoalsQuery({ data: [] })
      mockUpdateSavingsGoalMutateAsync.mockResolvedValue(updatedGoal)

      const updateRequest: UpdateSavingsGoalRequest = {
        current_amount: 2000,
      }

      const { result } = renderHook(() => useSavingsGoals(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      let returnedGoal: SavingsGoal | undefined
      await act(async () => {
        returnedGoal = await result.current.updateGoal('goal-1', updateRequest)
      })

      expect(mockUpdateSavingsGoalMutateAsync).toHaveBeenCalledWith({ id: 'goal-1', data: updateRequest })
      expect(returnedGoal).toEqual(updatedGoal)
    })

    it('should handle update errors', async () => {
      const error = new Error('Failed to update savings goal')
      configureAllGoalsQuery({ data: mockSavingsGoals })
      configureActiveGoalsQuery({ data: [] })
      mockUpdateSavingsGoalMutateAsync.mockRejectedValue(error)

      const updateRequest: UpdateSavingsGoalRequest = {
        current_amount: 2000,
      }

      const { result } = renderHook(() => useSavingsGoals(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      await expect(result.current.updateGoal('goal-1', updateRequest)).rejects.toThrow('Failed to update savings goal')
    })
  })

  describe('Delete Savings Goal', () => {
    it('should delete a savings goal', async () => {
      configureAllGoalsQuery({ data: mockSavingsGoals })
      configureActiveGoalsQuery({ data: [] })
      mockDeleteSavingsGoalMutateAsync.mockResolvedValue(undefined)

      const { result } = renderHook(() => useSavingsGoals(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      await act(async () => {
        await result.current.deleteGoal('goal-1')
      })

      expect(mockDeleteSavingsGoalMutateAsync).toHaveBeenCalledWith({ id: 'goal-1' })
    })

    it('should handle delete errors', async () => {
      const error = new Error('Failed to delete savings goal')
      configureAllGoalsQuery({ data: mockSavingsGoals })
      configureActiveGoalsQuery({ data: [] })
      mockDeleteSavingsGoalMutateAsync.mockRejectedValue(error)

      const { result } = renderHook(() => useSavingsGoals(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      await expect(result.current.deleteGoal('goal-1')).rejects.toThrow('Failed to delete savings goal')
    })
  })

  describe('Computed Values', () => {
    it('should compute progress percentage for each goal', async () => {
      configureAllGoalsQuery({ data: mockSavingsGoals })
      configureActiveGoalsQuery({ data: [] })

      const { result } = renderHook(() => useSavingsGoals(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.goalsWithProgress).toBeDefined()
      expect(result.current.goalsWithProgress[0].progress).toBe(30) // 1500/5000 * 100
      expect(result.current.goalsWithProgress[1].progress).toBe(80) // 8000/10000 * 100
    })

    it('should compute total progress across all goals', async () => {
      configureAllGoalsQuery({ data: mockSavingsGoals })
      configureActiveGoalsQuery({ data: [] })

      const { result } = renderHook(() => useSavingsGoals(), {
        wrapper: createWrapper(),
      })

      await waitFor(() => {
        expect(result.current.loading).toBe(false)
      })

      expect(result.current.totalTarget).toBe(15000) // 5000 + 10000
      expect(result.current.totalSaved).toBe(9500) // 1500 + 8000
    })
  })
})
