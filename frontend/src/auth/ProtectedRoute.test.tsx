import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { BrowserRouter } from 'react-router-dom'
import { ProtectedRoute } from './ProtectedRoute'

// Mock Clerk
const mockUseAuth = vi.hoisted(() => vi.fn())
const mockRedirectToSignIn = vi.hoisted(() => vi.fn(() => null))

vi.mock('@clerk/clerk-react', () => ({
  useAuth: mockUseAuth,
  RedirectToSignIn: mockRedirectToSignIn,
}))

describe('ProtectedRoute', () => {
  const TestComponent = () => <div>Protected Content</div>

  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('should show loading state while authentication is loading', () => {
    mockUseAuth.mockReturnValue({
      isLoaded: false,
      isSignedIn: false,
    })

    render(
      <BrowserRouter>
        <ProtectedRoute>
          <TestComponent />
        </ProtectedRoute>
      </BrowserRouter>
    )

    expect(screen.getByText(/loading/i)).toBeInTheDocument()
  })

  it('should redirect unauthenticated users to sign in', () => {
    mockUseAuth.mockReturnValue({
      isLoaded: true,
      isSignedIn: false,
    })

    render(
      <BrowserRouter>
        <ProtectedRoute>
          <TestComponent />
        </ProtectedRoute>
      </BrowserRouter>
    )

    // Should render RedirectToSignIn component
    expect(mockRedirectToSignIn).toHaveBeenCalled()
    // Should not show protected content
    expect(screen.queryByText('Protected Content')).not.toBeInTheDocument()
  })

  it('should render protected content for authenticated users', () => {
    mockUseAuth.mockReturnValue({
      isLoaded: true,
      isSignedIn: true,
    })

    render(
      <BrowserRouter>
        <ProtectedRoute>
          <TestComponent />
        </ProtectedRoute>
      </BrowserRouter>
    )

    expect(screen.getByText('Protected Content')).toBeInTheDocument()
  })
})
