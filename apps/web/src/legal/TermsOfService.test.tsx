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
    expect(privacyLinks.some((link) => link.getAttribute('href') === '/legal/privacy-policy')).toBe(true)
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

  it('renders fintech disclaimers', () => {
    renderWithRouter(<TermsOfService />)
    const bankTexts = screen.getAllByText(/not a bank/i)
    expect(bankTexts.length).toBeGreaterThan(0)
    expect(screen.getByText(/not fdic-insured or sipc-protected/i)).toBeInTheDocument()
  })

  it('renders Paddle as Merchant of Record', () => {
    renderWithRouter(<TermsOfService />)
    const morTexts = screen.getAllByText(/merchant of record/i)
    expect(morTexts.length).toBeGreaterThan(0)
  })

  it('renders PostHog in third-party services', () => {
    renderWithRouter(<TermsOfService />)
    const posthogTexts = screen.getAllByText(/posthog/i)
    expect(posthogTexts.length).toBeGreaterThan(0)
  })

  it('renders General Provisions section', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.getByText(/general provisions/i)).toBeInTheDocument()
    expect(screen.getAllByText(/indemnification/i).length).toBeGreaterThan(0)
    expect(screen.getByText(/force majeure/i)).toBeInTheDocument()
    expect(screen.getAllByText(/severability/i).length).toBeGreaterThan(0)
    expect(screen.getAllByText(/entire agreement/i).length).toBeGreaterThan(0)
  })

  it('renders link to Cookie Policy', () => {
    renderWithRouter(<TermsOfService />)
    const cookieLinks = screen.getAllByRole('link', { name: /cookie policy/i })
    expect(cookieLinks.length).toBeGreaterThan(0)
    expect(cookieLinks.some((link) => link.getAttribute('href') === '/legal/cookie-policy')).toBe(true)
  })
})
