import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { BrowserRouter } from 'react-router-dom'
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

  const renderDropdown = (user = mockUser) => {
    return render(
      <BrowserRouter>
        <UserDropdown user={user} onLogout={mockOnLogout} />
      </BrowserRouter>
    )
  }

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
