/**
 * useSavingsGoals Hook Tests
 * Issue #564: Migrated to mock generated React Query hooks
 */

import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import type { SavingsGoal, CreateSavingsGoalRequest, UpdateSavingsGoalRequest } from '../types'

// Mock mutation functions
const mockCreateMutateAsync = mock(() => Promise.resolve({}))
const mockUpdateMutateAsync = mock(() => Promise.resolve({}))
const mockDeleteMutateAsync = mock(() => Promise.resolve({}))
const mockAllRefetch = mock(() => Promise.resolve({}))
const mockActiveRefetch = mock(() => Promise.resolve({}))

// Mock for query hooks
const mockUseGetSavingsGoals = mock(() => ({
  data: undefined,
  isLoading: true,
  error: null,
  refetch: mockAllRefetch,
}))

const mockUseGetActiveSavingsGoals = mock(() => ({
  data: undefined,
  isLoading: false,
  error: null,
  refetch: mockActiveRefetch,
}))

// Mock the generated hooks
mock.module('@/api/generated/endpoints/savings-goals/savings-goals', () => ({
  useGetSavingsGoals: mockUseGetSavingsGoals,
  useGetActiveSavingsGoals: mockUseGetActiveSavingsGoals,
  useCreateSavingsGoal: mock(() => ({
    mutateAsync: mockCreateMutateAsync,
  })),
  useUpdateSavingsGoal: mock(() => ({
    mutateAsync: mockUpdateMutateAsync,
  })),
  useDeleteSavingsGoal: mock(() => ({
    mutateAsync: mockDeleteMutateAsync,
  })),
  getGetSavingsGoalsQueryKey: mock(() => ['savings-goals']),
  getGetActiveSavingsGoalsQueryKey: mock(() => ['savings-goals', 'active']),
}))

import { useSavingsGoals } from './useSavingsGoals'

// Helper to create wrapper with QueryClient
function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  });
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
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
  ];

  beforeEach(() => {
    mockCreateMutateAsync.mockClear()
    mockUpdateMutateAsync.mockClear()
    mockDeleteMutateAsync.mockClear()
    mockAllRefetch.mockClear()
    mockActiveRefetch.mockClear()
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
      refetch: mockAllRefetch,
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
      refetch: mockActiveRefetch,
    })
  }

  describe('Initial State', () => {
    it('should return loading state initially', () => {
      configureAllGoalsQuery({ isLoading: true });
      configureActiveGoalsQuery({ isLoading: false, data: [] });

      const { result } = renderHook(() => useSavingsGoals(), {
        wrapper: createWrapper(),
      });

      expect(result.current.loading).toBe(true);
      expect(result.current.goals).toEqual([]);
      expect(result.current.error).toBeNull();
    });

    it('should fetch and return savings goals on mount', async () => {
      configureAllGoalsQuery({ data: mockSavingsGoals });
      configureActiveGoalsQuery({ data: [] });

      const { result } = renderHook(() => useSavingsGoals(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.goals).toEqual(mockSavingsGoals);
      expect(result.current.error).toBeNull();
    });

    it('should handle fetch errors gracefully', async () => {
      const error = new Error('Failed to fetch savings goals');
      configureAllGoalsQuery({ error });
      configureActiveGoalsQuery({ data: [] });

      const { result } = renderHook(() => useSavingsGoals(), {
        wrapper: createWrapper(),
      });

      expect(result.current.loading).toBe(false);
      expect(result.current.error).toEqual(error);
      expect(result.current.goals).toEqual([]);
    });

    it('should return empty array when no goals exist', async () => {
      configureAllGoalsQuery({ data: [] });
      configureActiveGoalsQuery({ data: [] });

      const { result } = renderHook(() => useSavingsGoals(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.goals).toEqual([]);
      expect(result.current.error).toBeNull();
    });
  });

  describe('Active Goals Filter', () => {
    it('should filter active goals when activeOnly option is true', async () => {
      const activeGoals = [mockSavingsGoals[0]];
      configureAllGoalsQuery({ data: [] });
      configureActiveGoalsQuery({ data: activeGoals });

      const { result } = renderHook(() => useSavingsGoals({ activeOnly: true }), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.goals).toEqual(activeGoals);
    });
  });

  describe('Refetch', () => {
    it('should provide refetch function to reload data', async () => {
      configureAllGoalsQuery({ data: mockSavingsGoals });
      configureActiveGoalsQuery({ data: [] });

      const { result } = renderHook(() => useSavingsGoals(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      await act(async () => {
        await result.current.refetch();
      });

      expect(mockAllRefetch).toHaveBeenCalled();
    });
  });

  describe('Create Savings Goal', () => {
    it('should create a new savings goal', async () => {
      configureAllGoalsQuery({ data: [] });
      configureActiveGoalsQuery({ data: [] });
      mockCreateMutateAsync.mockResolvedValue(mockSavingsGoals[0]);

      const createRequest: CreateSavingsGoalRequest = {
        name: 'Vacation Fund',
        target_amount: 5000,
        account_id: 'account-789',
      };

      const { result } = renderHook(() => useSavingsGoals(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      let createdGoal: SavingsGoal | undefined;
      await act(async () => {
        createdGoal = await result.current.createGoal(createRequest);
      });

      expect(mockCreateMutateAsync).toHaveBeenCalledWith({ data: createRequest });
      expect(createdGoal).toEqual(mockSavingsGoals[0]);
    });

    it('should handle create errors', async () => {
      const error = new Error('Failed to create savings goal');
      configureAllGoalsQuery({ data: [] });
      configureActiveGoalsQuery({ data: [] });
      mockCreateMutateAsync.mockRejectedValue(error);

      const createRequest: CreateSavingsGoalRequest = {
        name: 'Vacation Fund',
        target_amount: 5000,
        account_id: 'account-789',
      };

      const { result } = renderHook(() => useSavingsGoals(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      await act(async () => {
        await expect(result.current.createGoal(createRequest)).rejects.toThrow(error);
      });
    });
  });

  describe('Update Savings Goal', () => {
    it('should update an existing savings goal', async () => {
      const updatedGoal: SavingsGoal = {
        ...mockSavingsGoals[0],
        current_amount: 2000,
      };
      configureAllGoalsQuery({ data: mockSavingsGoals });
      configureActiveGoalsQuery({ data: [] });
      mockUpdateMutateAsync.mockResolvedValue(updatedGoal);

      const updateRequest: UpdateSavingsGoalRequest = {
        current_amount: 2000,
      };

      const { result } = renderHook(() => useSavingsGoals(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      let returnedGoal: SavingsGoal | undefined;
      await act(async () => {
        returnedGoal = await result.current.updateGoal('goal-1', updateRequest);
      });

      expect(mockUpdateMutateAsync).toHaveBeenCalledWith({ id: 'goal-1', data: updateRequest });
      expect(returnedGoal).toEqual(updatedGoal);
    });

    it('should handle update errors', async () => {
      const error = new Error('Failed to update savings goal');
      configureAllGoalsQuery({ data: mockSavingsGoals });
      configureActiveGoalsQuery({ data: [] });
      mockUpdateMutateAsync.mockRejectedValue(error);

      const updateRequest: UpdateSavingsGoalRequest = {
        current_amount: 2000,
      };

      const { result } = renderHook(() => useSavingsGoals(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      await act(async () => {
        await expect(result.current.updateGoal('goal-1', updateRequest)).rejects.toThrow(error);
      });
    });
  });

  describe('Delete Savings Goal', () => {
    it('should delete a savings goal', async () => {
      configureAllGoalsQuery({ data: mockSavingsGoals });
      configureActiveGoalsQuery({ data: [] });
      mockDeleteMutateAsync.mockResolvedValue(undefined);

      const { result } = renderHook(() => useSavingsGoals(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      await act(async () => {
        await result.current.deleteGoal('goal-1');
      });

      expect(mockDeleteMutateAsync).toHaveBeenCalledWith({ id: 'goal-1' });
    });

    it('should handle delete errors', async () => {
      const error = new Error('Failed to delete savings goal');
      configureAllGoalsQuery({ data: mockSavingsGoals });
      configureActiveGoalsQuery({ data: [] });
      mockDeleteMutateAsync.mockRejectedValue(error);

      const { result } = renderHook(() => useSavingsGoals(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      await act(async () => {
        await expect(result.current.deleteGoal('goal-1')).rejects.toThrow(error);
      });
    });
  });

  describe('Computed Values', () => {
    it('should compute progress percentage for each goal', async () => {
      configureAllGoalsQuery({ data: mockSavingsGoals });
      configureActiveGoalsQuery({ data: [] });

      const { result } = renderHook(() => useSavingsGoals(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.goalsWithProgress).toBeDefined();
      expect(result.current.goalsWithProgress[0].progress).toBe(30); // 1500/5000 * 100
      expect(result.current.goalsWithProgress[1].progress).toBe(80); // 8000/10000 * 100
    });

    it('should compute total progress across all goals', async () => {
      configureAllGoalsQuery({ data: mockSavingsGoals });
      configureActiveGoalsQuery({ data: [] });

      const { result } = renderHook(() => useSavingsGoals(), {
        wrapper: createWrapper(),
      });

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.totalTarget).toBe(15000); // 5000 + 10000
      expect(result.current.totalSaved).toBe(9500); // 1500 + 8000
    });
  });
});
