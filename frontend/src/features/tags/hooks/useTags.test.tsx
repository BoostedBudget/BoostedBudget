/**
 * useTags Hook Tests
 * Issue #497: Budget Category Manager + Match Config UI
 * Issue #575: Migrated from MSW to centralized API mocks
 *
 * Tests the React hook with React Query caching strategy.
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'
import { useTags } from './useTags'
import type { UserTag } from '@/api/generated/models'
import {
  mockUseGetUserTags,
  mockRefetchUserTags,
} from '@/test/mocks/tags-api'

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
]

function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
    },
  })
}

function createWrapper() {
  const queryClient = createTestQueryClient()
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    )
  }
}

describe('useTags', () => {
  beforeEach(() => {
    mockUseGetUserTags.mockClear()
    mockRefetchUserTags.mockClear()

    // Default successful response
    mockUseGetUserTags.mockReturnValue({
      data: mockTags,
      isLoading: false,
      error: null,
      refetch: mockRefetchUserTags,
    })
  })

  describe('Initial State', () => {
    it('should return loading=true initially', () => {
      mockUseGetUserTags.mockReturnValue({
        data: undefined,
        isLoading: true,
        error: null,
        refetch: mockRefetchUserTags,
      })

      const { result } = renderHook(() => useTags(), {
        wrapper: createWrapper(),
      })

      expect(result.current.loading).toBe(true)
      expect(result.current.tags).toEqual([])
      expect(result.current.error).toBeNull()
    })
  })

  describe('Fetching Tags', () => {
    it('should return tags on successful fetch', () => {
      const { result } = renderHook(() => useTags(), {
        wrapper: createWrapper(),
      })

      expect(result.current.loading).toBe(false)
      expect(result.current.tags.length).toBe(2)
      expect(result.current.error).toBeNull()
    })

    it('should sort tags alphabetically by name', () => {
      const unsortedTags: UserTag[] = [
        { ...mockTags[0], name: 'Zebra' },
        { ...mockTags[1], name: 'Alpha' },
      ]

      mockUseGetUserTags.mockReturnValue({
        data: unsortedTags,
        isLoading: false,
        error: null,
        refetch: mockRefetchUserTags,
      })

      const { result } = renderHook(() => useTags(), {
        wrapper: createWrapper(),
      })

      expect(result.current.tags[0].name).toBe('Alpha')
      expect(result.current.tags[1].name).toBe('Zebra')
    })
  })

  describe('Error Handling', () => {
    it('should return error on failed fetch', () => {
      const error = new Error('Network error')
      mockUseGetUserTags.mockReturnValue({
        data: undefined,
        isLoading: false,
        error,
        refetch: mockRefetchUserTags,
      })

      const { result } = renderHook(() => useTags(), {
        wrapper: createWrapper(),
      })

      expect(result.current.error).toBeTruthy()
      expect(result.current.tags).toEqual([])
    })

    it('should wrap errors in Error instances', () => {
      mockUseGetUserTags.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: 'String error',
        refetch: mockRefetchUserTags,
      })

      const { result } = renderHook(() => useTags(), {
        wrapper: createWrapper(),
      })

      expect(result.current.error).toBeInstanceOf(Error)
    })
  })

  describe('Refetch', () => {
    it('should provide refetch function', () => {
      const { result } = renderHook(() => useTags(), {
        wrapper: createWrapper(),
      })

      expect(typeof result.current.refetch).toBe('function')
    })

    it('should call underlying refetch when invoked', () => {
      const { result } = renderHook(() => useTags(), {
        wrapper: createWrapper(),
      })

      result.current.refetch()

      expect(mockRefetchUserTags).toHaveBeenCalled()
    })
  })

  describe('Edge Cases', () => {
    it('should handle empty tags array', () => {
      mockUseGetUserTags.mockReturnValue({
        data: [],
        isLoading: false,
        error: null,
        refetch: mockRefetchUserTags,
      })

      const { result } = renderHook(() => useTags(), {
        wrapper: createWrapper(),
      })

      expect(result.current.tags).toEqual([])
      expect(result.current.error).toBeNull()
    })

    it('should handle tags with undefined color', () => {
      const tagsWithUndefinedColor: UserTag[] = [
        {
          ...mockTags[0],
          color: undefined,
        },
      ]

      mockUseGetUserTags.mockReturnValue({
        data: tagsWithUndefinedColor,
        isLoading: false,
        error: null,
        refetch: mockRefetchUserTags,
      })

      const { result } = renderHook(() => useTags(), {
        wrapper: createWrapper(),
      })

      expect(result.current.tags[0].color).toBeUndefined()
    })
  })
})
