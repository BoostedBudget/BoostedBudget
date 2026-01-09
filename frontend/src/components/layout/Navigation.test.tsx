import { describe, it, expect, mock } from 'bun:test'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { Navigation } from './Navigation'

const ROUTES = [
  { name: /dashboard/i, href: '/dashboard' },
  { name: /accounts/i, href: '/accounts' },
  { name: /transactions/i, href: '/transactions' },
  { name: /budgets/i, href: '/budgets' },
  { name: /auto-cat/i, href: '/auto-categorization' },
]

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

    ROUTES.forEach(({ name }) => {
      expect(screen.getByRole('link', { name })).toBeInTheDocument()
    })
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

    ROUTES.forEach(({ name, href }) => {
      expect(screen.getByRole('link', { name })).toHaveAttribute('href', href)
    })
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
    const onLinkClick = mock(() => {})

    render(
      <MemoryRouter>
        <Navigation onLinkClick={onLinkClick} />
      </MemoryRouter>
    )

    await user.click(screen.getByRole('link', { name: /dashboard/i }))
    expect(onLinkClick).toHaveBeenCalledTimes(1)
  })
})
