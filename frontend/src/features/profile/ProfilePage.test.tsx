import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ProfilePage } from './ProfilePage'
import * as profileApi from './api/profileApi'
import type { User } from './types'

// Mock the profile API
vi.mock('./api/profileApi')

// Mock Auth0
const mockUseAuth0 = vi.hoisted(() => vi.fn())
vi.mock('@auth0/auth0-react', () => ({
  useAuth0: mockUseAuth0,
}))

describe('ProfilePage', () => {
  const mockUser: User = {
    id: '123e4567-e89b-12d3-a456-426614174000',
    auth0_user_id: 'auth0|507f1f77bcf86cd799439011',
    email: 'test@example.com',
    email_verified: true,
    subscription_tier: 'premium',
    subscription_status: 'active',
    primary_region: 'us-east-1',
    is_active: true,
    is_deleted: false,
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
    last_login_at: '2025-01-01T00:00:00Z',
  }

  beforeEach(() => {
    vi.clearAllMocks()
    mockUseAuth0.mockReturnValue({
      isAuthenticated: true,
      user: { email: 'test@example.com' },
    })
  })

  describe('Loading and data display', () => {
    it('should show loading state initially', () => {
      vi.mocked(profileApi.fetchProfile).mockImplementation(
        () => new Promise(() => {}) // Never resolves
      )

      render(<ProfilePage />)

      expect(screen.getByText(/loading/i)).toBeInTheDocument()
    })

    it('should render user profile data after loading', async () => {
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)

      render(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByText('test@example.com')).toBeInTheDocument()
      })

      expect(screen.getByText(/USA - us-east-1/i)).toBeInTheDocument()
    })

    it('should display user email from profile', async () => {
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)

      render(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })
    })

    it('should show data region correctly', async () => {
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)

      render(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByText(/usa.*us-east-1/i)).toBeInTheDocument()
      })
    })

    it('should show current subscription tier', async () => {
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)

      render(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByText(/premium/i)).toBeInTheDocument()
      })
    })
  })

  describe('Error handling', () => {
    it('should show error message when API fails', async () => {
      vi.mocked(profileApi.fetchProfile).mockRejectedValue(new Error('API Error'))

      render(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByText(/failed to load profile/i)).toBeInTheDocument()
      })
    })

    it('should show retry button on error', async () => {
      vi.mocked(profileApi.fetchProfile).mockRejectedValue(new Error('API Error'))

      render(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument()
      })
    })

    it('should retry fetching profile when retry button clicked', async () => {
      const fetchProfileMock = vi.mocked(profileApi.fetchProfile)
      fetchProfileMock.mockRejectedValueOnce(new Error('API Error'))
      fetchProfileMock.mockResolvedValueOnce(mockUser)

      render(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument()
      })

      const user = userEvent.setup()
      const retryButton = screen.getByRole('button', { name: /retry/i })
      await user.click(retryButton)

      await waitFor(() => {
        expect(screen.getByText('test@example.com')).toBeInTheDocument()
      })

      expect(fetchProfileMock).toHaveBeenCalledTimes(2)
    })
  })

  describe('Edit form validation', () => {
    it('should validate email format', async () => {
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)

      render(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })

      const user = userEvent.setup()
      const emailInput = screen.getByLabelText(/email/i)

      await user.clear(emailInput)
      await user.type(emailInput, 'invalid-email')

      const saveButton = screen.getByRole('button', { name: /save/i })
      await user.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/invalid email/i)).toBeInTheDocument()
      })
    })

    it('should require valid region selection', async () => {
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)

      render(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })

      // Region should be a select dropdown
      const regionSelect = screen.getByLabelText(/region/i)
      expect(regionSelect).toBeInTheDocument()
      expect(regionSelect.tagName).toBe('SELECT')
    })

    it('should show validation error for empty email', async () => {
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)

      render(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })

      const user = userEvent.setup()
      const emailInput = screen.getByLabelText(/email/i)

      await user.clear(emailInput)

      const saveButton = screen.getByRole('button', { name: /save/i })
      await user.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/email is required/i)).toBeInTheDocument()
      })
    })
  })

  describe('Form submission', () => {
    it('should submit updated email via API', async () => {
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)
      vi.mocked(profileApi.updateProfile).mockResolvedValue({
        ...mockUser,
        email: 'newemail@example.com',
      })

      render(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })

      const user = userEvent.setup()
      const emailInput = screen.getByLabelText(/email/i)

      await user.clear(emailInput)
      await user.type(emailInput, 'newemail@example.com')

      const saveButton = screen.getByRole('button', { name: /save/i })
      await user.click(saveButton)

      await waitFor(() => {
        expect(profileApi.updateProfile).toHaveBeenCalledWith({
          email: 'newemail@example.com',
        })
      })
    })

    it('should submit updated region via API', async () => {
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)
      vi.mocked(profileApi.updateProfile).mockResolvedValue({
        ...mockUser,
        primary_region: 'eu-west-1',
      })

      render(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })

      const user = userEvent.setup()
      const regionSelect = screen.getByLabelText(/region/i)

      await user.selectOptions(regionSelect, 'eu-west-1')

      const saveButton = screen.getByRole('button', { name: /save/i })
      await user.click(saveButton)

      await waitFor(() => {
        expect(profileApi.updateProfile).toHaveBeenCalledWith({
          email: 'test@example.com',
          primary_region: 'eu-west-1',
        })
      })
    })

    it('should show success message after update', async () => {
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)
      vi.mocked(profileApi.updateProfile).mockResolvedValue({
        ...mockUser,
        email: 'newemail@example.com',
      })

      render(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })

      const user = userEvent.setup()
      const emailInput = screen.getByLabelText(/email/i)

      await user.clear(emailInput)
      await user.type(emailInput, 'newemail@example.com')

      const saveButton = screen.getByRole('button', { name: /save/i })
      await user.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/profile updated successfully/i)).toBeInTheDocument()
      })
    })

    it('should show error message when update fails', async () => {
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)
      vi.mocked(profileApi.updateProfile).mockRejectedValue(new Error('Update failed'))

      render(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })

      const user = userEvent.setup()
      const emailInput = screen.getByLabelText(/email/i)

      await user.clear(emailInput)
      await user.type(emailInput, 'newemail@example.com')

      const saveButton = screen.getByRole('button', { name: /save/i })
      await user.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/failed to update profile/i)).toBeInTheDocument()
      })
    })

    it('should disable save button while submitting', async () => {
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)
      vi.mocked(profileApi.updateProfile).mockImplementation(
        () => new Promise(() => {}) // Never resolves
      )

      render(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })

      const user = userEvent.setup()
      const emailInput = screen.getByLabelText(/email/i)

      await user.clear(emailInput)
      await user.type(emailInput, 'newemail@example.com')

      const saveButton = screen.getByRole('button', { name: /save/i })
      await user.click(saveButton)

      await waitFor(() => {
        expect(saveButton).toBeDisabled()
      })
    })
  })

  describe('Responsive design', () => {
    it('should render mobile layout on small screens', async () => {
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)

      const { container } = render(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByText('test@example.com')).toBeInTheDocument()
      })

      const profilePage = container.querySelector('.profile-page')
      expect(profilePage).toHaveClass('responsive')
    })
  })
})
