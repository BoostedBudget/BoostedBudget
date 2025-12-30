import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { BrowserRouter } from 'react-router-dom'
import { Header } from './Header'

// Mock Clerk
const mockUseAuth = vi.hoisted(() => vi.fn())
const mockUseUser = vi.hoisted(() => vi.fn())
const mockUseClerk = vi.hoisted(() => vi.fn())

vi.mock('@clerk/clerk-react', () => ({
  useAuth: mockUseAuth,
  useUser: mockUseUser,
  useClerk: mockUseClerk,
}))

describe('Header', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  const renderHeader = () => {
    return render(
      <BrowserRouter>
        <Header />
      </BrowserRouter>
    )
  }

  describe('when not authenticated', () => {
    beforeEach(() => {
      mockUseAuth.mockReturnValue({
        isSignedIn: false,
      })
      mockUseUser.mockReturnValue({
        user: null,
      })
      mockUseClerk.mockReturnValue({
        openSignIn: vi.fn(),
        signOut: vi.fn(),
      })
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
    const mockClerkUser = {
      id: 'user_123',
      fullName: 'Test User',
      primaryEmailAddress: {
        emailAddress: 'test@example.com',
      },
      imageUrl: 'https://example.com/avatar.jpg',
    }

    beforeEach(() => {
      mockUseAuth.mockReturnValue({
        isSignedIn: true,
      })
      mockUseUser.mockReturnValue({
        user: mockClerkUser,
      })
      mockUseClerk.mockReturnValue({
        openSignIn: vi.fn(),
        signOut: vi.fn(),
      })
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
      mockUseAuth.mockReturnValue({
        isSignedIn: false,
      })
      mockUseUser.mockReturnValue({
        user: null,
      })
      mockUseClerk.mockReturnValue({
        openSignIn: vi.fn(),
        signOut: vi.fn(),
      })
    })

    it('has sticky positioning', () => {
      renderHeader()

      const header = screen.getByRole('banner')
      expect(header).toHaveClass('sticky')
      expect(header).toHaveClass('top-0')
    })

    it('has z-index for stacking', () => {
      renderHeader()

      const header = screen.getByRole('banner')
      expect(header).toHaveClass('z-50')
    })

    it('has border-bottom', () => {
      renderHeader()

      const header = screen.getByRole('banner')
      expect(header).toHaveClass('border-b')
    })

    it('has fixed height', () => {
      renderHeader()

      const header = screen.getByRole('banner')
      expect(header).toHaveClass('h-16')
    })
  })

  describe('logo link', () => {
    it('links to dashboard when authenticated', () => {
      mockUseAuth.mockReturnValue({
        isSignedIn: true,
      })
      mockUseUser.mockReturnValue({
        user: { id: 'user_123', fullName: 'Test' },
      })
      mockUseClerk.mockReturnValue({
        openSignIn: vi.fn(),
        signOut: vi.fn(),
      })

      renderHeader()

      const logoLink = screen.getByRole('link', { name: /boostedbudget/i })
      expect(logoLink).toHaveAttribute('href', '/dashboard')
    })

    it('links to home when not authenticated', () => {
      mockUseAuth.mockReturnValue({
        isSignedIn: false,
      })
      mockUseUser.mockReturnValue({
        user: null,
      })
      mockUseClerk.mockReturnValue({
        openSignIn: vi.fn(),
        signOut: vi.fn(),
      })

      renderHeader()

      const logoLink = screen.getByRole('link', { name: /boostedbudget/i })
      expect(logoLink).toHaveAttribute('href', '/')
    })
  })

  describe('accepts custom className', () => {
    it('applies custom className to header', () => {
      mockUseAuth.mockReturnValue({
        isSignedIn: false,
      })
      mockUseUser.mockReturnValue({
        user: null,
      })
      mockUseClerk.mockReturnValue({
        openSignIn: vi.fn(),
        signOut: vi.fn(),
      })

      render(
        <BrowserRouter>
          <Header className="custom-class" />
        </BrowserRouter>
      )

      const header = screen.getByRole('banner')
      expect(header).toHaveClass('custom-class')
    })
  })
})
