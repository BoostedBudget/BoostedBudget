/**
 * AppFooter Tests
 *
 * Tests for the unified site-wide footer.
 * Issue #950: Unify footer component — single template used across all pages
 * Issue #1411: Restructure the flat link row into four grouped columns
 * (Product, Developers, Legal, Support) and give the MCP server a named home.
 */

import { describe, it, expect } from 'bun:test'
import { render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { AppFooter } from './AppFooter'

const renderWithRouter = (component: React.ReactElement) =>
  render(<MemoryRouter>{component}</MemoryRouter>)

describe('AppFooter', () => {
  it('renders a footer semantic element', () => {
    const { container } = renderWithRouter(<AppFooter />)
    expect(container.querySelector('footer')).toBeInTheDocument()
  })

  it('renders dynamic copyright year (not hardcoded)', () => {
    renderWithRouter(<AppFooter />)
    const currentYear = new Date().getFullYear()
    expect(screen.getByText(new RegExp(String(currentYear)))).toBeInTheDocument()
  })

  it('renders "All rights reserved" in copyright text', () => {
    renderWithRouter(<AppFooter />)
    expect(screen.getByText(/all rights reserved/i)).toBeInTheDocument()
  })

  it('renders brand logo and name', () => {
    renderWithRouter(<AppFooter />)
    expect(screen.getByText('BoostedBudget')).toBeInTheDocument()
  })

  it('renders the four group headings: Product, Developers, Legal, Support', () => {
    renderWithRouter(<AppFooter />)
    expect(screen.getByRole('heading', { name: 'Product' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Developers' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Legal' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Support' })).toBeInTheDocument()
  })

  it('renders "Features" link pointing to /features', () => {
    renderWithRouter(<AppFooter />)
    const link = screen.getByRole('link', { name: /^features$/i })
    expect(link).toHaveAttribute('href', '/features')
  })

  it('renders "FAQ" link pointing to /faq', () => {
    renderWithRouter(<AppFooter />)
    const link = screen.getByRole('link', { name: /^faq$/i })
    expect(link).toHaveAttribute('href', '/faq')
  })

  it('renders "MCP Server" link pointing to /mcp', () => {
    renderWithRouter(<AppFooter />)
    const link = screen.getByRole('link', { name: /mcp server/i })
    expect(link).toBeInTheDocument()
    expect(link).toHaveAttribute('href', '/mcp')
  })

  it('renders "Privacy Policy" link pointing to /legal/privacy-policy', () => {
    renderWithRouter(<AppFooter />)
    const link = screen.getByRole('link', { name: /privacy policy/i })
    expect(link).toBeInTheDocument()
    expect(link).toHaveAttribute('href', '/legal/privacy-policy')
  })

  it('renders "Terms of Service" link pointing to /legal/terms', () => {
    renderWithRouter(<AppFooter />)
    const link = screen.getByRole('link', { name: /terms of service/i })
    expect(link).toBeInTheDocument()
    expect(link).toHaveAttribute('href', '/legal/terms')
  })

  it('renders "Cookie Policy" link pointing to /legal/cookie-policy', () => {
    renderWithRouter(<AppFooter />)
    const link = screen.getByRole('link', { name: /cookie policy/i })
    expect(link).toBeInTheDocument()
    expect(link).toHaveAttribute('href', '/legal/cookie-policy')
  })

  it('renders "Pricing" link pointing to /pricing', () => {
    renderWithRouter(<AppFooter />)
    const link = screen.getByRole('link', { name: /^pricing$/i })
    expect(link).toBeInTheDocument()
    expect(link).toHaveAttribute('href', '/pricing')
  })

  it('renders "Contact" link with mailto:support@boostedbudget.com', () => {
    renderWithRouter(<AppFooter />)
    const link = screen.getByRole('link', { name: /contact/i })
    expect(link).toBeInTheDocument()
    expect(link).toHaveAttribute('href', 'mailto:support@boostedbudget.com')
  })

  it('renders the Contact link as a real anchor element', () => {
    renderWithRouter(<AppFooter />)
    const link = screen.getByRole('link', { name: /contact/i })
    expect(link.tagName).toBe('A')
    expect(link).toHaveAttribute('href', 'mailto:support@boostedbudget.com')
  })

  it('renders all navigation links (at least 5)', () => {
    renderWithRouter(<AppFooter />)
    const links = screen.getAllByRole('link')
    expect(links.length).toBeGreaterThanOrEqual(5)
  })

  it('renders GDPR compliance text', () => {
    renderWithRouter(<AppFooter />)
    expect(screen.getByText(/gdpr/i)).toBeInTheDocument()
  })

  it('does not render internal jargon "Single Region"', () => {
    renderWithRouter(<AppFooter />)
    expect(screen.queryByText(/single region/i)).not.toBeInTheDocument()
  })

  it('does not render internal jargon "us-east-1"', () => {
    renderWithRouter(<AppFooter />)
    expect(screen.queryByText(/us-east-1/i)).not.toBeInTheDocument()
  })

  it('does not render internal jargon "MVP"', () => {
    renderWithRouter(<AppFooter />)
    expect(screen.queryByText(/\bMVP\b/)).not.toBeInTheDocument()
  })

  // Regression tripwires: growing a flat link row into a grouped footer with a
  // brand block is exactly the kind of change that introduces a second match
  // for a single-match query. These assert length === 1 explicitly so a future
  // regression fails with a clear message instead of an opaque
  // "found multiple elements" error from getByText/getByRole.
  describe('uniqueness guardrails (regression tripwires)', () => {
    it('renders the copyright year in exactly one text node', () => {
      renderWithRouter(<AppFooter />)
      const currentYear = new Date().getFullYear()
      expect(screen.getAllByText(new RegExp(String(currentYear)))).toHaveLength(1)
    })

    it('renders "BoostedBudget" as an exact match in exactly one node', () => {
      renderWithRouter(<AppFooter />)
      expect(screen.getAllByText('BoostedBudget')).toHaveLength(1)
    })

    it('has exactly one link whose accessible name matches /contact/i', () => {
      renderWithRouter(<AppFooter />)
      expect(screen.getAllByRole('link', { name: /contact/i })).toHaveLength(1)
    })

    it('has exactly one node matching /gdpr/i', () => {
      renderWithRouter(<AppFooter />)
      expect(screen.getAllByText(/gdpr/i)).toHaveLength(1)
    })

    it('has exactly one link named exactly "Pricing"', () => {
      renderWithRouter(<AppFooter />)
      expect(screen.getAllByRole('link', { name: 'Pricing' })).toHaveLength(1)
    })

    // Developers and Support are deliberately single-link columns (no other
    // developer or support page exists yet). A future contributor "balancing"
    // the columns by inventing links (e.g. "API Reference", "Help Centre")
    // should fail this test rather than slip through the >=5 total-link check.
    it('keeps Developers and Support as single-link groups', () => {
      renderWithRouter(<AppFooter />)
      const developersHeading = screen.getByRole('heading', { name: 'Developers' })
      const developersGroup = developersHeading.parentElement as HTMLElement
      expect(within(developersGroup).getAllByRole('link')).toHaveLength(1)

      const supportHeading = screen.getByRole('heading', { name: 'Support' })
      const supportGroup = supportHeading.parentElement as HTMLElement
      expect(within(supportGroup).getAllByRole('link')).toHaveLength(1)
    })
  })

  describe('accessibility', () => {
    it('every link has an accessible name', () => {
      renderWithRouter(<AppFooter />)
      const links = screen.getAllByRole('link')
      for (const link of links) {
        expect(link).toHaveAccessibleName()
      }
    })

    it('exposes each group heading as a real heading element', () => {
      renderWithRouter(<AppFooter />)
      for (const name of ['Product', 'Developers', 'Legal', 'Support']) {
        const heading = screen.getByRole('heading', { name })
        expect(heading.tagName).toMatch(/^H[1-6]$/)
      }
    })
  })
})
