/**
 * useAvailableCategories Hook Tests
 * Issue #497: Budget Category Manager + Match Config UI
 *
 * TDD RED Phase: Write failing tests first
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';
import { useAvailableCategories } from './useAvailableCategories';
import * as budgetsApi from '../api/budgetsApi';

// Mock the API module
vi.mock('../api/budgetsApi');

describe('useAvailableCategories', () => {
  // API returns flat strings like "Food and Drink > Restaurants"
  const mockFlatCategories: string[] = [
    'Food and Drink > Restaurants',
    'Food and Drink > Groceries',
    'Transportation > Gas',
    'Transportation > Public Transit',
    'Shopping > Clothing',
  ];

  // Expected transformed result: string[][] format
  const expectedCategories: string[][] = [
    ['Food and Drink', 'Restaurants'],
    ['Food and Drink', 'Groceries'],
    ['Transportation', 'Gas'],
    ['Transportation', 'Public Transit'],
    ['Shopping', 'Clothing'],
  ];

  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  describe('Initial State', () => {
    it('should return loading=true initially', () => {
      vi.mocked(budgetsApi.fetchAvailableCategories).mockImplementation(
        () => new Promise(() => {}) // Never resolves
      );

      const { result } = renderHook(() => useAvailableCategories());

      expect(result.current.loading).toBe(true);
      expect(result.current.categories).toEqual([]);
      expect(result.current.error).toBeNull();
    });
  });

  describe('Fetching Categories', () => {
    it('should return categories on successful fetch', async () => {
      vi.mocked(budgetsApi.fetchAvailableCategories).mockResolvedValue(mockFlatCategories);

      const { result } = renderHook(() => useAvailableCategories());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(budgetsApi.fetchAvailableCategories).toHaveBeenCalledTimes(1);
      expect(result.current.categories).toEqual(expectedCategories);
      expect(result.current.error).toBeNull();
    });

    it('should transform flat strings to string[][] format', async () => {
      vi.mocked(budgetsApi.fetchAvailableCategories).mockResolvedValue([
        'Level1 > Level2 > Level3',
      ]);

      const { result } = renderHook(() => useAvailableCategories());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.categories).toEqual([
        ['Level1', 'Level2', 'Level3'],
      ]);
    });

    it('should handle single-level categories', async () => {
      vi.mocked(budgetsApi.fetchAvailableCategories).mockResolvedValue([
        'SingleCategory',
      ]);

      const { result } = renderHook(() => useAvailableCategories());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.categories).toEqual([['SingleCategory']]);
    });

    it('should trim whitespace from category parts', async () => {
      vi.mocked(budgetsApi.fetchAvailableCategories).mockResolvedValue([
        '  Food and Drink  >  Restaurants  ',
      ]);

      const { result } = renderHook(() => useAvailableCategories());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.categories).toEqual([
        ['Food and Drink', 'Restaurants'],
      ]);
    });
  });

  describe('Error Handling', () => {
    it('should return error on failed fetch', async () => {
      const testError = new Error('Network error');
      vi.mocked(budgetsApi.fetchAvailableCategories).mockRejectedValue(testError);

      const { result } = renderHook(() => useAvailableCategories());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.error).toBeTruthy();
      expect(result.current.error?.message).toBe('Network error');
      expect(result.current.categories).toEqual([]);
    });

    it('should wrap non-Error objects in Error', async () => {
      vi.mocked(budgetsApi.fetchAvailableCategories).mockRejectedValue('String error');

      const { result } = renderHook(() => useAvailableCategories());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.error).toBeTruthy();
      expect(result.current.error?.message).toBe('Failed to fetch categories');
    });
  });

  describe('Refetch', () => {
    it('should provide refetch function', async () => {
      vi.mocked(budgetsApi.fetchAvailableCategories).mockResolvedValue(mockFlatCategories);

      const { result } = renderHook(() => useAvailableCategories());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(typeof result.current.refetch).toBe('function');
    });

    it('should refetch data when called', async () => {
      const updatedCategories = [...mockFlatCategories, 'Entertainment > Movies'];

      vi.mocked(budgetsApi.fetchAvailableCategories)
        .mockResolvedValueOnce(mockFlatCategories)
        .mockResolvedValueOnce(updatedCategories);

      const { result } = renderHook(() => useAvailableCategories());

      // Wait for initial load
      await waitFor(() => {
        expect(result.current.categories.length).toBe(5);
      });

      // Call refetch
      await act(async () => {
        await result.current.refetch();
      });

      expect(result.current.categories.length).toBe(6);
      expect(budgetsApi.fetchAvailableCategories).toHaveBeenCalledTimes(2);
    });

    it('should clear error on successful refetch', async () => {
      vi.mocked(budgetsApi.fetchAvailableCategories)
        .mockRejectedValueOnce(new Error('First error'))
        .mockResolvedValueOnce(mockFlatCategories);

      const { result } = renderHook(() => useAvailableCategories());

      // Wait for error
      await waitFor(() => {
        expect(result.current.error).toBeTruthy();
      });

      // Refetch
      await act(async () => {
        await result.current.refetch();
      });

      expect(result.current.error).toBeNull();
      expect(result.current.categories).toEqual(expectedCategories);
    });
  });

  describe('Edge Cases', () => {
    it('should handle empty categories array', async () => {
      vi.mocked(budgetsApi.fetchAvailableCategories).mockResolvedValue([]);

      const { result } = renderHook(() => useAvailableCategories());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.categories).toEqual([]);
      expect(result.current.error).toBeNull();
    });

    it('should filter out empty strings', async () => {
      vi.mocked(budgetsApi.fetchAvailableCategories).mockResolvedValue([
        'Food and Drink > Restaurants',
        '',
        'Transportation > Gas',
      ]);

      const { result } = renderHook(() => useAvailableCategories());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.categories).toEqual([
        ['Food and Drink', 'Restaurants'],
        ['Transportation', 'Gas'],
      ]);
    });
  });
});
