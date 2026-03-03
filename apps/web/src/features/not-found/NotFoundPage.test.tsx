/**
 * NotFoundPage Tests
 *
 * Tests for the 404 page displayed when navigating to undefined routes.
 * Issue #836: Create Proper 404 Page
 */

import { describe, it, expect, beforeEach, afterEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { NotFoundPage } from './NotFoundPage'

describe('NotFoundPage', () => {
  let originalTitle: string

  beforeEach(() => {
    originalTitle = document.title
  })

  afterEach(() => {
    document.title = originalTitle
  })

  it('renders "404" display text', () => {
    render(
      <MemoryRouter>
        <NotFoundPage />
      </MemoryRouter>
    )
    expect(screen.getByText('404')).toBeInTheDocument()
  })

  it('renders "Page Not Found" as h1', () => {
    render(
      <MemoryRouter>
        <NotFoundPage />
      </MemoryRouter>
    )
    expect(screen.getByRole('heading', { level: 1, name: /page not found/i })).toBeInTheDocument()
  })

  it('renders descriptive help message', () => {
    render(
      <MemoryRouter>
        <NotFoundPage />
      </MemoryRouter>
    )
    expect(screen.getByText(/the page you.re looking for/i)).toBeInTheDocument()
  })

  it('renders "Go to Dashboard" link with href="/dashboard"', () => {
    render(
      <MemoryRouter>
        <NotFoundPage />
      </MemoryRouter>
    )
    const link = screen.getByRole('link', { name: /go to dashboard/i })
    expect(link).toBeInTheDocument()
    expect(link).toHaveAttribute('href', '/dashboard')
  })

  it('renders "Contact Support" link with mailto href', () => {
    render(
      <MemoryRouter>
        <NotFoundPage />
      </MemoryRouter>
    )
    const link = screen.getByRole('link', { name: /contact support/i })
    expect(link).toBeInTheDocument()
    expect(link).toHaveAttribute('href', 'mailto:support@boostedbudget.com')
  })

  it('sets document.title to "404 — Page Not Found | BoostedBudget"', () => {
    render(
      <MemoryRouter>
        <NotFoundPage />
      </MemoryRouter>
    )
    expect(document.title).toBe('404 — Page Not Found | BoostedBudget')
  })

  it('renders icon with aria-hidden="true"', () => {
    const { container } = render(
      <MemoryRouter>
        <NotFoundPage />
      </MemoryRouter>
    )
    const hiddenElements = container.querySelectorAll('[aria-hidden="true"]')
    expect(hiddenElements.length).toBeGreaterThan(0)
  })
})
