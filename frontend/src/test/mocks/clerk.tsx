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
