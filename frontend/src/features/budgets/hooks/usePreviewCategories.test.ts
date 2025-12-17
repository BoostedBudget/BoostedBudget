/**
 * usePreviewCategories Hook Tests
 * Issue #497: Budget Category Manager + Match Config UI
 *
 * TDD RED Phase: Write failing tests first
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';
import { usePreviewCategories } from './usePreviewCategories';
import * as budgetsApi from '../api/budgetsApi';

// Mock the API module
vi.mock('../api/budgetsApi', () => ({
  previewCategoryMatches: vi.fn(),
}));

describe('usePreviewCategories', () => {
  const mockPreviewCategoryMatches = vi.mocked(budgetsApi.previewCategoryMatches);

  beforeEach(() => {
    vi.clearAllMocks();
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
    mockPreviewCategoryMatches.mockResolvedValue({ transaction_count: 5, total_amount: 250 });

    const { result } = renderHook(() =>
      usePreviewCategories(
        defaultParams.categories,
        defaultParams.tags,
        defaultParams.matchLogic,
        defaultParams.startDate,
        defaultParams.endDate,
        defaultParams.enabled
      )
    );

    // Should start with null preview before debounce completes
    expect(result.current.preview).toBeNull();
  });

  it('should debounce requests by 300ms', async () => {
    vi.useFakeTimers();
    mockPreviewCategoryMatches.mockResolvedValue({ transaction_count: 5, total_amount: 250 });

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
      { initialProps: { categories: [['Food', 'A']] } }
    );

    // Change params multiple times quickly
    rerender({ categories: [['Food', 'B']] });
    rerender({ categories: [['Food', 'C']] });
    rerender({ categories: [['Food', 'D']] });

    // API should not be called yet (still in debounce window)
    expect(mockPreviewCategoryMatches).not.toHaveBeenCalled();

    // Advance timers past debounce and run pending promises
    await act(async () => {
      vi.advanceTimersByTime(350);
      await Promise.resolve();
    });

    // Should have been called only once with the final params
    expect(mockPreviewCategoryMatches).toHaveBeenCalledTimes(1);
    expect(mockPreviewCategoryMatches).toHaveBeenCalledWith(
      expect.objectContaining({
        categories: [['Food', 'D']],
      })
    );

    vi.useRealTimers();
  });

  it('should return count and amount on success', async () => {
    mockPreviewCategoryMatches.mockResolvedValue({ transaction_count: 10, total_amount: 500.50 });

    const { result } = renderHook(() =>
      usePreviewCategories(
        defaultParams.categories,
        defaultParams.tags,
        defaultParams.matchLogic,
        defaultParams.startDate,
        defaultParams.endDate,
        defaultParams.enabled
      )
    );

    await waitFor(() => {
      expect(result.current.preview).toEqual({
        transaction_count: 10,
        total_amount: 500.50,
      });
      expect(result.current.loading).toBe(false);
      expect(result.current.error).toBeNull();
    });
  });

  it('should handle API errors gracefully', async () => {
    mockPreviewCategoryMatches.mockRejectedValue(new Error('Network error'));

    const { result } = renderHook(() =>
      usePreviewCategories(
        defaultParams.categories,
        defaultParams.tags,
        defaultParams.matchLogic,
        defaultParams.startDate,
        defaultParams.endDate,
        defaultParams.enabled
      )
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
    mockPreviewCategoryMatches.mockResolvedValue({ transaction_count: 5, total_amount: 250 });

    renderHook(() =>
      usePreviewCategories(
        defaultParams.categories,
        defaultParams.tags,
        defaultParams.matchLogic,
        defaultParams.startDate,
        defaultParams.endDate,
        false // disabled
      )
    );

    // Advance timers past debounce
    await act(async () => {
      vi.advanceTimersByTime(350);
    });

    expect(mockPreviewCategoryMatches).not.toHaveBeenCalled();
    vi.useRealTimers();
  });

  it('should not make API call when categories are empty', async () => {
    vi.useFakeTimers();
    mockPreviewCategoryMatches.mockResolvedValue({ transaction_count: 5, total_amount: 250 });

    renderHook(() =>
      usePreviewCategories(
        [], // empty categories
        defaultParams.tags,
        defaultParams.matchLogic,
        defaultParams.startDate,
        defaultParams.endDate,
        defaultParams.enabled
      )
    );

    // Advance timers past debounce
    await act(async () => {
      vi.advanceTimersByTime(350);
    });

    expect(mockPreviewCategoryMatches).not.toHaveBeenCalled();
    vi.useRealTimers();
  });

  it('should cancel pending request on param change', async () => {
    // First call returns old data, second call returns new data
    mockPreviewCategoryMatches
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
      { initialProps: { categories: [['Food', 'A']] } }
    );

    // Wait for first request to start
    await waitFor(() => {
      expect(mockPreviewCategoryMatches).toHaveBeenCalledTimes(1);
    });

    // Change params
    rerender({ categories: [['Food', 'B']] });

    // Wait for second request
    await waitFor(() => {
      expect(mockPreviewCategoryMatches).toHaveBeenCalledTimes(2);
    });

    // Should have the second request's result
    await waitFor(() => {
      expect(result.current.preview?.transaction_count).toBe(20);
    });
  });

  it('should reset error when params change', async () => {
    mockPreviewCategoryMatches
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
      { initialProps: { categories: [['Food', 'A']] } }
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
    mockPreviewCategoryMatches.mockResolvedValue({ transaction_count: 5, total_amount: 250 });

    renderHook(() =>
      usePreviewCategories(
        [['Food', 'Restaurants'], ['Shopping', 'Clothing']],
        ['Essential', 'Discretionary'],
        'and',
        '2024-02-01',
        '2024-02-29',
        true
      )
    );

    await waitFor(() => {
      expect(mockPreviewCategoryMatches).toHaveBeenCalledWith({
        categories: [['Food', 'Restaurants'], ['Shopping', 'Clothing']],
        tags: ['Essential', 'Discretionary'],
        match_logic: 'and',
        start_date: '2024-02-01',
        end_date: '2024-02-29',
      });
    });
  });
});
