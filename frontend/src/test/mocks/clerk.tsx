import { vi } from 'vitest'
import React from 'react'

// Mock user for testing (Clerk format)
export const mockClerkUser = {
  id: 'user_123456',
  primaryEmailAddress: {
    emailAddress: 'test@example.com',
    verification: { status: 'verified' as const },
  },
  fullName: 'Test User',
  imageUrl: 'https://example.com/avatar.jpg',
  publicMetadata: {
    subscription_tier: 'free',
    primary_region: 'iad',
  },
}

// Mock Clerk context values for useAuth
export const createMockClerkAuth = (overrides = {}) => ({
  isSignedIn: false,
  isLoaded: true,
  userId: null,
  sessionId: null,
  getToken: vi.fn().mockResolvedValue('mock-token'),
  ...overrides,
})

// Mock Clerk context values for useUser
export const createMockClerkUser = (overrides = {}) => ({
  isSignedIn: false,
  isLoaded: true,
  user: null,
  ...overrides,
})

// Mock Clerk context values for useClerk
export const createMockClerk = (overrides = {}) => ({
  openSignIn: vi.fn(),
  openSignUp: vi.fn(),
  signOut: vi.fn(),
  ...overrides,
})

// Mock ClerkProvider for tests
export const MockClerkProvider: React.FC<{
  children: React.ReactNode
  authValue?: ReturnType<typeof createMockClerkAuth>
  userValue?: ReturnType<typeof createMockClerkUser>
  clerkValue?: ReturnType<typeof createMockClerk>
}> = ({ children }) => {
  return <>{children}</>
}

// Default mock for @clerk/react-router
// This can be imported in test files that need to mock Clerk
export const defaultClerkMock = {
  useAuth: vi.fn(() => createMockClerkAuth()),
  useUser: vi.fn(() => createMockClerkUser()),
  useClerk: vi.fn(() => createMockClerk()),
  ClerkProvider: MockClerkProvider,
  SignedIn: ({ children: _children }: { children: React.ReactNode }) => null,
  SignedOut: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SignInButton: ({ children }: { children?: React.ReactNode }) => <button>{children || 'Sign In'}</button>,
  UserButton: () => <button>User</button>,
}
