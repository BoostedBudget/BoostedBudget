/**
 * AppFooter Tests
 *
 * Tests for the unified site-wide footer.
 * Issue #950: Unify footer component — single template used across all pages
 */

import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
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

  it('renders all 5 navigation links', () => {
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
})
