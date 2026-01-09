import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { BrowserRouter, MemoryRouter } from 'react-router-dom'
import App from './App'

// Mock Clerk
const mockUseAuth = vi.hoisted(() => vi.fn())
const mockUseUser = vi.hoisted(() => vi.fn())
const mockUseClerk = vi.hoisted(() => vi.fn())

vi.mock('@clerk/react-router', () => ({
  useAuth: mockUseAuth,
  useUser: mockUseUser,
  useClerk: mockUseClerk,
}))

// Mock useDefaultDashboardLayout hook to avoid QueryClient requirement
vi.mock('./features/dashboard/hooks/useDefaultDashboardLayout', () => ({
  useDefaultDashboardLayout: vi.fn(() => ({
    defaultLayout: {
      widgets: [],
      version: 1,
    },
    loading: false,
    error: null,
    refetch: vi.fn(),
    isFromApi: false,
  })),
}))

// Mock useDashboardLayout hook to avoid QueryClient requirement
vi.mock('./features/dashboard/hooks/useDashboardLayout', () => ({
  useDashboardLayout: vi.fn(() => ({
    layout: {
      widgets: [
        { id: '1', type: 'balance-summary', position: { x: 0, y: 0 }, size: { width: 4, height: 2 } },
      ],
      version: 1,
    },
    loading: false,
    error: null,
    saveLayout: vi.fn(),
    resetLayout: vi.fn(),
    isModified: false,
    isSaving: false,
  })),
}))

// Mock useDashboardData hook to avoid QueryClient requirement
vi.mock('./features/dashboard/hooks/useDashboardData', () => ({
  useDashboardData: vi.fn(() => ({
    balanceSummary: null,
    balanceHistory: [],
    accountTypes: { types: [] },
    loading: false,
    error: null,
    refetch: vi.fn(),
  })),
}))

// Mock useCashFlowData hook to avoid QueryClient requirement
vi.mock('./features/dashboard/hooks/useCashFlowData', () => ({
  useCashFlowData: vi.fn(() => ({
    summary: null,
    history: [],
    loading: false,
    error: null,
    refetch: vi.fn(),
  })),
}))

// Mock fetch globally
globalThis.fetch = vi.fn() as typeof fetch

// Mock the generated privacy hooks used by App.tsx
vi.mock('@/api/generated/endpoints/privacy/privacy', () => ({
  useGetPrivacySettings: vi.fn(() => ({
    data: null,
    isLoading: false,
    error: null,
    refetch: vi.fn(),
  })),
  useUpdatePrivacySettings: vi.fn(() => ({
    mutate: vi.fn(),
    mutateAsync: vi.fn(),
    isPending: false,
    isError: false,
    isSuccess: false,
    error: null,
    data: null,
    reset: vi.fn(),
  })),
}))

// Mock the generated dashboard hooks used by WidgetCatalog
vi.mock('@/api/generated/endpoints/dashboard/dashboard', () => ({
  useGetWidgetTypes: vi.fn(() => ({
    data: {
      widgetTypes: [
        { type: 'balance-overview', name: 'Balance Overview', category: 'overview' },
        { type: 'balance-history', name: 'Balance History', category: 'charts' },
        { type: 'cash-flow-summary', name: 'Cash Flow Summary', category: 'overview' },
        { type: 'cash-flow-chart', name: 'Cash Flow Chart', category: 'charts' },
        { type: 'account-cards', name: 'Account Cards', category: 'accounts' },
        { type: 'net-worth', name: 'Net Worth', category: 'overview' },
      ],
    },
    isLoading: false,
    error: null,
  })),
}))

describe('App', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // Default mock for fetch (health check)
    ;(globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValue({
      json: async () => ({
        status: 'healthy',
        version: '1.0.0',
        timestamp: '2025-10-18T00:00:00Z',
        checks: {
          database: true,
          redis: true,
        },
      }),
    })
  })

  describe('when user is not authenticated', () => {
    beforeEach(() => {
      mockUseAuth.mockReturnValue({
        isSignedIn: false,
        isLoaded: true,
      })
      mockUseUser.mockReturnValue({
        user: null,
      })
      mockUseClerk.mockReturnValue({
        openSignIn: vi.fn(),
        signOut: vi.fn(),
      })
    })

    it('should render login button', async () => {
      render(
        <BrowserRouter>
          <App />
        </BrowserRouter>
      )

      await waitFor(() => {
        expect(screen.getByText(/log in/i)).toBeInTheDocument()
      })
    })

    it('should not render logout button', async () => {
      render(
        <BrowserRouter>
          <App />
        </BrowserRouter>
      )

      await waitFor(() => {
        expect(screen.queryByText(/log out/i)).not.toBeInTheDocument()
      })
    })

    it('should not display user profile', async () => {
      render(
        <BrowserRouter>
          <App />
        </BrowserRouter>
      )

      await waitFor(() => {
        expect(screen.queryByText(/welcome/i)).not.toBeInTheDocument()
      })
    })
  })

  describe('when user is authenticated', () => {
    const mockClerkUser = {
      id: 'user_123',
      fullName: 'Test User',
      primaryEmailAddress: {
        emailAddress: 'test@example.com',
      },
      imageUrl: 'https://example.com/avatar.jpg',
    }

    beforeEach(() => {
      mockUseAuth.mockReturnValue({
        isSignedIn: true,
        isLoaded: true,
      })
      mockUseUser.mockReturnValue({
        user: mockClerkUser,
      })
      mockUseClerk.mockReturnValue({
        openSignIn: vi.fn(),
        signOut: vi.fn(),
      })
    })

    it('should render user menu button', async () => {
      render(
        <BrowserRouter>
          <App />
        </BrowserRouter>
      )

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /user menu/i })).toBeInTheDocument()
      })
    })

    it('should not render login button', async () => {
      render(
        <BrowserRouter>
          <App />
        </BrowserRouter>
      )

      await waitFor(() => {
        expect(screen.queryByText(/log in/i)).not.toBeInTheDocument()
      })
    })

    it('should display user name in dropdown when opened', async () => {
      const user = userEvent.setup()
      render(
        <BrowserRouter>
          <App />
        </BrowserRouter>
      )

      // Open the user dropdown (increased timeout for CI)
      await waitFor(() => {
        expect(screen.getByRole('button', { name: /user menu/i })).toBeInTheDocument()
      }, { timeout: 10000 })
      await user.click(screen.getByRole('button', { name: /user menu/i }))

      await waitFor(() => {
        expect(screen.getByText('Test User')).toBeInTheDocument()
      }, { timeout: 10000 })
    }, 60000) // Extended timeout for CI

    it('should display user email in dropdown when opened', async () => {
      const user = userEvent.setup()
      render(
        <BrowserRouter>
          <App />
        </BrowserRouter>
      )

      // Open the user dropdown (increased timeout for CI)
      await waitFor(() => {
        expect(screen.getByRole('button', { name: /user menu/i })).toBeInTheDocument()
      }, { timeout: 10000 })
      await user.click(screen.getByRole('button', { name: /user menu/i }))

      await waitFor(() => {
        expect(screen.getByText('test@example.com')).toBeInTheDocument()
      }, { timeout: 10000 })
    }, 60000) // Extended timeout for CI
  })

  describe('phase status', () => {
    beforeEach(() => {
      mockUseAuth.mockReturnValue({
        isSignedIn: false,
        isLoaded: true,
      })
      mockUseUser.mockReturnValue({
        user: null,
      })
      mockUseClerk.mockReturnValue({
        openSignIn: vi.fn(),
        signOut: vi.fn(),
      })
    })

    it('should show Clerk integration as complete', async () => {
      render(
        <MemoryRouter initialEntries={['/']}>
          <App />
        </MemoryRouter>
      )

      await waitFor(() => {
        expect(screen.getByText(/✅ Clerk integration/i)).toBeInTheDocument()
      })
    })

    it('should not show Clerk as coming next', async () => {
      render(
        <MemoryRouter initialEntries={['/']}>
          <App />
        </MemoryRouter>
      )

      await waitFor(() => {
        expect(screen.queryByText(/⏳ Clerk integration \(coming next\)/i)).not.toBeInTheDocument()
      })
    })
  })

  describe('health check section', () => {
    beforeEach(() => {
      mockUseAuth.mockReturnValue({
        isSignedIn: false,
        isLoaded: true,
      })
      mockUseUser.mockReturnValue({
        user: null,
      })
      mockUseClerk.mockReturnValue({
        openSignIn: vi.fn(),
        signOut: vi.fn(),
      })
    })

    it('should still display system status section', async () => {
      render(
        <MemoryRouter initialEntries={['/']}>
          <App />
        </MemoryRouter>
      )

      await waitFor(() => {
        expect(screen.getByRole('heading', { name: /system status/i })).toBeInTheDocument()
      })
    })

    it('should display health check error message', async () => {
      render(
        <MemoryRouter initialEntries={['/']}>
          <App />
        </MemoryRouter>
      )

      await waitFor(() => {
        expect(screen.getByText(/Failed to load system status/i)).toBeInTheDocument()
      })
    })
  })

  describe('transactions navigation', () => {
    describe('when user is authenticated', () => {
      const mockClerkUser = {
        id: 'user_123',
        fullName: 'Test User',
        primaryEmailAddress: {
          emailAddress: 'test@example.com',
        },
        imageUrl: 'https://example.com/avatar.jpg',
      }

      beforeEach(() => {
        mockUseAuth.mockReturnValue({
          isSignedIn: true,
          isLoaded: true,
        })
        mockUseUser.mockReturnValue({
          user: mockClerkUser,
        })
        mockUseClerk.mockReturnValue({
          openSignIn: vi.fn(),
          signOut: vi.fn(),
        })
      })

      it('should display transactions link in navigation', async () => {
        render(
          <BrowserRouter>
            <App />
          </BrowserRouter>
        )

        await waitFor(() => {
          const transactionsLink = screen.getByRole('link', { name: /transactions/i })
          expect(transactionsLink).toBeInTheDocument()
          expect(transactionsLink).toHaveAttribute('href', '/transactions')
        })
      })
    })

    describe('when user is not authenticated', () => {
      beforeEach(() => {
        mockUseAuth.mockReturnValue({
          isSignedIn: false,
          isLoaded: true,
        })
        mockUseUser.mockReturnValue({
          user: null,
        })
        mockUseClerk.mockReturnValue({
          openSignIn: vi.fn(),
          signOut: vi.fn(),
        })
      })

      it('should not display transactions link in navigation', async () => {
        render(
          <BrowserRouter>
            <App />
          </BrowserRouter>
        )

        await waitFor(() => {
          const transactionsLink = screen.queryByRole('link', { name: /transactions/i })
          expect(transactionsLink).not.toBeInTheDocument()
        })
      })
    })
  })
})
