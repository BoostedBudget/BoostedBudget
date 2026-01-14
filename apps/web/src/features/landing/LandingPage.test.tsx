/**
 * LandingPage Tests
 *
 * Tests for the marketing landing page displayed to unauthenticated visitors.
 * Issue #699: Create Marketing Landing Page for First-Time Visitors
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { BrowserRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { LandingPage } from './LandingPage'

// Import centralized mocks
import { mockUseUser, mockUseClerk } from '@/test/mocks/clerk'
import {
  mockUseGetSubscriptionTiers,
  mockRefetchSubscriptionTiers,
} from '@/test/mocks/subscription-api'
import { mockSubscriptionTiersResponse } from '@/test/fixtures/subscriptionFixtures'

function renderWithProviders(ui: React.ReactElement) {
  return render(
    <QueryClientProvider
      client={
        new QueryClient({
          defaultOptions: {
            queries: { retry: false, gcTime: 0 },
          },
        })
      }
    >
      <BrowserRouter>{ui}</BrowserRouter>
    </QueryClientProvider>
  )
}

describe('LandingPage', () => {
  beforeEach(() => {
    // Reset all mocks
    mockUseUser.mockClear()
    mockUseClerk.mockClear()
    mockUseGetSubscriptionTiers.mockClear()
    mockRefetchSubscriptionTiers.mockClear()

    // Default: unauthenticated user
    mockUseUser.mockReturnValue({
      user: undefined,
      isSignedIn: false,
      isLoaded: true,
    })

    // Default: subscription tiers loaded
    mockUseGetSubscriptionTiers.mockReturnValue({
      data: mockSubscriptionTiersResponse,
      isLoading: false,
      error: null,
      refetch: mockRefetchSubscriptionTiers,
    })
  })

  describe('Page Structure', () => {
    it('should render without crashing', () => {
      renderWithProviders(<LandingPage />)
      expect(document.body).toBeDefined()
    })

    it('should have a single h1 heading for SEO', () => {
      renderWithProviders(<LandingPage />)
      const h1Elements = screen.getAllByRole('heading', { level: 1 })
      expect(h1Elements).toHaveLength(1)
    })

    it('should have proper semantic main element', () => {
      renderWithProviders(<LandingPage />)
      expect(screen.getByRole('main')).toBeInTheDocument()
    })
  })

  describe('Hero Section', () => {
    it('should display the main headline', () => {
      renderWithProviders(<LandingPage />)
      expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent(
        /take control of your money/i
      )
    })

    it('should display primary CTA button', () => {
      renderWithProviders(<LandingPage />)
      // There are multiple CTAs, just check at least one exists
      const ctaButtons = screen.getAllByRole('button', { name: /start free today/i })
      expect(ctaButtons.length).toBeGreaterThanOrEqual(1)
    })

    it('should display secondary CTA', () => {
      renderWithProviders(<LandingPage />)
      expect(screen.getByText(/see how it works/i)).toBeInTheDocument()
    })

    it('should display trust badges', () => {
      renderWithProviders(<LandingPage />)
      // Trust badges appear multiple times on the page
      expect(screen.getAllByText(/no credit card required/i).length).toBeGreaterThanOrEqual(1)
      expect(screen.getAllByText(/gdpr compliant/i).length).toBeGreaterThanOrEqual(1)
    })
  })

  describe('Features Section', () => {
    it('should display features section heading', () => {
      renderWithProviders(<LandingPage />)
      expect(
        screen.getByRole('heading', { name: /why choose boostedbudget/i })
      ).toBeInTheDocument()
    })

    it('should display feature items', () => {
      renderWithProviders(<LandingPage />)
      // Current 4 features in 2x2 grid
      expect(screen.getByRole('heading', { name: /all your accounts in one place/i })).toBeInTheDocument()
      expect(screen.getByRole('heading', { name: /customizable dashboard/i })).toBeInTheDocument()
      expect(screen.getByRole('heading', { name: /smart categorization/i })).toBeInTheDocument()
      expect(screen.getByRole('heading', { name: /powerful automation/i })).toBeInTheDocument()
    })
  })

  describe('How It Works Section', () => {
    it('should display how it works section heading', () => {
      renderWithProviders(<LandingPage />)
      expect(
        screen.getByRole('heading', { name: /how it works/i })
      ).toBeInTheDocument()
    })

    it('should display 3 steps', () => {
      renderWithProviders(<LandingPage />)
      // Step titles appear as headings
      expect(screen.getByRole('heading', { name: /^connect$/i })).toBeInTheDocument()
      expect(screen.getByRole('heading', { name: /^organize$/i })).toBeInTheDocument()
      expect(screen.getByRole('heading', { name: /^automate$/i })).toBeInTheDocument()
    })
  })

  describe('Social Proof Section', () => {
    it('should display trust badges', () => {
      renderWithProviders(<LandingPage />)
      // Bank-level security appears in multiple sections
      expect(screen.getAllByText(/bank-level security/i).length).toBeGreaterThanOrEqual(1)
    })
  })

  describe('Pricing Preview Section', () => {
    it('should display pricing section heading', () => {
      renderWithProviders(<LandingPage />)
      expect(
        screen.getByRole('heading', { name: /simple.*pricing/i })
      ).toBeInTheDocument()
    })

    it('should have link to full pricing page', () => {
      renderWithProviders(<LandingPage />)
      expect(screen.getByRole('link', { name: /compare all plans/i })).toHaveAttribute(
        'href',
        '/pricing'
      )
    })
  })

  describe('FAQ Section', () => {
    it('should display FAQ section heading', () => {
      renderWithProviders(<LandingPage />)
      expect(
        screen.getByRole('heading', { name: /frequently asked questions/i })
      ).toBeInTheDocument()
    })

    it('should display FAQ questions', () => {
      renderWithProviders(<LandingPage />)
      expect(screen.getByText(/is my bank data secure/i)).toBeInTheDocument()
      expect(screen.getByText(/can i cancel anytime/i)).toBeInTheDocument()
    })
  })

  describe('Final CTA Section', () => {
    it('should display final CTA heading', () => {
      renderWithProviders(<LandingPage />)
      expect(
        screen.getByRole('heading', { name: /ready to take control/i })
      ).toBeInTheDocument()
    })

    it('should display final CTA button', () => {
      renderWithProviders(<LandingPage />)
      // There should be at least 2 "Start Free Today" buttons (hero + final CTA)
      const ctaButtons = screen.getAllByRole('button', { name: /start free today/i })
      expect(ctaButtons.length).toBeGreaterThanOrEqual(2)
    })

    it('should display risk reversal messaging', () => {
      renderWithProviders(<LandingPage />)
      // These messages appear in multiple places on the page
      expect(screen.getAllByText(/2-month free trial/i).length).toBeGreaterThanOrEqual(1)
      expect(screen.getAllByText(/cancel anytime/i).length).toBeGreaterThanOrEqual(1)
    })
  })

  describe('Accessibility', () => {
    it('should have proper heading hierarchy', () => {
      renderWithProviders(<LandingPage />)

      // Only one h1
      const h1Elements = screen.getAllByRole('heading', { level: 1 })
      expect(h1Elements).toHaveLength(1)

      // Multiple h2 for sections
      const h2Elements = screen.getAllByRole('heading', { level: 2 })
      expect(h2Elements.length).toBeGreaterThanOrEqual(4)
    })

    it('should have accessible buttons', () => {
      renderWithProviders(<LandingPage />)

      const buttons = screen.getAllByRole('button')
      buttons.forEach((button) => {
        // Each button should have accessible name
        expect(button).toHaveAccessibleName()
      })
    })

    it('should have accessible links', () => {
      renderWithProviders(<LandingPage />)

      const links = screen.getAllByRole('link')
      links.forEach((link) => {
        expect(link).toHaveAccessibleName()
      })
    })
  })
})
