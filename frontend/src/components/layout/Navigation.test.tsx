import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { Navigation } from './Navigation'

describe('Navigation', () => {
  const renderWithRouter = (initialEntries = ['/dashboard']) => {
    return render(
      <MemoryRouter initialEntries={initialEntries}>
        <Navigation />
      </MemoryRouter>
    )
  }

  it('renders all navigation links', () => {
    renderWithRouter()

    expect(screen.getByRole('link', { name: /dashboard/i })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /accounts/i })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /transactions/i })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /budgets/i })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /auto-cat/i })).toBeInTheDocument()
  })

  it('does NOT render Profile link (moved to dropdown)', () => {
    renderWithRouter()

    expect(screen.queryByRole('link', { name: /^profile$/i })).not.toBeInTheDocument()
  })

  it('applies active styling to current page link', () => {
    renderWithRouter(['/dashboard'])

    const dashboardLink = screen.getByRole('link', { name: /dashboard/i })
    expect(dashboardLink).toHaveClass('text-foreground')
    expect(dashboardLink).toHaveClass('font-semibold')
  })

  it('applies inactive styling to non-current links', () => {
    renderWithRouter(['/dashboard'])

    const accountsLink = screen.getByRole('link', { name: /accounts/i })
    expect(accountsLink).toHaveClass('text-muted-foreground')
    expect(accountsLink).not.toHaveClass('font-semibold')
  })

  it('has correct href attributes', () => {
    renderWithRouter()

    expect(screen.getByRole('link', { name: /dashboard/i })).toHaveAttribute('href', '/dashboard')
    expect(screen.getByRole('link', { name: /accounts/i })).toHaveAttribute('href', '/accounts')
    expect(screen.getByRole('link', { name: /transactions/i })).toHaveAttribute('href', '/transactions')
    expect(screen.getByRole('link', { name: /budgets/i })).toHaveAttribute('href', '/budgets')
    expect(screen.getByRole('link', { name: /auto-cat/i })).toHaveAttribute('href', '/auto-categorization')
  })

  it('renders with main navigation aria-label', () => {
    renderWithRouter()

    expect(screen.getByRole('navigation', { name: /main navigation/i })).toBeInTheDocument()
  })

  it('accepts custom className', () => {
    render(
      <MemoryRouter>
        <Navigation className="custom-class" />
      </MemoryRouter>
    )

    const nav = screen.getByRole('navigation')
    expect(nav).toHaveClass('custom-class')
  })

  it('supports vertical orientation', () => {
    render(
      <MemoryRouter>
        <Navigation orientation="vertical" />
      </MemoryRouter>
    )

    const nav = screen.getByRole('navigation')
    expect(nav).toHaveClass('flex-col')
  })

  it('calls onLinkClick when a link is clicked', async () => {
    const user = userEvent.setup()
    const onLinkClick = vi.fn()

    render(
      <MemoryRouter>
        <Navigation onLinkClick={onLinkClick} />
      </MemoryRouter>
    )

    await user.click(screen.getByRole('link', { name: /dashboard/i }))
    expect(onLinkClick).toHaveBeenCalledTimes(1)
  })
})
