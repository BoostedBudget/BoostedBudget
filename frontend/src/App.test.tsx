/**
 * App Component Tests
 * Issue #575: Migrated to bun:test with centralized mocks
 */

import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { BrowserRouter, MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import App from './App'

// Import centralized mocks
import {
  mockUseAuth,
  mockUseUser,
  mockUseClerk,
  mockClerkUser,
  createMockClerkAuth,
  createMockClerkUser,
  createMockClerk,
} from '@/test/mocks/clerk'
import { mockUseUpdatePrivacySettings } from '@/test/mocks/privacy-api'

// Mock fetch globally
const mockFetch = mock(() =>
  Promise.resolve({
    json: () =>
      Promise.resolve({
        status: 'healthy',
        version: '1.0.0',
        timestamp: '2025-10-18T00:00:00Z',
        checks: { database: true, redis: true },
      }),
  })
)
globalThis.fetch = mockFetch as unknown as typeof fetch

function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
    },
  })
}

function createWrapper() {
  const queryClient = createTestQueryClient()
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <BrowserRouter>{children}</BrowserRouter>
      </QueryClientProvider>
    )
  }
}

function createMemoryWrapper(initialEntries: string[]) {
  const queryClient = createTestQueryClient()
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={initialEntries}>{children}</MemoryRouter>
      </QueryClientProvider>
    )
  }
}

describe('App', () => {
  beforeEach(() => {
    mockUseAuth.mockClear()
    mockUseUser.mockClear()
    mockUseClerk.mockClear()
    mockUseUpdatePrivacySettings.mockClear()
    mockFetch.mockClear()

    // Default privacy settings mock
    mockUseUpdatePrivacySettings.mockReturnValue({
      mutate: mock(() => {}),
      mutateAsync: mock(() => Promise.resolve()),
      isPending: false,
      isError: false,
      isSuccess: false,
      error: null,
      data: null,
      reset: mock(() => {}),
    })
  })

  describe('when user is not authenticated', () => {
    beforeEach(() => {
      mockUseAuth.mockReturnValue(createMockClerkAuth({ isSignedIn: false, isLoaded: true }))
      mockUseUser.mockReturnValue(createMockClerkUser({ user: null }))
      mockUseClerk.mockReturnValue(createMockClerk())
    })

    it('should render login button', async () => {
      const Wrapper = createWrapper()
      render(<App />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText(/log in/i)).toBeInTheDocument()
      })
    })

    it('should not render logout button', async () => {
      const Wrapper = createWrapper()
      render(<App />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.queryByText(/log out/i)).not.toBeInTheDocument()
      })
    })

    it('should not display user profile', async () => {
      const Wrapper = createWrapper()
      render(<App />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.queryByText(/welcome/i)).not.toBeInTheDocument()
      })
    })
  })

  describe('when user is authenticated', () => {
    beforeEach(() => {
      mockUseAuth.mockReturnValue(createMockClerkAuth({ isSignedIn: true, isLoaded: true }))
      mockUseUser.mockReturnValue(
        createMockClerkUser({
          isSignedIn: true,
          user: {
            ...mockClerkUser,
            fullName: 'Test User',
            primaryEmailAddress: { emailAddress: 'test@example.com' },
          },
        })
      )
      mockUseClerk.mockReturnValue(createMockClerk())
    })

    it('should render user menu button', async () => {
      const Wrapper = createWrapper()
      render(<App />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /user menu/i })).toBeInTheDocument()
      })
    })

    it('should not render login button', async () => {
      const Wrapper = createWrapper()
      render(<App />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.queryByText(/log in/i)).not.toBeInTheDocument()
      })
    })

    it('should display user name in dropdown when opened', async () => {
      const user = userEvent.setup()
      const Wrapper = createWrapper()
      render(<App />, { wrapper: Wrapper })

      await waitFor(
        () => {
          expect(screen.getByRole('button', { name: /user menu/i })).toBeInTheDocument()
        },
        { timeout: 10000 }
      )
      await user.click(screen.getByRole('button', { name: /user menu/i }))

      await waitFor(
        () => {
          expect(screen.getByText('Test User')).toBeInTheDocument()
        },
        { timeout: 10000 }
      )
    }, 60000)

    it('should display user email in dropdown when opened', async () => {
      const user = userEvent.setup()
      const Wrapper = createWrapper()
      render(<App />, { wrapper: Wrapper })

      await waitFor(
        () => {
          expect(screen.getByRole('button', { name: /user menu/i })).toBeInTheDocument()
        },
        { timeout: 10000 }
      )
      await user.click(screen.getByRole('button', { name: /user menu/i }))

      await waitFor(
        () => {
          expect(screen.getByText('test@example.com')).toBeInTheDocument()
        },
        { timeout: 10000 }
      )
    }, 60000)
  })

  describe('phase status', () => {
    beforeEach(() => {
      mockUseAuth.mockReturnValue(createMockClerkAuth({ isSignedIn: false, isLoaded: true }))
      mockUseUser.mockReturnValue(createMockClerkUser({ user: null }))
      mockUseClerk.mockReturnValue(createMockClerk())
    })

    it('should show Clerk integration as complete', async () => {
      const Wrapper = createMemoryWrapper(['/'])
      render(<App />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText(/✅ Clerk integration/i)).toBeInTheDocument()
      })
    })

    it('should not show Clerk as coming next', async () => {
      const Wrapper = createMemoryWrapper(['/'])
      render(<App />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(
          screen.queryByText(/⏳ Clerk integration \(coming next\)/i)
        ).not.toBeInTheDocument()
      })
    })
  })

  describe('health check section', () => {
    beforeEach(() => {
      mockUseAuth.mockReturnValue(createMockClerkAuth({ isSignedIn: false, isLoaded: true }))
      mockUseUser.mockReturnValue(createMockClerkUser({ user: null }))
      mockUseClerk.mockReturnValue(createMockClerk())
    })

    it('should still display system status section', async () => {
      const Wrapper = createMemoryWrapper(['/'])
      render(<App />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByRole('heading', { name: /system status/i })).toBeInTheDocument()
      })
    })

    it('should display health check error message', async () => {
      const Wrapper = createMemoryWrapper(['/'])
      render(<App />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText(/Failed to load system status/i)).toBeInTheDocument()
      })
    })
  })

  describe('transactions navigation', () => {
    describe('when user is authenticated', () => {
      beforeEach(() => {
        mockUseAuth.mockReturnValue(createMockClerkAuth({ isSignedIn: true, isLoaded: true }))
        mockUseUser.mockReturnValue(
          createMockClerkUser({
            isSignedIn: true,
            user: {
              ...mockClerkUser,
              fullName: 'Test User',
              primaryEmailAddress: { emailAddress: 'test@example.com' },
            },
          })
        )
        mockUseClerk.mockReturnValue(createMockClerk())
      })

      it('should display transactions link in navigation', async () => {
        const Wrapper = createWrapper()
        render(<App />, { wrapper: Wrapper })

        await waitFor(() => {
          const transactionsLink = screen.getByRole('link', { name: /transactions/i })
          expect(transactionsLink).toBeInTheDocument()
          expect(transactionsLink).toHaveAttribute('href', '/transactions')
        })
      })
    })

    describe('when user is not authenticated', () => {
      beforeEach(() => {
        mockUseAuth.mockReturnValue(createMockClerkAuth({ isSignedIn: false, isLoaded: true }))
        mockUseUser.mockReturnValue(createMockClerkUser({ user: null }))
        mockUseClerk.mockReturnValue(createMockClerk())
      })

      it('should not display transactions link in navigation', async () => {
        const Wrapper = createWrapper()
        render(<App />, { wrapper: Wrapper })

        await waitFor(() => {
          const transactionsLink = screen.queryByRole('link', { name: /transactions/i })
          expect(transactionsLink).not.toBeInTheDocument()
        })
      })
    })
  })
})
