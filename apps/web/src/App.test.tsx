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

const createTestQueryClient = () =>
  new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
    },
  })

const createWrapper = () => {
  const queryClient = createTestQueryClient()
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>{children}</BrowserRouter>
    </QueryClientProvider>
  )
}

const createMemoryWrapper = (initialEntries: string[]) => {
  const queryClient = createTestQueryClient()
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={initialEntries}>{children}</MemoryRouter>
    </QueryClientProvider>
  )
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

    it('should render landing page with hero section', async () => {
      const Wrapper = createWrapper()
      render(<App />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent(/take control/i)
      })
    })

    it('should render start free today CTA', async () => {
      const Wrapper = createWrapper()
      render(<App />, { wrapper: Wrapper })

      await waitFor(() => {
        // Multiple CTAs on landing page
        const ctaButtons = screen.getAllByRole('button', { name: /start free today/i })
        expect(ctaButtons.length).toBeGreaterThanOrEqual(1)
      })
    })

    it('should not render user menu', async () => {
      const Wrapper = createWrapper()
      render(<App />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.queryByRole('button', { name: /user menu/i })).not.toBeInTheDocument()
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

  describe('landing page sections', () => {
    beforeEach(() => {
      mockUseAuth.mockReturnValue(createMockClerkAuth({ isSignedIn: false, isLoaded: true }))
      mockUseUser.mockReturnValue(createMockClerkUser({ user: null }))
      mockUseClerk.mockReturnValue(createMockClerk())
    })

    it('should display features section', async () => {
      const Wrapper = createMemoryWrapper(['/'])
      render(<App />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByRole('heading', { name: /why choose boostedbudget/i })).toBeInTheDocument()
      })
    })

    it('should display how it works section', async () => {
      const Wrapper = createMemoryWrapper(['/'])
      render(<App />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByRole('heading', { name: /how it works/i })).toBeInTheDocument()
      })
    })

    it('should display FAQ section', async () => {
      const Wrapper = createMemoryWrapper(['/'])
      render(<App />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByRole('heading', { name: /frequently asked questions/i })).toBeInTheDocument()
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
