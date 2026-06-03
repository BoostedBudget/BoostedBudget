/**
 * PrivacyPolicy Tests
 *
 * Tests for the Privacy Policy page.
 * Issue #858: Legal pages for Paddle domain verification
 */

import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { PrivacyPolicy } from './PrivacyPolicy'

const renderWithRouter = (component: React.ReactElement) =>
  render(<MemoryRouter>{component}</MemoryRouter>)

describe('PrivacyPolicy', () => {
  it('renders "Privacy Policy" heading', () => {
    renderWithRouter(<PrivacyPolicy />)
    // Use h1 specifically — h2 "12. Changes to This Privacy Policy" also matches /privacy policy/i
    const h1 = document.querySelector('h1')
    expect(h1?.textContent).toMatch(/privacy policy/i)
  })

  it('renders GDPR Article 6 legal basis table', () => {
    renderWithRouter(<PrivacyPolicy />)
    expect(screen.getByText(/contract performance/i)).toBeInTheDocument()
    expect(screen.getByText(/legitimate interests/i)).toBeInTheDocument()
    // "legal obligation" appears in both Section 3 compliance bullet and Section 3.1 GDPR table
    expect(screen.getAllByText(/legal obligation/i).length).toBeGreaterThan(0)
  })

  it('renders 6-month passive retention for cancelled accounts (Section 8.4)', () => {
    renderWithRouter(<PrivacyPolicy />)
    expect(screen.getByText(/cancelled accounts/i)).toBeInTheDocument()
    expect(screen.getAllByText(/6 months/i).length).toBeGreaterThan(0)
  })

  it('renders household member deletion cross-reference (Section 8.5)', () => {
    renderWithRouter(<PrivacyPolicy />)
    expect(screen.getByText(/removed household members/i)).toBeInTheDocument()
    expect(screen.getByText(/30 days before permanent deletion/i)).toBeInTheDocument()
  })

  it('renders Sentry data disclosure with specifics', () => {
    renderWithRouter(<PrivacyPolicy />)
    expect(screen.getByText(/device and browser information/i)).toBeInTheDocument()
    expect(screen.getByText(/application state at the time of an error/i)).toBeInTheDocument()
  })

  it('renders CPRA sensitive personal information right', () => {
    renderWithRouter(<PrivacyPolicy />)
    expect(screen.getByText(/limit the use of your sensitive personal information/i)).toBeInTheDocument()
    expect(screen.getByText(/financial account numbers, balances, and/i)).toBeInTheDocument()
  })

  it('renders Do Not Track disclosure (Section 7.2)', () => {
    renderWithRouter(<PrivacyPolicy />)
    // "Do Not Track" appears in both the h3 heading and the paragraph body
    expect(screen.getAllByText(/do not track/i).length).toBeGreaterThan(0)
    expect(screen.getByText(/no industry/i)).toBeInTheDocument()
  })

  it('renders data breach notification commitment', () => {
    renderWithRouter(<PrivacyPolicy />)
    expect(screen.getByText(/data breach notification/i)).toBeInTheDocument()
    expect(screen.getByText(/without unreasonable delay/i)).toBeInTheDocument()
  })

  it('replaces pricing table with link to /pricing page', () => {
    renderWithRouter(<PrivacyPolicy />)
    const pricingLink = screen.getByRole('link', { name: /pricing page/i })
    expect(pricingLink.getAttribute('href')).toBe('/pricing')
    // Confirm pricing amounts are gone
    expect(screen.queryByText(/\$8\.99/)).not.toBeInTheDocument()
    expect(screen.queryByText(/\$12\.99/)).not.toBeInTheDocument()
  })

  it('renders global "do not sell" statement outside CCPA section', () => {
    renderWithRouter(<PrivacyPolicy />)
    const doNotSellInstances = screen.getAllByText(/we do not sell your personal information|do not sell or share personal information/i)
    expect(doNotSellInstances.length).toBeGreaterThan(0)
  })

  it('renders CCPA rights section', () => {
    renderWithRouter(<PrivacyPolicy />)
    expect(screen.getByText(/california privacy rights/i)).toBeInTheDocument()
    expect(screen.getByText(/california resident/i)).toBeInTheDocument()
  })

  it('renders SCCs for international transfers', () => {
    renderWithRouter(<PrivacyPolicy />)
    expect(screen.getByText(/standard contractual clauses/i)).toBeInTheDocument()
  })

  it('renders link to Terms of Service', () => {
    renderWithRouter(<PrivacyPolicy />)
    const tosLinks = screen.getAllByRole('link', { name: /terms of service/i })
    expect(tosLinks.length).toBeGreaterThan(0)
    expect(tosLinks.some((link) => link.getAttribute('href') === '/legal/terms')).toBe(true)
  })

  it('renders link to Cookie Policy', () => {
    renderWithRouter(<PrivacyPolicy />)
    const cookieLinks = screen.getAllByRole('link', { name: /cookie policy/i })
    expect(cookieLinks.length).toBeGreaterThan(0)
    expect(cookieLinks.some((link) => link.getAttribute('href') === '/legal/cookie-policy')).toBe(true)
  })

  it('renders application-level encryption details (Section 4.2)', () => {
    renderWithRouter(<PrivacyPolicy />)
    // Algorithm name
    expect(screen.getByText(/AES-256-GCM/i)).toBeInTheDocument()
    // Section 2.1 forward-reference
    expect(screen.getByText(/sensitive personal and financial fields.*encrypted at the application level/i)).toBeInTheDocument()
    // Section 4.2 headings and content
    expect(screen.getAllByText(/application.level encryption/i).length).toBeGreaterThan(0)
    expect(screen.getAllByText(/display masking/i).length).toBeGreaterThan(0)
    // Masking examples
    expect(screen.getByText(/u\*\*\*@example\.com/)).toBeInTheDocument()
    expect(screen.getByText(/\*\*\*\*9012/)).toBeInTheDocument()
    // Blind indexes disclosure
    expect(screen.getAllByText(/blind index/i).length).toBeGreaterThan(0)
    // Key management
    expect(screen.getAllByText(/cryptographically derived/i).length).toBeGreaterThan(0)
  })

  it('renders correct Last Updated date (April 4, 2026)', () => {
    renderWithRouter(<PrivacyPolicy />)
    expect(screen.getByText(/april 4, 2026/i)).toBeInTheDocument()
  })
})
