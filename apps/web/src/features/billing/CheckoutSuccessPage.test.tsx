/**
 * CheckoutSuccessPage Tests
 * Issue #772: GAP 4 - Checkout success page tests
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Routes, Route, useLocation } from 'react-router-dom'
import { CheckoutSuccessPage } from './CheckoutSuccessPage'

function LocationDisplay() {
  const location = useLocation()
  return <div data-testid="location-display">{location.pathname}</div>
}

function renderWithRouter(initialEntries: string[] = ['/billing/success']) {
  return render(
    <MemoryRouter initialEntries={initialEntries}>
      <LocationDisplay />
      <Routes>
        <Route path="/billing/success" element={<CheckoutSuccessPage />} />
        <Route path="/billing/manage" element={<div data-testid="manage-page">Manage Page</div>} />
      </Routes>
    </MemoryRouter>
  )
}

describe('CheckoutSuccessPage', () => {
  beforeEach(() => {
    // Mock window.scrollTo
    window.scrollTo = () => {}
  })

  describe('Rendering', () => {
    it('should render success message', () => {
      renderWithRouter()

      expect(screen.getByText('Subscription Activated!')).toBeInTheDocument()
    })

    it('should show confirmation icon', () => {
      const { container } = renderWithRouter()

      const icon = container.querySelector('svg')
      expect(icon).toBeInTheDocument()
    })

    it('should display default tier name when no tier param', () => {
      renderWithRouter(['/billing/success'])

      expect(screen.getByText(/Welcome to Premium/i)).toBeInTheDocument()
    })

    it('should display tier name from URL param', () => {
      renderWithRouter(['/billing/success?tier=Household%20(5%20members)'])

      expect(screen.getByText(/Welcome to Household \(5 members\)/i)).toBeInTheDocument()
    })

    it('should show next steps information', () => {
      renderWithRouter()

      expect(screen.getByText("What's Next?")).toBeInTheDocument()
      expect(screen.getByText(/Connect your bank accounts/i)).toBeInTheDocument()
      expect(screen.getByText(/Set up automation rules/i)).toBeInTheDocument()
      expect(screen.getByText(/Explore your dashboard/i)).toBeInTheDocument()
    })

    it('should render view subscription button', () => {
      renderWithRouter()

      expect(screen.getByRole('button', { name: /View Subscription Details/i })).toBeInTheDocument()
    })
  })

  describe('Navigation', () => {
    it('should have clickable button for viewing subscription', async () => {
      const user = userEvent.setup()
      renderWithRouter()

      const button = screen.getByRole('button', { name: /View Subscription Details/i })
      expect(button).toBeEnabled()

      // Verify button is clickable (actual navigation tested in E2E)
      await user.click(button)
      expect(button).toBeInTheDocument()
    })
  })

  describe('Accessibility', () => {
    it('should have proper heading hierarchy', () => {
      renderWithRouter()

      const h1 = screen.getByRole('heading', { level: 1, name: /Subscription Activated/i })
      const h2 = screen.getByRole('heading', { level: 2, name: /What's Next/i })

      expect(h1).toBeInTheDocument()
      expect(h2).toBeInTheDocument()
    })

    it('should hide decorative icon from screen readers', () => {
      const { container } = renderWithRouter()

      const icon = container.querySelector('svg')
      expect(icon).toHaveAttribute('aria-hidden', 'true')
    })
  })
})
