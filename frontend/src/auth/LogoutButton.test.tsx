import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { LogoutButton } from './LogoutButton'

// Mock Clerk
const mockUseClerk = vi.hoisted(() => vi.fn())

vi.mock('@clerk/clerk-react', () => ({
  useClerk: mockUseClerk,
}))

describe('LogoutButton', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('should render logout button', () => {
    mockUseClerk.mockReturnValue({
      signOut: vi.fn(),
    })

    render(<LogoutButton />)
    expect(screen.getByRole('button', { name: /log out/i })).toBeInTheDocument()
  })

  it('should call signOut with redirectUrl when clicked', async () => {
    const mockSignOut = vi.fn()
    mockUseClerk.mockReturnValue({
      signOut: mockSignOut,
    })

    const user = userEvent.setup({ delay: null })
    render(<LogoutButton />)

    const button = screen.getByRole('button', { name: /log out/i })
    await user.click(button)

    expect(mockSignOut).toHaveBeenCalledWith({
      redirectUrl: window.location.origin,
    })
  })

  it('should pass custom className if provided', () => {
    mockUseClerk.mockReturnValue({
      signOut: vi.fn(),
    })

    render(<LogoutButton className="custom-class" />)
    const button = screen.getByRole('button', { name: /log out/i })
    expect(button).toHaveClass('custom-class')
  })

  it('should be disabled when disabled prop is true', () => {
    mockUseClerk.mockReturnValue({
      signOut: vi.fn(),
    })

    render(<LogoutButton disabled />)
    const button = screen.getByRole('button', { name: /log out/i })
    expect(button).toBeDisabled()
  })
})
