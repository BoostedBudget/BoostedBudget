import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { LogoutButton } from './LogoutButton'

// Mock Auth0
const mockUseAuth0 = vi.hoisted(() => vi.fn())

vi.mock('@auth0/auth0-react', () => ({
  useAuth0: mockUseAuth0,
}))

describe('LogoutButton', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('should render logout button', () => {
    mockUseAuth0.mockReturnValue({
      logout: vi.fn(),
    })

    render(<LogoutButton />)
    expect(screen.getByRole('button', { name: /log out/i })).toBeInTheDocument()
  })

  it('should call logout with returnTo when clicked', async () => {
    const mockLogout = vi.fn()
    mockUseAuth0.mockReturnValue({
      logout: mockLogout,
    })

    const user = userEvent.setup()
    render(<LogoutButton />)

    const button = screen.getByRole('button', { name: /log out/i })
    await user.click(button)

    expect(mockLogout).toHaveBeenCalledWith({
      logoutParams: {
        returnTo: window.location.origin,
      },
    })
  })

  it('should pass custom className if provided', () => {
    mockUseAuth0.mockReturnValue({
      logout: vi.fn(),
    })

    render(<LogoutButton className="custom-class" />)
    const button = screen.getByRole('button', { name: /log out/i })
    expect(button).toHaveClass('custom-class')
  })

  it('should be disabled when disabled prop is true', () => {
    mockUseAuth0.mockReturnValue({
      logout: vi.fn(),
    })

    render(<LogoutButton disabled />)
    const button = screen.getByRole('button', { name: /log out/i })
    expect(button).toBeDisabled()
  })
})
