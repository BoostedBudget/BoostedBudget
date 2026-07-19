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
      expect(screen.getByRole('link', { name: /planning/i })).toBeInTheDocument()
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

  // Issue #1398 removed the PostHog feature-flag kill-switch prop that used to gate
  // MobileNav's mobile Log In button (pre-removal render guard was
  // `!isAuthenticated && <that prop> && (<Button>...Log In</Button>)`). That prop no
  // longer exists on MobileNav's props, so there is nothing left to pass or gate on.
  // The test below verifies the mobile Log In button renders unconditionally for a
  // signed-out user post-removal. Note: since the prop is gone, this test would also
  // have passed against the OLD component (which defaulted the prop to `true`) — it
  // is not a regression test proving the old gated-off behavior is now bypassed, only
  // confirmation that the button renders by default today. Do not reintroduce that
  // gating prop here.
  describe('login button always renders when signed out (no feature-flag gating prop)', () => {
    it('renders login button regardless', async () => {
      const user = userEvent.setup()
      render(
        <BrowserRouter>
          <MobileNav
            isAuthenticated={false}
            user={null}
            onLogout={mockOnLogout}
            onLogin={mockOnLogin}
          />
        </BrowserRouter>
      )

      await user.click(screen.getByRole('button', { name: /open menu/i }))

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /log in/i })).toBeInTheDocument()
      })
    })
  })
})
