/**
 * useCurrentBudget Hook Tests
 *
 * TDD RED: These tests are written first and will initially fail.
 * Tests the React hook for fetching the current active budget.
 * Issue #564: Migrated to generated React Query hooks
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { useCurrentBudget } from './useCurrentBudget';
import type { Budget } from '../types';

// Mock the generated hook
const mockRefetch = vi.fn();
vi.mock('@/api/generated/endpoints/budgets/budgets', () => ({
  useGetCurrentBudget: vi.fn(),
  getGetCurrentBudgetQueryKey: vi.fn(() => ['budgets', 'current']),
}));

// Import the mock after vi.mock
import { useGetCurrentBudget } from '@/api/generated/endpoints/budgets/budgets';

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
    vi.clearAllMocks();
    mockRefetch.mockClear();
  });

  // Helper to configure the mock for loading state
  function configureLoading() {
    vi.mocked(useGetCurrentBudget).mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
      refetch: mockRefetch,
    } as unknown as ReturnType<typeof useGetCurrentBudget>);
  }

  // Helper to configure the mock for success state
  function configureSuccess(budget: Budget) {
    vi.mocked(useGetCurrentBudget).mockReturnValue({
      data: budget,
      isLoading: false,
      error: null,
      refetch: mockRefetch,
    } as unknown as ReturnType<typeof useGetCurrentBudget>);
  }

  // Helper to configure the mock for 404 error
  function configure404Error() {
    vi.mocked(useGetCurrentBudget).mockReturnValue({
      data: undefined,
      isLoading: false,
      error: { response: { status: 404 } },
      refetch: mockRefetch,
    } as unknown as ReturnType<typeof useGetCurrentBudget>);
  }

  // Helper to configure the mock for error state
  function configureError(error: Error) {
    vi.mocked(useGetCurrentBudget).mockReturnValue({
      data: undefined,
      isLoading: false,
      error,
      refetch: mockRefetch,
    } as unknown as ReturnType<typeof useGetCurrentBudget>);
  }

  describe('Initial State', () => {
    it('should return loading state initially', () => {
      configureLoading();

      const { result } = renderHook(() => useCurrentBudget(), {
        wrapper: createWrapper(),
      });

      expect(result.current.loading).toBe(true);
      expect(result.current.budget).toBeNull();
      expect(result.current.error).toBeNull();
    });

    it('should return current budget on successful fetch', () => {
      configureSuccess(mockBudget);

      const { result } = renderHook(() => useCurrentBudget(), {
        wrapper: createWrapper(),
      });

      expect(result.current.loading).toBe(false);
      expect(result.current.budget).toEqual(mockBudget);
      expect(result.current.error).toBeNull();
    });

    it('should handle 404 when no current budget exists', () => {
      configure404Error();

      const { result } = renderHook(() => useCurrentBudget(), {
        wrapper: createWrapper(),
      });

      expect(result.current.loading).toBe(false);
      expect(result.current.budget).toBeNull();
      expect(result.current.hasNoBudget).toBe(true);
      expect(result.current.error).toBeNull(); // 404 should not be an error
    });

    it('should handle fetch errors gracefully', () => {
      const error = new Error('Failed to fetch current budget');
      configureError(error);

      const { result } = renderHook(() => useCurrentBudget(), {
        wrapper: createWrapper(),
      });

      expect(result.current.loading).toBe(false);
      expect(result.current.error).toEqual(error);
      expect(result.current.budget).toBeNull();
    });
  });

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
      };
      configureSuccess(budgetWithCategories);

      const { result } = renderHook(() => useCurrentBudget(), {
        wrapper: createWrapper(),
      });

      expect(result.current.metrics).toBeDefined();
      expect(result.current.metrics?.total_allocated).toBe(700); // 400 + 300
      expect(result.current.metrics?.total_spent).toBe(300); // 200 + 100
      expect(result.current.metrics?.remaining_amount).toBe(700); // 1000 - 300
      expect(result.current.metrics?.percent_used).toBe(30); // (300/1000) * 100
      expect(result.current.metrics?.is_over_budget).toBe(false);
    });

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
      };
      configureSuccess(overBudget);

      const { result } = renderHook(() => useCurrentBudget(), {
        wrapper: createWrapper(),
      });

      expect(result.current.metrics?.is_over_budget).toBe(true);
      expect(result.current.metrics?.percent_used).toBe(120); // 600/500 * 100
    });

    it('should return null metrics when no budget exists', () => {
      configure404Error();

      const { result } = renderHook(() => useCurrentBudget(), {
        wrapper: createWrapper(),
      });

      expect(result.current.metrics).toBeNull();
    });
  });

  describe('Refetch', () => {
    it('should provide refetch function to reload data', async () => {
      configureSuccess(mockBudget);

      const { result } = renderHook(() => useCurrentBudget(), {
        wrapper: createWrapper(),
      });

      await act(async () => {
        await result.current.refetch();
      });

      await waitFor(() => {
        expect(mockRefetch).toHaveBeenCalled();
      });
    });
  });
});
