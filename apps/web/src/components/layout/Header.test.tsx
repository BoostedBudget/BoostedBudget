/**
 * Header Component Tests
 * Issue #575: Migrated to bun:test with centralized Clerk mocks
 */

import { describe, it, expect, beforeEach, afterEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { BrowserRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Header } from './Header'
import { mockUseAuth, mockUseUser, mockUseClerk } from '@/test/mocks/clerk'
import { mockUseFeatureFlagEnabled } from '@/test/mocks/posthog'
import { mockUseGetTransactionReviewSummary } from '@/test/mocks/transactions-api'

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

const renderHeader = (props?: object) => {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Header {...props} />
      </BrowserRouter>
    </QueryClientProvider>
  )
}

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

  describe('ReviewInboxButton', () => {
    beforeEach(() => {
      setupAuthenticatedMocks()
    })

    it('shows ReviewInboxButton with count badge when there are unreviewed transactions', () => {
      mockUseGetTransactionReviewSummary.mockReturnValue({
        data: {
          unreviewed_count: 3,
          reviewed_by_rule_count: 0,
          reviewed_by_manual_count: 0,
          reviewed_by_plaid_count: 0,
        },
        isLoading: false,
        isError: false,
      })
      renderHeader()
      expect(screen.getByRole('link', { name: /review inbox, 3 unreviewed/i })).toBeInTheDocument()
      expect(screen.getByText('3')).toBeInTheDocument()
    })

    it('shows ReviewInboxButton without badge when 0 unreviewed', () => {
      mockUseGetTransactionReviewSummary.mockReturnValue({
        data: {
          unreviewed_count: 0,
          reviewed_by_rule_count: 0,
          reviewed_by_manual_count: 0,
          reviewed_by_plaid_count: 0,
        },
        isLoading: false,
        isError: false,
      })
      renderHeader()
      expect(screen.getByRole('link', { name: /^review inbox$/i })).toBeInTheDocument()
      expect(screen.queryByText('0')).not.toBeInTheDocument()
    })

    it('shows ReviewInboxButton without badge when summary is loading', () => {
      mockUseGetTransactionReviewSummary.mockReturnValue({
        data: undefined,
        isLoading: true,
        isError: false,
      })
      renderHeader()
      expect(screen.getByRole('link', { name: /^review inbox$/i })).toBeInTheDocument()
    })
  })

  describe('when login_enabled flag is false (flag no longer gates the login button)', () => {
    beforeEach(() => {
      setupUnauthenticatedMocks()
      window.ENV = { POSTHOG_KEY: 'test-key' }
      mockUseFeatureFlagEnabled.mockReturnValue(false)
    })

    afterEach(() => {
      window.ENV = undefined
      mockUseFeatureFlagEnabled.mockReturnValue(undefined)
    })

    it('still renders login button when flag is false', () => {
      renderHeader()
      expect(screen.getByRole('button', { name: /log in/i })).toBeInTheDocument()
    })

    it('still renders logo when flag is false', () => {
      renderHeader()
      expect(screen.getByText(/boostedbudget/i)).toBeInTheDocument()
    })

    it('still renders mobile nav trigger when flag is false', () => {
      renderHeader()
      expect(screen.getByRole('button', { name: /open menu/i })).toBeInTheDocument()
    })
  })
})
