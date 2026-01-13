import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { BrowserRouter } from 'react-router-dom'
import { MobileNav } from './MobileNav'

const mockUser = {
  name: 'Test User',
  email: 'test@example.com',
  picture: 'https://example.com/avatar.jpg',
}

const mockOnLogout = mock(() => {})
const mockOnLogin = mock(() => {})

describe('MobileNav', () => {
  beforeEach(() => {
    mockOnLogout.mockClear()
    mockOnLogin.mockClear()
  })

  const renderMobileNav = (isAuthenticated = true, user: typeof mockUser | null = mockUser) =>
    render(
      <BrowserRouter>
        <MobileNav
          isAuthenticated={isAuthenticated}
          user={user}
          onLogout={mockOnLogout}
          onLogin={mockOnLogin}
        />
      </BrowserRouter>
    )

  it('renders hamburger menu button with open menu aria-label', () => {
    renderMobileNav()

    expect(screen.getByRole('button', { name: /open menu/i })).toBeInTheDocument()
  })

  it('opens sheet on click', async () => {
    const user = userEvent.setup()
    renderMobileNav()

    await user.click(screen.getByRole('button', { name: /open menu/i }))

    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })
  })

  it('shows content in sheet when authenticated', async () => {
    const user = userEvent.setup()
    renderMobileNav()

    await user.click(screen.getByRole('button', { name: /open menu/i }))

    await waitFor(() => {
      expect(screen.getByText('BoostedBudget')).toBeInTheDocument()
      expect(screen.getByRole('link', { name: /dashboard/i })).toBeInTheDocument()
      expect(screen.getByRole('link', { name: /accounts/i })).toBeInTheDocument()
      expect(screen.getByRole('link', { name: /transactions/i })).toBeInTheDocument()
      expect(screen.getByRole('link', { name: /budgets/i })).toBeInTheDocument()
      expect(screen.getByText(mockUser.name)).toBeInTheDocument()
      expect(screen.getByText(mockUser.email)).toBeInTheDocument()
      expect(screen.getByRole('link', { name: /view profile/i })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /log out/i })).toBeInTheDocument()
    })
  })

  it('shows login button when not authenticated', async () => {
    const user = userEvent.setup()
    renderMobileNav(false, null)

    await user.click(screen.getByRole('button', { name: /open menu/i }))

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /log in/i })).toBeInTheDocument()
    })
  })

  it('does not show navigation links when not authenticated', async () => {
    const user = userEvent.setup()
    renderMobileNav(false, null)

    await user.click(screen.getByRole('button', { name: /open menu/i }))

    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })

    expect(screen.queryByRole('link', { name: /dashboard/i })).not.toBeInTheDocument()
  })

  it('calls onLogout when logout clicked', async () => {
    const user = userEvent.setup()
    renderMobileNav()

    await user.click(screen.getByRole('button', { name: /open menu/i }))

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /log out/i })).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: /log out/i }))

    expect(mockOnLogout).toHaveBeenCalledTimes(1)
  })

  it('calls onLogin when login clicked', async () => {
    const user = userEvent.setup()
    renderMobileNav(false, null)

    await user.click(screen.getByRole('button', { name: /open menu/i }))

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /log in/i })).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: /log in/i }))

    expect(mockOnLogin).toHaveBeenCalledTimes(1)
  })

  it('closes sheet when navigation link is clicked', async () => {
    const user = userEvent.setup()
    renderMobileNav()

    await user.click(screen.getByRole('button', { name: /open menu/i }))

    await waitFor(() => {
      expect(screen.getByRole('link', { name: /dashboard/i })).toBeInTheDocument()
    })

    await user.click(screen.getByRole('link', { name: /dashboard/i }))

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
  })

  it('accepts custom className', () => {
    render(
      <BrowserRouter>
        <MobileNav
          isAuthenticated={true}
          user={mockUser}
          onLogout={mockOnLogout}
          onLogin={mockOnLogin}
          className="custom-class"
        />
      </BrowserRouter>
    )

    const trigger = screen.getByRole('button', { name: /open menu/i })
    expect(trigger).toHaveClass('custom-class')
  })
})
