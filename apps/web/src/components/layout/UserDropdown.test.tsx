import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { BrowserRouter } from 'react-router'
import { UserDropdown } from './UserDropdown'

const mockUser = {
  name: 'Test User',
  email: 'test@example.com',
  picture: 'https://example.com/avatar.jpg',
}

const mockOnLogout = mock(() => {})

describe('UserDropdown', () => {
  beforeEach(() => {
    mockOnLogout.mockClear()
  })

  const renderDropdown = (user = mockUser) =>
    render(
      <BrowserRouter>
        <UserDropdown user={user} onLogout={mockOnLogout} />
      </BrowserRouter>
    )

  it('renders avatar button with user menu aria-label', () => {
    renderDropdown()

    const trigger = screen.getByRole('button', { name: /user menu/i })
    expect(trigger).toBeInTheDocument()
  })

  it('opens dropdown on click', async () => {
    const user = userEvent.setup()
    renderDropdown()

    const trigger = screen.getByRole('button', { name: /user menu/i })
    await user.click(trigger)

    await waitFor(() => {
      expect(screen.getByText(mockUser.name)).toBeInTheDocument()
      expect(screen.getByText(mockUser.email)).toBeInTheDocument()
    })
  })

  it('shows menu items in dropdown', async () => {
    const user = userEvent.setup()
    renderDropdown()

    await user.click(screen.getByRole('button', { name: /user menu/i }))

    await waitFor(() => {
      expect(screen.getByRole('menuitem', { name: /view profile/i })).toBeInTheDocument()
      expect(screen.getByRole('menuitem', { name: /privacy settings/i })).toBeInTheDocument()
      expect(screen.getByRole('menuitem', { name: /log out/i })).toBeInTheDocument()
    })
  })

  // Issue #1666: the privacy entry linked to `/profile#privacy`, a dead anchor —
  // no element with `id="privacy"` exists, and ProfilePage drives its tabs through
  // `useTabState('tab', 'profile', validTabs)`. Assert the real mechanism
  // (`?tab=privacy`) so a future revert to the hash cannot pass silently.
  it('links Privacy Settings to /profile?tab=privacy', async () => {
    const user = userEvent.setup()
    renderDropdown()

    await user.click(screen.getByRole('button', { name: /user menu/i }))

    await waitFor(() => {
      expect(screen.getByRole('menuitem', { name: /privacy settings/i })).toBeInTheDocument()
    })

    const privacyLink = screen.getByRole('menuitem', { name: /privacy settings/i })
    expect(privacyLink).toHaveAttribute('href', '/profile?tab=privacy')
  })

  it('calls onLogout when logout clicked', async () => {
    const user = userEvent.setup()
    renderDropdown()

    await user.click(screen.getByRole('button', { name: /user menu/i }))

    await waitFor(() => {
      expect(screen.getByRole('menuitem', { name: /log out/i })).toBeInTheDocument()
    })

    await user.click(screen.getByRole('menuitem', { name: /log out/i }))

    expect(mockOnLogout).toHaveBeenCalledTimes(1)
  })

  it('closes dropdown on Escape key', async () => {
    const user = userEvent.setup()
    renderDropdown()

    await user.click(screen.getByRole('button', { name: /user menu/i }))

    await waitFor(() => {
      expect(screen.getByText(mockUser.name)).toBeInTheDocument()
    })

    await user.keyboard('{Escape}')

    await waitFor(() => {
      expect(screen.queryByText(mockUser.name)).not.toBeInTheDocument()
    })
  })

  it('shows fallback initials when no picture', () => {
    renderDropdown({ ...mockUser, picture: undefined })

    expect(screen.getByText('TU')).toBeInTheDocument()
  })

  // Issue #1514: the avatar's `alt` attribute interpolated the user's name
  // (`alt={user.name || 'User'}`). PostHog session replay records DOM attributes
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
      renderDropdown()

      await user.click(screen.getByRole('button', { name: /user menu/i }))

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

  it('accepts custom className', () => {
    render(
      <BrowserRouter>
        <UserDropdown user={mockUser} onLogout={mockOnLogout} className="custom-class" />
      </BrowserRouter>
    )

    const container = screen.getByRole('button', { name: /user menu/i }).parentElement
    expect(container).toHaveClass('custom-class')
  })
})
