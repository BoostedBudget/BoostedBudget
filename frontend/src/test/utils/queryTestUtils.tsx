/**
 * Test utilities for React Query
 *
 * Provides QueryClientProvider wrapper for testing components that use React Query hooks.
 */

import React from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, type RenderOptions, type RenderResult } from '@testing-library/react'

/**
 * Creates a fresh QueryClient for testing with disabled retries and caching.
 */
export function createTestQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
        gcTime: 0,
        staleTime: 0,
      },
      mutations: {
        retry: false,
      },
    },
  })
}

interface WrapperProps {
  children: React.ReactNode
}

/**
 * Creates a wrapper component with QueryClientProvider.
 * Use with @testing-library/react render function.
 *
 * @example
 * const { rerender } = render(<MyComponent />, {
 *   wrapper: createQueryWrapper(),
 * })
 */
export function createQueryWrapper(): React.ComponentType<WrapperProps> {
  const queryClient = createTestQueryClient()

  function QueryWrapper({ children }: WrapperProps) {
    return (
      <QueryClientProvider client={queryClient}>
        {children}
      </QueryClientProvider>
    )
  }

  return QueryWrapper
}

/**
 * Render helper that automatically wraps with QueryClientProvider.
 *
 * @example
 * renderWithQuery(<MyComponent />)
 * renderWithQuery(<MyComponent />, { wrapper: MemoryRouter })
 */
export function renderWithQuery(
  ui: React.ReactElement,
  options?: Omit<RenderOptions, 'wrapper'>
): RenderResult {
  const queryClient = createTestQueryClient()

  function Wrapper({ children }: WrapperProps) {
    return (
      <QueryClientProvider client={queryClient}>
        {children}
      </QueryClientProvider>
    )
  }

  return render(ui, { wrapper: Wrapper, ...options })
}

/**
 * Render helper that wraps with both QueryClientProvider and a custom wrapper.
 * Useful when you need both React Query and other providers (like MemoryRouter).
 *
 * @example
 * renderWithQueryAndWrapper(<MyComponent />, MemoryRouter)
 */
export function renderWithQueryAndWrapper(
  ui: React.ReactElement,
  CustomWrapper: React.ComponentType<{ children: React.ReactNode }>,
  options?: Omit<RenderOptions, 'wrapper'>
): RenderResult {
  const queryClient = createTestQueryClient()

  function CombinedWrapper({ children }: WrapperProps) {
    return (
      <QueryClientProvider client={queryClient}>
        <CustomWrapper>{children}</CustomWrapper>
      </QueryClientProvider>
    )
  }

  return render(ui, { wrapper: CombinedWrapper, ...options })
}
