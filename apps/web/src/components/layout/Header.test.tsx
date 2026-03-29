/**
 * Header Component Tests
 * Issue #575: Migrated to bun:test with centralized Clerk mocks
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { BrowserRouter } from 'react-router-dom'
import { Header } from './Header'
import { mockUseAuth, mockUseUser, mockUseClerk } from '@/test/mocks/clerk'

const mockClerkUser = {
  id: 'user_123',
  fullName: 'Test User',
  primaryEmailAddress: {
    emailAddress: 'test@example.com',
  },
  imageUrl: 'https://example.com/avatar.jpg',
}

const setupUnauthenticatedMocks = () => {
  mockUseAuth.mockReturnValue({ isSignedIn: false })
  mockUseUser.mockReturnValue({ user: null })
  mockUseClerk.mockReturnValue({
    openSignIn: () => {},
    signOut: () => {},
  })
}

const setupAuthenticatedMocks = (user = mockClerkUser) => {
  mockUseAuth.mockReturnValue({ isSignedIn: true })
  mockUseUser.mockReturnValue({ user })
  mockUseClerk.mockReturnValue({
    openSignIn: () => {},
    signOut: () => {},
  })
}

const renderHeader = (props?: object) =>
  render(
    <BrowserRouter>
      <Header {...props} />
    </BrowserRouter>
  )

describe('Header', () => {
  beforeEach(() => {
    mockUseAuth.mockClear()
    mockUseUser.mockClear()
    mockUseClerk.mockClear()
  })

  describe('when not authenticated', () => {
    beforeEach(() => {
      setupUnauthenticatedMocks()
    })

    it('renders logo', () => {
      renderHeader()
      expect(screen.getByText(/boostedbudget/i)).toBeInTheDocument()
    })

    it('renders rocket emoji', () => {
      renderHeader()
      expect(screen.getByText('🚀')).toBeInTheDocument()
    })

    it('renders login button on desktop', () => {
      renderHeader()
      // There should be a login button (may be hidden on mobile via CSS)
      expect(screen.getByRole('button', { name: /log in/i })).toBeInTheDocument()
    })

    it('does not render navigation links', () => {
      renderHeader()
      expect(screen.queryByRole('navigation', { name: /main navigation/i })).not.toBeInTheDocument()
    })

    it('renders mobile nav trigger', () => {
      renderHeader()
      expect(screen.getByRole('button', { name: /open menu/i })).toBeInTheDocument()
    })
  })

  describe('when authenticated', () => {
    beforeEach(() => {
      setupAuthenticatedMocks()
    })

    it('renders logo', () => {
      renderHeader()
      expect(screen.getByText(/boostedbudget/i)).toBeInTheDocument()
    })

    it('renders navigation links', () => {
      renderHeader()
      expect(screen.getByRole('navigation', { name: /main navigation/i })).toBeInTheDocument()
    })

    it('renders user dropdown trigger', () => {
      renderHeader()
      expect(screen.getByRole('button', { name: /user menu/i })).toBeInTheDocument()
    })

    it('does not render login button', () => {
      renderHeader()
      expect(screen.queryByRole('button', { name: /log in/i })).not.toBeInTheDocument()
    })

    it('renders mobile nav trigger', () => {
      renderHeader()
      expect(screen.getByRole('button', { name: /open menu/i })).toBeInTheDocument()
    })
  })

  describe('styling', () => {
    beforeEach(() => {
      setupUnauthenticatedMocks()
    })

    it('has sticky positioning', () => {
      renderHeader()
      const header = screen.getByRole('banner')
      expect(header).toHaveClass('sticky', 'top-0')
    })

    it('has z-index for stacking', () => {
      renderHeader()
      expect(screen.getByRole('banner')).toHaveClass('z-50')
    })

    it('has border-bottom', () => {
      renderHeader()
      expect(screen.getByRole('banner')).toHaveClass('border-b')
    })

    it('has fixed height', () => {
      renderHeader()
      expect(screen.getByRole('banner')).toHaveClass('h-16')
    })
  })

  describe('logo link', () => {
    it('links to dashboard when authenticated', () => {
      setupAuthenticatedMocks()
      renderHeader()
      expect(screen.getByRole('link', { name: /boostedbudget/i })).toHaveAttribute('href', '/dashboard')
    })

    it('links to home when not authenticated', () => {
      setupUnauthenticatedMocks()
      renderHeader()
      expect(screen.getByRole('link', { name: /boostedbudget/i })).toHaveAttribute('href', '/')
    })
  })

  describe('accepts custom className', () => {
    it('applies custom className to header', () => {
      setupUnauthenticatedMocks()
      renderHeader({ className: 'custom-class' })
      expect(screen.getByRole('banner')).toHaveClass('custom-class')
    })
  })
})
