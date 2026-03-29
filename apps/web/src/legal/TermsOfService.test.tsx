/**
 * TermsOfService Tests
 *
 * Tests for the full Terms of Service page.
 * Issue #858: Legal pages for Paddle domain verification
 */

import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { TermsOfService } from './TermsOfService'

const renderWithRouter = (component: React.ReactElement) =>
  render(<MemoryRouter>{component}</MemoryRouter>)

describe('TermsOfService', () => {
  it('renders "Terms of Service" heading', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.getByRole('heading', { name: /terms of service/i })).toBeInTheDocument()
  })

  it('renders "Last Updated" date', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.getByText(/last updated: march/i)).toBeInTheDocument()
  })

  it('renders all major sections', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.getByText(/acceptance of terms/i)).toBeInTheDocument()
    expect(screen.getByText(/service description/i)).toBeInTheDocument()
    expect(screen.getByText(/subscription plans and billing/i)).toBeInTheDocument()
    expect(screen.getByText(/refund and cancellation policy/i)).toBeInTheDocument()
    expect(screen.getByText(/trial expiration and read-only access/i)).toBeInTheDocument()
    expect(screen.getByText(/dispute resolution/i)).toBeInTheDocument()
    expect(screen.getByText(/governing law/i)).toBeInTheDocument()
  })

  it('renders no refunds policy', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.getByText(/all subscription payments are non-refundable/i)).toBeInTheDocument()
  })

  it('renders free trial terms including no ads', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.getByText(/60-day free trial/i)).toBeInTheDocument()
    expect(screen.getByText(/no credit card is required/i)).toBeInTheDocument()
    expect(screen.getByText(/no advertisements are displayed/i)).toBeInTheDocument()
  })

  it('renders current subscription tiers and prices', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.getByText(/\$8\.99\/month/i)).toBeInTheDocument()
    expect(screen.getByText(/\$12\.99\/month/i)).toBeInTheDocument()
    expect(screen.getByText(/\$18\.99\/month/i)).toBeInTheDocument()
  })

  it('renders Paddle as payment processor', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.getByText(/payments are processed by/i)).toBeInTheDocument()
  })

  it('renders Delaware governing law', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.getByText(/state of delaware/i)).toBeInTheDocument()
  })

  it('renders link to Privacy Policy', () => {
    renderWithRouter(<TermsOfService />)
    const privacyLinks = screen.getAllByRole('link', { name: /privacy policy/i })
    expect(privacyLinks.length).toBeGreaterThan(0)
    expect(privacyLinks[0]).toHaveAttribute('href', '/legal/privacy-policy')
  })

  it('does not contain placeholder coming soon text', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.queryByText(/coming soon/i)).not.toBeInTheDocument()
  })

  it('renders support contact email', () => {
    renderWithRouter(<TermsOfService />)
    const emails = screen.getAllByText(/support@boostedbudget\.com/i)
    expect(emails.length).toBeGreaterThan(0)
  })
})
