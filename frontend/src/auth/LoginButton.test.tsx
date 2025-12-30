import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { LoginButton } from './LoginButton'

// Mock Clerk
const mockUseClerk = vi.hoisted(() => vi.fn())

vi.mock('@clerk/clerk-react', () => ({
  useClerk: mockUseClerk,
}))

describe('LoginButton', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('should render login button', () => {
    mockUseClerk.mockReturnValue({
      openSignIn: vi.fn(),
    })

    render(<LoginButton />)
    expect(screen.getByRole('button', { name: /log in/i })).toBeInTheDocument()
  })

  it('should call openSignIn when clicked', async () => {
    const mockOpenSignIn = vi.fn()
    mockUseClerk.mockReturnValue({
      openSignIn: mockOpenSignIn,
    })

    const user = userEvent.setup({ delay: null })
    render(<LoginButton />)

    const button = screen.getByRole('button', { name: /log in/i })
    await user.click(button)

    expect(mockOpenSignIn).toHaveBeenCalled()
  })

  it('should pass custom className if provided', () => {
    mockUseClerk.mockReturnValue({
      openSignIn: vi.fn(),
    })

    render(<LoginButton className="custom-class" />)
    const button = screen.getByRole('button', { name: /log in/i })
    expect(button).toHaveClass('custom-class')
  })

  it('should be disabled when disabled prop is true', () => {
    mockUseClerk.mockReturnValue({
      openSignIn: vi.fn(),
    })

    render(<LoginButton disabled />)
    const button = screen.getByRole('button', { name: /log in/i })
    expect(button).toBeDisabled()
  })
})
