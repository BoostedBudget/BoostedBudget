/**
 * PrivacyPolicy Tests
 *
 * Tests for the Privacy Policy page.
 * Issue #858: Legal pages for Paddle domain verification
 */

import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
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

  it('renders correct Last Updated date (July 18, 2026)', () => {
    renderWithRouter(<PrivacyPolicy />)
    expect(screen.getByText(/july 18, 2026/i)).toBeInTheDocument()
  })

  it('renders household data sharing as a consent-based lawful basis (Section 3.1)', () => {
    renderWithRouter(<PrivacyPolicy />)
    const consentBasisItem = screen.getByText(/analytics cookies \(posthog\), marketing emails/i)
    expect(consentBasisItem.textContent).toMatch(/household data sharing/i)
  })

  it('renders Household Members data-sharing subsection with accurate visible/never-shared lists (Section 9)', () => {
    renderWithRouter(<PrivacyPolicy />)
    expect(screen.getByText(/9\.1 household data sharing/i)).toBeInTheDocument()
    expect(screen.getAllByText(/household members/i).length).toBeGreaterThan(0)
    // Visible to household members
    expect(screen.getByText(/account balances/i)).toBeInTheDocument()
    expect(screen.getByText(/net worth contribution/i)).toBeInTheDocument()
    expect(screen.getByText(/spending activity, including merchant names/i)).toBeInTheDocument()
    expect(screen.getByText(/income sources and savings-rate figures/i)).toBeInTheDocument()
    // Never shared with household members
    expect(screen.getByText(/individual transaction line items/i)).toBeInTheDocument()
    // Covers both unmasked and masked account numbers — the household aggregate response
    // never serializes even a masked/last-4 account number (only id/name/balance).
    expect(screen.getByText(/full account numbers or masks/i)).toBeInTheDocument()
    expect(screen.getByText(/login credentials/i)).toBeInTheDocument()
  })

  it('describes both household consent moments and per-account revocation (Section 9)', () => {
    renderWithRouter(<PrivacyPolicy />)
    // Consent at invite-join
    expect(screen.getByText(/before joining a household/i)).toBeInTheDocument()
    // Consent at newly-linked-account sharing
    expect(screen.getByText(/accounts you link.*after joining a household/i)).toBeInTheDocument()
    // Revocation via hiding
    expect(screen.getByText(/hide.*account.*household/i)).toBeInTheDocument()
  })

  it('scopes the recorded-consent language to invite-join only, not post-join account sharing (Section 9.1)', () => {
    renderWithRouter(<PrivacyPolicy />)
    // The "recorded... version identifier and timestamp" claim describes the versioned
    // consent snapshot written by AcceptInvite (backend household_service.go), which only
    // happens at invite-join time. Post-join newly-linked-account sharing only writes a
    // household_account_shares visibility row (SetShareVisibility), not a versioned
    // consent entry, so that paragraph must not carry this claim.
    const recordedText = screen.getByText(/version identifier and timestamp/i)
    expect(recordedText.textContent).toMatch(/before joining a household/i)
    expect(recordedText.textContent).not.toMatch(/after joining a household/i)
  })
})
