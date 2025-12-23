import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { BrowserRouter } from 'react-router-dom'
import App from './App'

// Mock Auth0
const mockUseAuth0 = vi.hoisted(() => vi.fn())

vi.mock('@auth0/auth0-react', () => ({
  useAuth0: mockUseAuth0,
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
      mockUseAuth0.mockReturnValue({
        isAuthenticated: false,
        isLoading: false,
        user: null,
        loginWithRedirect: vi.fn(),
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
    const mockUser = {
      name: 'Test User',
      email: 'test@example.com',
      picture: 'https://example.com/avatar.jpg',
    }

    beforeEach(() => {
      mockUseAuth0.mockReturnValue({
        isAuthenticated: true,
        isLoading: false,
        user: mockUser,
        logout: vi.fn(),
        loginWithRedirect: vi.fn(),
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

      // Open the user dropdown
      await waitFor(() => {
        expect(screen.getByRole('button', { name: /user menu/i })).toBeInTheDocument()
      })
      await user.click(screen.getByRole('button', { name: /user menu/i }))

      await waitFor(() => {
        expect(screen.getByText(mockUser.name)).toBeInTheDocument()
      })
    })

    it('should display user email in dropdown when opened', async () => {
      const user = userEvent.setup()
      render(
        <BrowserRouter>
          <App />
        </BrowserRouter>
      )

      // Open the user dropdown
      await waitFor(() => {
        expect(screen.getByRole('button', { name: /user menu/i })).toBeInTheDocument()
      })
      await user.click(screen.getByRole('button', { name: /user menu/i }))

      await waitFor(() => {
        expect(screen.getByText(mockUser.email)).toBeInTheDocument()
      })
    })
  })

  describe('phase status', () => {
    beforeEach(() => {
      mockUseAuth0.mockReturnValue({
        isAuthenticated: false,
        isLoading: false,
        user: null,
        loginWithRedirect: vi.fn(),
      })
    })

    it('should show Auth0 integration as complete', async () => {
      render(
        <BrowserRouter>
          <App />
        </BrowserRouter>
      )

      await waitFor(() => {
        expect(screen.getByText(/✅ Auth0 integration/i)).toBeInTheDocument()
      })
    })

    it('should not show Auth0 as coming next', async () => {
      render(
        <BrowserRouter>
          <App />
        </BrowserRouter>
      )

      await waitFor(() => {
        expect(screen.queryByText(/⏳ Auth0 integration \(coming next\)/i)).not.toBeInTheDocument()
      })
    })
  })

  describe('health check section', () => {
    beforeEach(() => {
      mockUseAuth0.mockReturnValue({
        isAuthenticated: false,
        isLoading: false,
        user: null,
        loginWithRedirect: vi.fn(),
      })
    })

    it('should still display system status section', async () => {
      render(
        <BrowserRouter>
          <App />
        </BrowserRouter>
      )

      await waitFor(() => {
        expect(screen.getByRole('heading', { name: /system status/i })).toBeInTheDocument()
      })
    })

    it('should display health check error message', async () => {
      render(
        <BrowserRouter>
          <App />
        </BrowserRouter>
      )

      await waitFor(() => {
        expect(screen.getByText(/Failed to load system status/i)).toBeInTheDocument()
      })
    })
  })

  describe('transactions navigation', () => {
    describe('when user is authenticated', () => {
      const mockUser = {
        name: 'Test User',
        email: 'test@example.com',
        picture: 'https://example.com/avatar.jpg',
      }

      beforeEach(() => {
        mockUseAuth0.mockReturnValue({
          isAuthenticated: true,
          isLoading: false,
          user: mockUser,
          logout: vi.fn(),
          loginWithRedirect: vi.fn(),
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
        mockUseAuth0.mockReturnValue({
          isAuthenticated: false,
          user: null,
          loginWithRedirect: vi.fn(),
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
