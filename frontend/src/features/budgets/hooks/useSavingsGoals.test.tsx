/**
 * useSavingsGoals Hook Tests
 *
 * TDD RED: These tests are written first and will initially fail.
 * Tests the React hook for savings goal CRUD operations.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';
import { useSavingsGoals } from './useSavingsGoals';
import * as budgetsApi from '../api/budgetsApi';
import type { SavingsGoal, CreateSavingsGoalRequest, UpdateSavingsGoalRequest } from '../types';

// Mock the API module
vi.mock('../api/budgetsApi');

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
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  describe('Initial State', () => {
    it('should return loading state initially', () => {
      // Arrange
      vi.mocked(budgetsApi.fetchSavingsGoals).mockImplementation(
        () => new Promise(() => {}) // Never resolves
      );

      // Act
      const { result } = renderHook(() => useSavingsGoals());

      // Assert
      expect(result.current.loading).toBe(true);
      expect(result.current.goals).toEqual([]);
      expect(result.current.error).toBeNull();
    });

    it('should fetch and return savings goals on mount', async () => {
      // Arrange
      vi.mocked(budgetsApi.fetchSavingsGoals).mockResolvedValue(mockSavingsGoals);

      // Act
      const { result } = renderHook(() => useSavingsGoals());

      // Assert - Initial loading state
      expect(result.current.loading).toBe(true);

      // Wait for data to load
      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Assert - Data loaded successfully
      expect(result.current.goals).toEqual(mockSavingsGoals);
      expect(result.current.error).toBeNull();
      expect(budgetsApi.fetchSavingsGoals).toHaveBeenCalledTimes(1);
    });

    it('should handle fetch errors gracefully', async () => {
      // Arrange
      const error = new Error('Failed to fetch savings goals');
      vi.mocked(budgetsApi.fetchSavingsGoals).mockRejectedValue(error);

      // Act
      const { result } = renderHook(() => useSavingsGoals());

      // Wait for error state
      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Assert
      expect(result.current.error).toEqual(error);
      expect(result.current.goals).toEqual([]);
    });

    it('should return empty array when no goals exist', async () => {
      // Arrange
      vi.mocked(budgetsApi.fetchSavingsGoals).mockResolvedValue([]);

      // Act
      const { result } = renderHook(() => useSavingsGoals());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Assert
      expect(result.current.goals).toEqual([]);
      expect(result.current.error).toBeNull();
    });
  });

  describe('Active Goals Filter', () => {
    it('should filter active goals when activeOnly option is true', async () => {
      // Arrange
      const activeGoals = [mockSavingsGoals[0]];
      vi.mocked(budgetsApi.fetchActiveSavingsGoals).mockResolvedValue(activeGoals);

      // Act
      const { result } = renderHook(() => useSavingsGoals({ activeOnly: true }));

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Assert
      expect(budgetsApi.fetchActiveSavingsGoals).toHaveBeenCalledTimes(1);
      expect(result.current.goals).toEqual(activeGoals);
    });
  });

  describe('Refetch', () => {
    it('should provide refetch function to reload data', async () => {
      // Arrange
      vi.mocked(budgetsApi.fetchSavingsGoals).mockResolvedValue(mockSavingsGoals);

      // Act
      const { result } = renderHook(() => useSavingsGoals());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(budgetsApi.fetchSavingsGoals).toHaveBeenCalledTimes(1);

      // Call refetch
      await act(async () => {
        await result.current.refetch();
      });

      // Assert
      expect(budgetsApi.fetchSavingsGoals).toHaveBeenCalledTimes(2);
    });
  });

  describe('Create Savings Goal', () => {
    it('should create a new savings goal and add it to the list', async () => {
      // Arrange
      vi.mocked(budgetsApi.fetchSavingsGoals).mockResolvedValue([]);
      vi.mocked(budgetsApi.createSavingsGoal).mockResolvedValue(mockSavingsGoals[0]);

      const createRequest: CreateSavingsGoalRequest = {
        name: 'Vacation Fund',
        target_amount: 5000,
        account_id: 'account-789',
      };

      // Act
      const { result } = renderHook(() => useSavingsGoals());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.goals).toHaveLength(0);

      // Create goal
      let createdGoal: SavingsGoal | undefined;
      await act(async () => {
        createdGoal = await result.current.createGoal(createRequest);
      });

      // Assert
      expect(budgetsApi.createSavingsGoal).toHaveBeenCalledWith(createRequest);
      expect(createdGoal).toEqual(mockSavingsGoals[0]);
      expect(result.current.goals).toHaveLength(1);
      expect(result.current.goals[0]).toEqual(mockSavingsGoals[0]);
    });

    it('should handle create errors and not add goal to list', async () => {
      // Arrange
      const error = new Error('Failed to create savings goal');
      vi.mocked(budgetsApi.fetchSavingsGoals).mockResolvedValue([]);
      vi.mocked(budgetsApi.createSavingsGoal).mockRejectedValue(error);

      const createRequest: CreateSavingsGoalRequest = {
        name: 'Vacation Fund',
        target_amount: 5000,
        account_id: 'account-789',
      };

      // Act
      const { result } = renderHook(() => useSavingsGoals());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Try to create goal
      await act(async () => {
        await expect(result.current.createGoal(createRequest)).rejects.toThrow(error);
      });

      // Assert - goals list should remain empty
      expect(result.current.goals).toHaveLength(0);
    });
  });

  describe('Update Savings Goal', () => {
    it('should update an existing savings goal in the list', async () => {
      // Arrange
      const updatedGoal: SavingsGoal = {
        ...mockSavingsGoals[0],
        current_amount: 2000,
      };
      vi.mocked(budgetsApi.fetchSavingsGoals).mockResolvedValue(mockSavingsGoals);
      vi.mocked(budgetsApi.updateSavingsGoal).mockResolvedValue(updatedGoal);

      const updateRequest: UpdateSavingsGoalRequest = {
        current_amount: 2000,
      };

      // Act
      const { result } = renderHook(() => useSavingsGoals());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Update goal
      let returnedGoal: SavingsGoal | undefined;
      await act(async () => {
        returnedGoal = await result.current.updateGoal('goal-1', updateRequest);
      });

      // Assert
      expect(budgetsApi.updateSavingsGoal).toHaveBeenCalledWith('goal-1', updateRequest);
      expect(returnedGoal).toEqual(updatedGoal);
      expect(result.current.goals[0].current_amount).toBe(2000);
      // Other goals should remain unchanged
      expect(result.current.goals[1]).toEqual(mockSavingsGoals[1]);
    });

    it('should handle update errors without modifying the list', async () => {
      // Arrange
      const error = new Error('Failed to update savings goal');
      vi.mocked(budgetsApi.fetchSavingsGoals).mockResolvedValue(mockSavingsGoals);
      vi.mocked(budgetsApi.updateSavingsGoal).mockRejectedValue(error);

      const updateRequest: UpdateSavingsGoalRequest = {
        current_amount: 2000,
      };

      // Act
      const { result } = renderHook(() => useSavingsGoals());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Try to update goal
      await act(async () => {
        await expect(result.current.updateGoal('goal-1', updateRequest)).rejects.toThrow(error);
      });

      // Assert - goal should remain unchanged
      expect(result.current.goals[0].current_amount).toBe(1500);
    });
  });

  describe('Delete Savings Goal', () => {
    it('should delete a savings goal and remove it from the list', async () => {
      // Arrange
      vi.mocked(budgetsApi.fetchSavingsGoals).mockResolvedValue(mockSavingsGoals);
      vi.mocked(budgetsApi.deleteSavingsGoal).mockResolvedValue(undefined);

      // Act
      const { result } = renderHook(() => useSavingsGoals());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.goals).toHaveLength(2);

      // Delete goal
      await act(async () => {
        await result.current.deleteGoal('goal-1');
      });

      // Assert
      expect(budgetsApi.deleteSavingsGoal).toHaveBeenCalledWith('goal-1');
      expect(result.current.goals).toHaveLength(1);
      expect(result.current.goals[0].id).toBe('goal-2');
    });

    it('should handle delete errors without removing goal from list', async () => {
      // Arrange
      const error = new Error('Failed to delete savings goal');
      vi.mocked(budgetsApi.fetchSavingsGoals).mockResolvedValue(mockSavingsGoals);
      vi.mocked(budgetsApi.deleteSavingsGoal).mockRejectedValue(error);

      // Act
      const { result } = renderHook(() => useSavingsGoals());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Try to delete goal
      await act(async () => {
        await expect(result.current.deleteGoal('goal-1')).rejects.toThrow(error);
      });

      // Assert - goal should still be in the list
      expect(result.current.goals).toHaveLength(2);
    });
  });

  describe('Computed Values', () => {
    it('should compute progress percentage for each goal', async () => {
      // Arrange
      vi.mocked(budgetsApi.fetchSavingsGoals).mockResolvedValue(mockSavingsGoals);

      // Act
      const { result } = renderHook(() => useSavingsGoals());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Assert
      expect(result.current.goalsWithProgress).toBeDefined();
      expect(result.current.goalsWithProgress[0].progress).toBe(30); // 1500/5000 * 100
      expect(result.current.goalsWithProgress[1].progress).toBe(80); // 8000/10000 * 100
    });

    it('should compute total progress across all goals', async () => {
      // Arrange
      vi.mocked(budgetsApi.fetchSavingsGoals).mockResolvedValue(mockSavingsGoals);

      // Act
      const { result } = renderHook(() => useSavingsGoals());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      // Assert
      expect(result.current.totalTarget).toBe(15000); // 5000 + 10000
      expect(result.current.totalSaved).toBe(9500); // 1500 + 8000
    });
  });
});
