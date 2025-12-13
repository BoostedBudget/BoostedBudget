import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { LoginButton } from './LoginButton'

// Mock Auth0
const mockUseAuth0 = vi.hoisted(() => vi.fn())

vi.mock('@auth0/auth0-react', () => ({
  useAuth0: mockUseAuth0,
}))

describe('LoginButton', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('should render login button', () => {
    mockUseAuth0.mockReturnValue({
      loginWithRedirect: vi.fn(),
    })

    render(<LoginButton />)
    expect(screen.getByRole('button', { name: /log in/i })).toBeInTheDocument()
  })

  it('should call loginWithRedirect when clicked', async () => {
    const mockLoginWithRedirect = vi.fn()
    mockUseAuth0.mockReturnValue({
      loginWithRedirect: mockLoginWithRedirect,
    })

    const user = userEvent.setup({ delay: null })
    render(<LoginButton />)

    const button = screen.getByRole('button', { name: /log in/i })
    await user.click(button)

    expect(mockLoginWithRedirect).toHaveBeenCalled()
  })

  it('should pass custom className if provided', () => {
    mockUseAuth0.mockReturnValue({
      loginWithRedirect: vi.fn(),
    })

    render(<LoginButton className="custom-class" />)
    const button = screen.getByRole('button', { name: /log in/i })
    expect(button).toHaveClass('custom-class')
  })

  it('should be disabled when disabled prop is true', () => {
    mockUseAuth0.mockReturnValue({
      loginWithRedirect: vi.fn(),
    })

    render(<LoginButton disabled />)
    const button = screen.getByRole('button', { name: /log in/i })
    expect(button).toBeDisabled()
  })
})
