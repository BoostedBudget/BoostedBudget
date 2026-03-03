/**
 * TermsOfService Tests
 *
 * Tests for the Terms of Service placeholder page.
 * Issue #837: Footer cleanup - add Terms of Service page
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

  it('renders placeholder message about upcoming content', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.getByText(/coming soon/i)).toBeInTheDocument()
  })

  it('renders "Go to Dashboard" link pointing to /dashboard', () => {
    renderWithRouter(<TermsOfService />)
    const link = screen.getByRole('link', { name: /go to dashboard/i })
    expect(link).toBeInTheDocument()
    expect(link).toHaveAttribute('href', '/dashboard')
  })
})
