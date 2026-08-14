import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { BrowserRouter } from 'react-router'
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

  // Issue #1514: the avatar's `alt` attribute interpolated the user's name
  // (`alt={user?.name || 'User'}`). PostHog session replay records DOM attributes
  // verbatim — data-ph-mask masks element TEXT only, never attribute values — so the
  // name must never appear in any attribute. Assert on the attribute itself (not the
  // accessible name), so a future "simplification" back to a plain name-bearing alt
  // fails loudly instead of passing on an unrelated accessible-name query.
  //
  // Radix's AvatarImage only mounts an <img> once its internal `new window.Image()`
  // probe reports `complete && naturalWidth > 0` — which never happens in happy-dom
  // (no real network fetch), so the fallback initials render instead and the alt
  // attribute never appears in the DOM at all. Stub `window.Image` so the probe
  // resolves as "loaded" synchronously, forcing the real <img alt> to mount.
  it('never puts the user name into any DOM attribute (issue #1514)', async () => {
    const OriginalImage = window.Image
    class ImmediatelyLoadedImage {
      complete = true
      naturalWidth = 1
      src = ''
      addEventListener() {}
      removeEventListener() {}
    }
    // @ts-expect-error -- test stub, not a full HTMLImageElement
    window.Image = ImmediatelyLoadedImage

    try {
      const user = userEvent.setup()
      renderMobileNav()

      await user.click(screen.getByRole('button', { name: /open menu/i }))

      await waitFor(() => {
        expect(screen.getByText(mockUser.name)).toBeInTheDocument()
      })

      // Confirm the <img> actually mounted, so this test truly exercises the alt attribute.
      const avatarImg = document.body.querySelector('img[data-slot="avatar-image"]')
      expect(avatarImg).not.toBeNull()

      for (const el of document.body.querySelectorAll('*')) {
        for (const attr of el.getAttributeNames()) {
          const value = (el.getAttribute(attr) ?? '').toLowerCase()
          expect(value).not.toContain(mockUser.name.toLowerCase())
          expect(value).not.toContain(mockUser.email.toLowerCase())
        }
      }
    } finally {
      window.Image = OriginalImage
    }
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
