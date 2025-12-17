/**
 * useTags Hook Tests
 * Issue #497: Budget Category Manager + Match Config UI
 *
 * TDD RED Phase: Write failing tests first
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';
import { useTags } from './useTags';
import * as tagsApi from '../api/tagsApi';
import type { UserTag } from '../types';

// Mock the API module
vi.mock('../api/tagsApi');

describe('useTags', () => {
  // Tags are already sorted alphabetically for easier testing
  const mockTags: UserTag[] = [
    {
      id: 'tag-2',
      user_id: 'user-1',
      name: 'Discretionary',
      color: '#33FF57',
      usage_count: 3,
      created_at: '2024-01-01T00:00:00Z',
      updated_at: '2024-01-01T00:00:00Z',
    },
    {
      id: 'tag-1',
      user_id: 'user-1',
      name: 'Essential',
      color: '#FF5733',
      usage_count: 5,
      created_at: '2024-01-01T00:00:00Z',
      updated_at: '2024-01-01T00:00:00Z',
    },
  ];

  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  describe('Initial State', () => {
    it('should return loading=true initially', () => {
      vi.mocked(tagsApi.fetchTags).mockImplementation(
        () => new Promise(() => {}) // Never resolves
      );

      const { result } = renderHook(() => useTags());

      expect(result.current.loading).toBe(true);
      expect(result.current.tags).toEqual([]);
      expect(result.current.error).toBeNull();
    });
  });

  describe('Fetching Tags', () => {
    it('should return tags on successful fetch', async () => {
      vi.mocked(tagsApi.fetchTags).mockResolvedValue(mockTags);

      const { result } = renderHook(() => useTags());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(tagsApi.fetchTags).toHaveBeenCalledTimes(1);
      expect(result.current.tags).toEqual(mockTags);
      expect(result.current.error).toBeNull();
    });

    it('should sort tags alphabetically by name', async () => {
      const unsortedTags: UserTag[] = [
        { ...mockTags[0], name: 'Zebra' },
        { ...mockTags[1], name: 'Alpha' },
      ];
      vi.mocked(tagsApi.fetchTags).mockResolvedValue(unsortedTags);

      const { result } = renderHook(() => useTags());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.tags[0].name).toBe('Alpha');
      expect(result.current.tags[1].name).toBe('Zebra');
    });
  });

  describe('Error Handling', () => {
    it('should return error on failed fetch', async () => {
      const testError = new Error('Network error');
      vi.mocked(tagsApi.fetchTags).mockRejectedValue(testError);

      const { result } = renderHook(() => useTags());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.error).toBeTruthy();
      expect(result.current.error?.message).toBe('Network error');
      expect(result.current.tags).toEqual([]);
    });

    it('should wrap non-Error objects in Error', async () => {
      vi.mocked(tagsApi.fetchTags).mockRejectedValue('String error');

      const { result } = renderHook(() => useTags());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.error).toBeTruthy();
      expect(result.current.error?.message).toBe('Failed to fetch tags');
    });
  });

  describe('Refetch', () => {
    it('should provide refetch function', async () => {
      vi.mocked(tagsApi.fetchTags).mockResolvedValue(mockTags);

      const { result } = renderHook(() => useTags());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(typeof result.current.refetch).toBe('function');
    });

    it('should refetch data when called', async () => {
      const updatedTags: UserTag[] = [
        ...mockTags,
        {
          id: 'tag-3',
          user_id: 'user-1',
          name: 'New Tag',
          color: '#0000FF',
          usage_count: 0,
          created_at: '2024-01-01T00:00:00Z',
          updated_at: '2024-01-01T00:00:00Z',
        },
      ];

      vi.mocked(tagsApi.fetchTags)
        .mockResolvedValueOnce(mockTags)
        .mockResolvedValueOnce(updatedTags);

      const { result } = renderHook(() => useTags());

      // Wait for initial load
      await waitFor(() => {
        expect(result.current.tags.length).toBe(2);
      });

      // Call refetch
      await act(async () => {
        await result.current.refetch();
      });

      expect(result.current.tags.length).toBe(3);
      expect(tagsApi.fetchTags).toHaveBeenCalledTimes(2);
    });

    it('should clear error on successful refetch', async () => {
      vi.mocked(tagsApi.fetchTags)
        .mockRejectedValueOnce(new Error('First error'))
        .mockResolvedValueOnce(mockTags);

      const { result } = renderHook(() => useTags());

      // Wait for error
      await waitFor(() => {
        expect(result.current.error).toBeTruthy();
      });

      // Refetch
      await act(async () => {
        await result.current.refetch();
      });

      expect(result.current.error).toBeNull();
      expect(result.current.tags).toEqual(mockTags);
    });
  });

  describe('Edge Cases', () => {
    it('should handle empty tags array', async () => {
      vi.mocked(tagsApi.fetchTags).mockResolvedValue([]);

      const { result } = renderHook(() => useTags());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.tags).toEqual([]);
      expect(result.current.error).toBeNull();
    });

    it('should handle tags with null color', async () => {
      const tagsWithNullColor: UserTag[] = [
        {
          ...mockTags[0],
          color: null,
        },
      ];
      vi.mocked(tagsApi.fetchTags).mockResolvedValue(tagsWithNullColor);

      const { result } = renderHook(() => useTags());

      await waitFor(() => {
        expect(result.current.loading).toBe(false);
      });

      expect(result.current.tags[0].color).toBeNull();
    });
  });
});
