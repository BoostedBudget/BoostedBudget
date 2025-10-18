import { vi } from 'vitest'
import React from 'react'

// Mock user for testing
export const mockUser = {
  sub: 'auth0|123456',
  email: 'test@example.com',
  email_verified: true,
  name: 'Test User',
}

// Mock Auth0 context values
export const createMockAuth0Context = (overrides = {}) => ({
  isAuthenticated: false,
  isLoading: false,
  user: undefined,
  getAccessTokenSilently: vi.fn().mockResolvedValue('mock-token'),
  getIdTokenClaims: vi.fn().mockResolvedValue({}),
  loginWithRedirect: vi.fn(),
  loginWithPopup: vi.fn(),
  logout: vi.fn(),
  ...overrides,
})

// Mock Auth0Provider for tests
export const MockAuth0Provider: React.FC<{
  children: React.ReactNode
  value?: any
}> = ({ children, value }) => {
  const mockValue = value || createMockAuth0Context()

  // Mock the useAuth0 hook
  vi.mock('@auth0/auth0-react', () => ({
    Auth0Provider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
    useAuth0: () => mockValue,
    withAuthenticationRequired: (component: React.ComponentType) => component,
  }))

  return <>{children}</>
}
