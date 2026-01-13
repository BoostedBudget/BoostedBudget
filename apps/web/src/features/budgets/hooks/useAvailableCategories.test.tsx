import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import {
  mockUseGetAvailableCategories,
  mockRefetchAvailableCategories,
} from '@/test/mocks/budgets-api'
import { useAvailableCategories } from './useAvailableCategories'

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

describe('useAvailableCategories', () => {
  // API returns flat strings like "Food and Drink > Restaurants"
  const mockFlatCategories: string[] = [
    'Food and Drink > Restaurants',
    'Food and Drink > Groceries',
    'Transportation > Gas',
    'Transportation > Public Transit',
    'Shopping > Clothing',
  ]

  // Expected transformed result: string[][] format
  const expectedCategories: string[][] = [
    ['Food and Drink', 'Restaurants'],
    ['Food and Drink', 'Groceries'],
    ['Transportation', 'Gas'],
    ['Transportation', 'Public Transit'],
    ['Shopping', 'Clothing'],
  ]

  beforeEach(() => {
    mockRefetchAvailableCategories.mockClear()
    mockUseGetAvailableCategories.mockClear()
  })

  // Helper to configure the mock for loading state
  function configureLoading() {
    mockUseGetAvailableCategories.mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
      refetch: mockRefetchAvailableCategories,
    })
  }

  // Helper to configure the mock for success state
  // Generated hook returns string[] directly, not { categories: string[] }
  function configureSuccess(categories: string[]) {
    mockUseGetAvailableCategories.mockReturnValue({
      data: categories,
      isLoading: false,
      error: null,
      refetch: mockRefetchAvailableCategories,
    })
  }

  // Helper to configure the mock for error state
  function configureError(error: Error) {
    mockUseGetAvailableCategories.mockReturnValue({
      data: undefined,
      isLoading: false,
      error,
      refetch: mockRefetchAvailableCategories,
    })
  }

  describe('Initial State', () => {
    it('should return loading=true initially', () => {
      configureLoading();

      const { result } = renderHook(() => useAvailableCategories(), {
        wrapper: createWrapper(),
      });

      expect(result.current.loading).toBe(true);
      expect(result.current.categories).toEqual([]);
      expect(result.current.error).toBeNull();
    });
  });

  describe('Fetching Categories', () => {
    it('should return categories on successful fetch', () => {
      configureSuccess(mockFlatCategories);

      const { result } = renderHook(() => useAvailableCategories(), {
        wrapper: createWrapper(),
      });

      expect(result.current.loading).toBe(false);
      expect(result.current.categories).toEqual(expectedCategories);
      expect(result.current.error).toBeNull();
    });

    it('should transform flat strings to string[][] format', () => {
      configureSuccess(['Level1 > Level2 > Level3']);

      const { result } = renderHook(() => useAvailableCategories(), {
        wrapper: createWrapper(),
      });

      expect(result.current.categories).toEqual([
        ['Level1', 'Level2', 'Level3'],
      ]);
    });

    it('should handle single-level categories', () => {
      configureSuccess(['SingleCategory']);

      const { result } = renderHook(() => useAvailableCategories(), {
        wrapper: createWrapper(),
      });

      expect(result.current.categories).toEqual([['SingleCategory']]);
    });

    it('should trim whitespace from category parts', () => {
      configureSuccess(['  Food and Drink  >  Restaurants  ']);

      const { result } = renderHook(() => useAvailableCategories(), {
        wrapper: createWrapper(),
      });

      expect(result.current.categories).toEqual([
        ['Food and Drink', 'Restaurants'],
      ]);
    });
  });

  describe('Error Handling', () => {
    it('should return error on failed fetch', () => {
      const testError = new Error('Network error');
      configureError(testError);

      const { result } = renderHook(() => useAvailableCategories(), {
        wrapper: createWrapper(),
      });

      expect(result.current.loading).toBe(false);
      expect(result.current.error).toBeTruthy();
      expect(result.current.error?.message).toBe('Network error');
      expect(result.current.categories).toEqual([]);
    });

    it('should wrap non-Error objects in Error', () => {
      mockUseGetAvailableCategories.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: 'String error' as unknown as Error,
        refetch: mockRefetchAvailableCategories,
      })

      const { result } = renderHook(() => useAvailableCategories(), {
        wrapper: createWrapper(),
      })

      expect(result.current.error).toBeTruthy()
      expect(result.current.error?.message).toBe('Failed to fetch categories')
    })
  })

  describe('Refetch', () => {
    it('should provide refetch function', () => {
      configureSuccess(mockFlatCategories);

      const { result } = renderHook(() => useAvailableCategories(), {
        wrapper: createWrapper(),
      });

      expect(typeof result.current.refetch).toBe('function');
    });

    it('should call query refetch when refetch is called', async () => {
      configureSuccess(mockFlatCategories);

      const { result } = renderHook(() => useAvailableCategories(), {
        wrapper: createWrapper(),
      });

      await act(async () => {
        await result.current.refetch();
      });

      await waitFor(() => {
        expect(mockRefetchAvailableCategories).toHaveBeenCalled();
      });
    });
  });

  describe('Edge Cases', () => {
    it('should handle empty categories array', () => {
      configureSuccess([]);

      const { result } = renderHook(() => useAvailableCategories(), {
        wrapper: createWrapper(),
      });

      expect(result.current.categories).toEqual([]);
      expect(result.current.error).toBeNull();
    });

    it('should filter out empty strings', () => {
      configureSuccess([
        'Food and Drink > Restaurants',
        '',
        'Transportation > Gas',
      ]);

      const { result } = renderHook(() => useAvailableCategories(), {
        wrapper: createWrapper(),
      });

      expect(result.current.categories).toEqual([
        ['Food and Drink', 'Restaurants'],
        ['Transportation', 'Gas'],
      ]);
    });

    it('should handle undefined data', () => {
      mockUseGetAvailableCategories.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: null,
        refetch: mockRefetchAvailableCategories,
      })

      const { result } = renderHook(() => useAvailableCategories(), {
        wrapper: createWrapper(),
      })

      expect(result.current.categories).toEqual([])
      expect(result.current.error).toBeNull()
    })
  })
})
