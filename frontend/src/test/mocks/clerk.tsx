// Issue #575: Centralized Clerk mock to prevent mock.module() pollution
// This file is preloaded via bunfig.toml before all tests

import { mock } from 'bun:test'
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
  getToken: mock(() => Promise.resolve('mock-token')),
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
  openSignIn: mock(() => {}),
  openSignUp: mock(() => {}),
  signOut: mock(() => Promise.resolve()),
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

// Configurable mock functions that tests can customize
export const mockUseAuth = mock(() => createMockClerkAuth())
export const mockUseUser = mock(() => createMockClerkUser())
export const mockUseClerk = mock(() => createMockClerk())

// Default mock for @clerk/react-router
export const defaultClerkMock = {
  useAuth: mockUseAuth,
  useUser: mockUseUser,
  useClerk: mockUseClerk,
  ClerkProvider: MockClerkProvider,
  SignedIn: ({ children: _children }: { children: React.ReactNode }) => null,
  SignedOut: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SignInButton: ({ children }: { children?: React.ReactNode }) => (
    <button>{children || 'Sign In'}</button>
  ),
  UserButton: () => <button>User</button>,
}

// Single mock.module() call - called once at preload time
mock.module('@clerk/react-router', () => defaultClerkMock)
