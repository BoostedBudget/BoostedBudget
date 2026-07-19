// Issue #1184: ProtectedRoute hard lockdown via login_enabled feature flag
// Tests all gating combinations: flag loading, flag off, auth state.

import { describe, it, expect, afterEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { ProtectedRoute } from './ProtectedRoute'
import { mockUseAuth } from '@/test/mocks/clerk'
import { mockUseFeatureFlagEnabled } from '@/test/mocks/posthog'

function renderProtectedRoute(initialPath = '/dashboard') {
  return render(
    <MemoryRouter initialEntries={[initialPath]}>
      <Routes>
        <Route path="/" element={<div>Home</div>} />
        <Route
          path="/dashboard"
          element={
            <ProtectedRoute>
              <div>Protected Content</div>
            </ProtectedRoute>
          }
        />
      </Routes>
    </MemoryRouter>
  )
}

afterEach(() => {
  // Reset window.ENV so PostHog configured state doesn't leak between tests
  delete (window as unknown as { ENV?: unknown }).ENV
  mockUseFeatureFlagEnabled.mockClear()
  mockUseAuth.mockClear()
})

describe('ProtectedRoute', () => {
  describe('login_enabled flag no longer gates auth (flag removed from gating)', () => {
    it('renders children for signed-in user while flag is loading (undefined) — no skeleton', () => {
      window.ENV = { POSTHOG_KEY: 'test-key' }
      mockUseFeatureFlagEnabled.mockReturnValue(undefined) // undefined → loading; should have no effect
      mockUseAuth.mockReturnValue({ isSignedIn: true, isLoaded: true } as ReturnType<typeof mockUseAuth>)

      renderProtectedRoute()

      expect(screen.getByText('Protected Content')).toBeInTheDocument()
      expect(screen.queryByText('Home')).not.toBeInTheDocument()
      expect(document.querySelector('.animate-pulse')).toBeNull()
    })

    it('redirects unauthenticated user to / while flag is loading (undefined), not stuck on a skeleton', () => {
      window.ENV = { POSTHOG_KEY: 'test-key' }
      mockUseFeatureFlagEnabled.mockReturnValue(undefined) // undefined → loading; should have no effect
      mockUseAuth.mockReturnValue({ isSignedIn: false, isLoaded: true } as ReturnType<typeof mockUseAuth>)

      renderProtectedRoute()

      expect(screen.queryByText('Protected Content')).not.toBeInTheDocument()
      expect(screen.getByText('Home')).toBeInTheDocument()
    })

    it('renders children for signed-in user when flag is false (no lockdown redirect)', () => {
      window.ENV = { POSTHOG_KEY: 'test-key' }
      mockUseFeatureFlagEnabled.mockReturnValue(false)
      mockUseAuth.mockReturnValue({ isSignedIn: true, isLoaded: true } as ReturnType<typeof mockUseAuth>)

      renderProtectedRoute()

      expect(screen.getByText('Protected Content')).toBeInTheDocument()
      expect(screen.queryByText('Home')).not.toBeInTheDocument()
    })

    it('redirects unauthenticated user to / when flag is false (redirect is auth-driven, not lockdown)', () => {
      window.ENV = { POSTHOG_KEY: 'test-key' }
      mockUseFeatureFlagEnabled.mockReturnValue(false)
      mockUseAuth.mockReturnValue({ isSignedIn: false, isLoaded: true } as ReturnType<typeof mockUseAuth>)

      renderProtectedRoute()

      expect(screen.queryByText('Protected Content')).not.toBeInTheDocument()
      expect(screen.getByText('Home')).toBeInTheDocument()
    })
  })

  describe('when login_enabled=true (normal operation)', () => {
    it('renders children for signed-in user', () => {
      window.ENV = { POSTHOG_KEY: 'test-key' }
      mockUseFeatureFlagEnabled.mockReturnValue(true)
      mockUseAuth.mockReturnValue({ isSignedIn: true, isLoaded: true } as ReturnType<typeof mockUseAuth>)

      renderProtectedRoute()

      expect(screen.getByText('Protected Content')).toBeInTheDocument()
      expect(screen.queryByText('Home')).not.toBeInTheDocument()
    })

    it('redirects unauthenticated user to / when login is enabled', () => {
      window.ENV = { POSTHOG_KEY: 'test-key' }
      mockUseFeatureFlagEnabled.mockReturnValue(true)
      mockUseAuth.mockReturnValue({ isSignedIn: false, isLoaded: true } as ReturnType<typeof mockUseAuth>)

      renderProtectedRoute()

      expect(screen.queryByText('Protected Content')).not.toBeInTheDocument()
      expect(screen.getByText('Home')).toBeInTheDocument()
    })
  })

  describe('while Clerk is loading (isLoaded=false)', () => {
    it('shows skeleton while Clerk auth is still loading', () => {
      // login_enabled flag no longer gates auth — only Clerk's isLoaded matters here
      mockUseAuth.mockReturnValue({ isSignedIn: false, isLoaded: false } as ReturnType<typeof mockUseAuth>)

      renderProtectedRoute()

      expect(screen.queryByText('Protected Content')).not.toBeInTheDocument()
      expect(screen.queryByText('Home')).not.toBeInTheDocument()
      expect(document.querySelector('.animate-pulse')).not.toBeNull()
    })
  })
})
