// Issue #1398: the PostHog feature-flag kill-switch that used to gate auth was removed;
// ProtectedRoute now gates purely on Clerk auth state (isLoaded / isSignedIn).

import { describe, it, expect, afterEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router'
import { ProtectedRoute } from './ProtectedRoute'
import { mockUseAuth } from '@/test/mocks/clerk'

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
  mockUseAuth.mockClear()
})

describe('ProtectedRoute', () => {
  describe('auth gating (feature-flag kill-switch removed in #1398 — Clerk auth state is the only gate)', () => {
    it('renders children for signed-in user, no skeleton', () => {
      mockUseAuth.mockReturnValue({ isSignedIn: true, isLoaded: true } as ReturnType<typeof mockUseAuth>)

      renderProtectedRoute()

      expect(screen.getByText('Protected Content')).toBeInTheDocument()
      expect(screen.queryByText('Home')).not.toBeInTheDocument()
      expect(document.querySelector('.animate-pulse')).toBeNull()
    })

    it('redirects unauthenticated user to /', () => {
      mockUseAuth.mockReturnValue({ isSignedIn: false, isLoaded: true } as ReturnType<typeof mockUseAuth>)

      renderProtectedRoute()

      expect(screen.queryByText('Protected Content')).not.toBeInTheDocument()
      expect(screen.getByText('Home')).toBeInTheDocument()
    })
  })

  describe('while Clerk is loading (isLoaded=false)', () => {
    it('shows skeleton while Clerk auth is still loading', () => {
      mockUseAuth.mockReturnValue({ isSignedIn: false, isLoaded: false } as ReturnType<typeof mockUseAuth>)

      renderProtectedRoute()

      expect(screen.queryByText('Protected Content')).not.toBeInTheDocument()
      expect(screen.queryByText('Home')).not.toBeInTheDocument()
      expect(document.querySelector('.animate-pulse')).not.toBeNull()
    })
  })
})
