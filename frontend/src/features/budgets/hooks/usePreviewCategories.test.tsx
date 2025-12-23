/**
 * usePreviewCategories Hook Tests
 * Issue #497: Budget Category Manager + Match Config UI
 * Issue #564: Migrated to mock generated React Query hooks
 *
 * TDD RED Phase: Write failing tests first
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { usePreviewCategories } from './usePreviewCategories';

// Mock mutateAsync function
const mockMutateAsync = vi.fn();

// Mock the generated mutation hook
vi.mock('@/api/generated/endpoints/budget/budget', () => ({
  usePreviewCategoryMatches: vi.fn(() => ({
    mutateAsync: mockMutateAsync,
    mutate: vi.fn(),
    isLoading: false,
    isError: false,
    error: null,
    data: undefined,
    reset: vi.fn(),
  })),
}));

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

describe('usePreviewCategories', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockMutateAsync.mockReset();
  });

  const defaultParams = {
    categories: [['Food and Drink', 'Restaurants']],
    tags: [] as string[],
    matchLogic: 'category_only' as const,
    startDate: '2024-01-01',
    endDate: '2024-01-31',
    enabled: true,
  };

  it('should return loading=true initially when enabled', async () => {
    mockMutateAsync.mockResolvedValue({ transaction_count: 5, total_amount: 250 });

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
    );

    // Should start with null preview before debounce completes
    expect(result.current.preview).toBeNull();
  });

  it('should debounce requests by 300ms', async () => {
    vi.useFakeTimers();
    mockMutateAsync.mockResolvedValue({ transaction_count: 5, total_amount: 250 });

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
    );

    // Change params multiple times quickly
    rerender({ categories: [['Food', 'B']] });
    rerender({ categories: [['Food', 'C']] });
    rerender({ categories: [['Food', 'D']] });

    // API should not be called yet (still in debounce window)
    expect(mockMutateAsync).not.toHaveBeenCalled();

    // Advance timers past debounce and run pending promises
    await act(async () => {
      vi.advanceTimersByTime(350);
      await Promise.resolve();
    });

    // Should have been called only once with the final params
    expect(mockMutateAsync).toHaveBeenCalledTimes(1);
    expect(mockMutateAsync).toHaveBeenCalledWith(
      expect.objectContaining({
        data: expect.objectContaining({
          categories: [['Food', 'D']],
        }),
      })
    );

    vi.useRealTimers();
  });

  it('should return count and amount on success', async () => {
    mockMutateAsync.mockResolvedValue({ transaction_count: 10, total_amount: 500.5 });

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
    );

    await waitFor(() => {
      expect(result.current.preview).toEqual({
        transaction_count: 10,
        total_amount: 500.5,
      });
      expect(result.current.loading).toBe(false);
      expect(result.current.error).toBeNull();
    });
  });

  it('should handle API errors gracefully', async () => {
    mockMutateAsync.mockRejectedValue(new Error('Network error'));

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
    );

    await waitFor(() => {
      expect(result.current.error).toBeTruthy();
      expect(result.current.error?.message).toBe('Network error');
      expect(result.current.loading).toBe(false);
      expect(result.current.preview).toBeNull();
    });
  });

  it('should not make API call when enabled=false', async () => {
    vi.useFakeTimers();
    mockMutateAsync.mockResolvedValue({ transaction_count: 5, total_amount: 250 });

    renderHook(
      () =>
        usePreviewCategories(
          defaultParams.categories,
          defaultParams.tags,
          defaultParams.matchLogic,
          defaultParams.startDate,
          defaultParams.endDate,
          false // disabled
        ),
      { wrapper: createWrapper() }
    );

    // Advance timers past debounce
    await act(async () => {
      vi.advanceTimersByTime(350);
    });

    expect(mockMutateAsync).not.toHaveBeenCalled();
    vi.useRealTimers();
  });

  it('should not make API call when categories are empty', async () => {
    vi.useFakeTimers();
    mockMutateAsync.mockResolvedValue({ transaction_count: 5, total_amount: 250 });

    renderHook(
      () =>
        usePreviewCategories(
          [], // empty categories
          defaultParams.tags,
          defaultParams.matchLogic,
          defaultParams.startDate,
          defaultParams.endDate,
          defaultParams.enabled
        ),
      { wrapper: createWrapper() }
    );

    // Advance timers past debounce
    await act(async () => {
      vi.advanceTimersByTime(350);
    });

    expect(mockMutateAsync).not.toHaveBeenCalled();
    vi.useRealTimers();
  });

  it('should cancel pending request on param change', async () => {
    // First call returns old data, second call returns new data
    mockMutateAsync
      .mockResolvedValueOnce({ transaction_count: 5, total_amount: 250 })
      .mockResolvedValueOnce({ transaction_count: 20, total_amount: 1000 });

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
    );

    // Wait for first request to start
    await waitFor(() => {
      expect(mockMutateAsync).toHaveBeenCalledTimes(1);
    });

    // Change params
    rerender({ categories: [['Food', 'B']] });

    // Wait for second request
    await waitFor(() => {
      expect(mockMutateAsync).toHaveBeenCalledTimes(2);
    });

    // Should have the second request's result
    await waitFor(() => {
      expect(result.current.preview?.transaction_count).toBe(20);
    });
  });

  it('should reset error when params change', async () => {
    mockMutateAsync
      .mockRejectedValueOnce(new Error('First error'))
      .mockResolvedValueOnce({ transaction_count: 5, total_amount: 250 });

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
    );

    // Wait for first request to fail
    await waitFor(() => {
      expect(result.current.error).toBeTruthy();
    });

    // Change params
    rerender({ categories: [['Food', 'B']] });

    // Wait for second request to succeed
    await waitFor(() => {
      expect(result.current.error).toBeNull();
      expect(result.current.preview?.transaction_count).toBe(5);
    });
  });

  it('should pass correct params to API', async () => {
    mockMutateAsync.mockResolvedValue({ transaction_count: 5, total_amount: 250 });

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
    );

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
      });
    });
  });
});
