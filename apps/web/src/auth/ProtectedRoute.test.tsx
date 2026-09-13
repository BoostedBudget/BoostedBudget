// Issue #1398: the PostHog feature-flag kill-switch that used to gate auth was removed;
// ProtectedRoute now gates purely on Clerk auth state (isLoaded / isSignedIn).

import { describe, it, expect, afterEach } from 'bun:test'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router'
import { ProtectedRoute } from './ProtectedRoute'
import { mockUseAuth, mockUseUser } from '@/test/mocks/clerk'
import { mockUseGetPostHogSupportIdentity } from '@/test/mocks/support-api'

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

function mockSignedInUser(): void {
  mockUseAuth.mockReturnValue({ isSignedIn: true, isLoaded: true } as ReturnType<typeof mockUseAuth>)
  mockUseUser.mockReturnValue({
    isSignedIn: true,
    isLoaded: true,
    user: { id: 'user_123456' },
  } as unknown as ReturnType<typeof mockUseUser>)
}

afterEach(() => {
  mockUseAuth.mockClear()
  mockUseUser.mockClear()
  mockUseGetPostHogSupportIdentity.mockClear()
})

describe('ProtectedRoute', () => {
  describe('auth gating (feature-flag kill-switch removed in #1398 — Clerk auth state is the only gate)', () => {
    it('renders children for signed-in user, no skeleton', () => {
      mockSignedInUser()

      renderProtectedRoute()

      expect(screen.getByText('Protected Content')).toBeInTheDocument()
      expect(screen.queryByText('Home')).not.toBeInTheDocument()
      expect(document.querySelector('.animate-pulse')).toBeNull()
    })

    it('owns authenticated PostHog experiences unless the route explicitly excludes them', async () => {
      mockSignedInUser()

      const { rerender } = render(
        <MemoryRouter>
          <ProtectedRoute><div>Product route</div></ProtectedRoute>
        </MemoryRouter>,
      )
      await waitFor(() => expect(mockUseGetPostHogSupportIdentity).toHaveBeenCalled())

      mockUseGetPostHogSupportIdentity.mockClear()
      rerender(
        <MemoryRouter>
          <ProtectedRoute postHogExperiences={false}><div>Admin route</div></ProtectedRoute>
        </MemoryRouter>,
      )
      expect(mockUseGetPostHogSupportIdentity).not.toHaveBeenCalled()
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
