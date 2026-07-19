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

  // Converted regression check for the removed `loginEnabled` kill-switch (#1398).
  // Pre-removal, MobileNav's render guard was
  // `!isAuthenticated && loginEnabled && (<Button>...Log In</Button>)`, so passing
  // `loginEnabled={false}` hid this button. That guard — and the prop entirely — is
  // gone now, so there is no prop to gate on; the test below asserts the button
  // renders unconditionally for a signed-out user, which is the intentional
  // replacement coverage for the old `loginEnabled=false` case (verified against a
  // scratch copy of the pre-removal source: flipping that old test's assertion to
  // expect the button present, while still passing `loginEnabled={false}`, fails
  // against the pre-removal component — see task-1-report.md for the verification
  // transcript). Do not reintroduce a `loginEnabled` prop here.
  describe('login button always renders when signed out (no loginEnabled prop)', () => {
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
