import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { BrowserRouter } from 'react-router-dom'
import App from './App'

// Mock Auth0
const mockUseAuth0 = vi.hoisted(() => vi.fn())

vi.mock('@auth0/auth0-react', () => ({
  useAuth0: mockUseAuth0,
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
        user: mockUser,
        logout: vi.fn(),
      })
    })

    it('should render logout button', async () => {
      render(
        <BrowserRouter>
          <App />
        </BrowserRouter>
      )

      await waitFor(() => {
        expect(screen.getByText(/log out/i)).toBeInTheDocument()
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

    it('should display user name', async () => {
      render(
        <BrowserRouter>
          <App />
        </BrowserRouter>
      )

      await waitFor(() => {
        expect(screen.getByText(mockUser.name)).toBeInTheDocument()
      })
    })

    it('should display user email', async () => {
      render(
        <BrowserRouter>
          <App />
        </BrowserRouter>
      )

      await waitFor(() => {
        expect(screen.getByText(mockUser.email)).toBeInTheDocument()
      })
    })
  })

  describe('phase status', () => {
    beforeEach(() => {
      mockUseAuth0.mockReturnValue({
        isAuthenticated: false,
        user: null,
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
        user: null,
      })
    })

    it('should still display system status section', async () => {
      render(
        <BrowserRouter>
          <App />
        </BrowserRouter>
      )

      await waitFor(() => {
        expect(screen.getByText(/system status/i)).toBeInTheDocument()
      })
    })

    it('should display health check data', async () => {
      render(
        <BrowserRouter>
          <App />
        </BrowserRouter>
      )

      await waitFor(() => {
        expect(screen.getByText(/Status: HEALTHY/i)).toBeInTheDocument()
        expect(screen.getByText(/Version: 1\.0\.0/i)).toBeInTheDocument()
      })
    })
  })
})
