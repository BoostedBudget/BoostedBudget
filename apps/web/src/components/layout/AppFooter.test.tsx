/**
 * AppFooter Tests
 *
 * Tests for the authenticated app footer.
 * Issue #837: Footer cleanup - remove internal jargon, add useful links
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

  it('renders dynamic copyright year (not hardcoded 2025)', () => {
    renderWithRouter(<AppFooter />)
    const currentYear = new Date().getFullYear()
    expect(screen.getByText(new RegExp(String(currentYear)))).toBeInTheDocument()
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

  it('renders "Contact" link with mailto:support@boostedbudget.com', () => {
    renderWithRouter(<AppFooter />)
    const link = screen.getByRole('link', { name: /contact/i })
    expect(link).toBeInTheDocument()
    expect(link).toHaveAttribute('href', 'mailto:support@boostedbudget.com')
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
