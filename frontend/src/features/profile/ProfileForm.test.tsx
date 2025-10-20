import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ProfileForm } from './ProfileForm'
import * as profileApi from './api/profileApi'
import type { User } from './types'

// Mock the profile API
vi.mock('./api/profileApi')

describe('ProfileForm', () => {
  const mockUser: User = {
    id: 'user-123',
    auth0_user_id: 'auth0|123',
    email: 'test@example.com',
    email_verified: true,
    primary_region: 'iad',
    subscription_tier: 'free',
    subscription_status: 'active',
    is_active: true,
    is_deleted: false,
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-15T10:00:00Z',
    last_login_at: '2025-01-15T10:00:00Z',
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Form rendering', () => {
    it('should render profile form with user data', () => {
      render(<ProfileForm user={mockUser} />)

      expect(screen.getByText(/Update Profile/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/email/i)).toHaveValue('test@example.com')
      expect(screen.getByLabelText(/primary region/i)).toHaveValue('iad')
      expect(screen.getByRole('button', { name: /save changes/i })).toBeInTheDocument()
    })

    it('should render all valid region options', () => {
      render(<ProfileForm user={mockUser} />)

      const regionSelect = screen.getByLabelText(/primary region/i) as HTMLSelectElement
      const options = Array.from(regionSelect.options).map((o) => o.value)

      expect(options).toContain('iad')
      expect(options).toContain('lhr')
      expect(options).toContain('syd')
    })
  })

  describe('Email validation', () => {
    it('should show error for empty email', async () => {
      const user = userEvent.setup()
      render(<ProfileForm user={mockUser} />)

      const emailInput = screen.getByLabelText(/email/i)
      const saveButton = screen.getByRole('button', { name: /save changes/i })

      // Clear email
      await user.clear(emailInput)
      await user.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/email is required/i)).toBeInTheDocument()
      })
    })

    it('should show error for invalid email format', async () => {
      const user = userEvent.setup()
      render(<ProfileForm user={mockUser} />)

      const emailInput = screen.getByLabelText(/email/i)
      const saveButton = screen.getByRole('button', { name: /save changes/i })

      // Enter invalid email
      await user.clear(emailInput)
      await user.type(emailInput, 'invalid-email')
      await user.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/invalid email address/i)).toBeInTheDocument()
      })
    })

    it('should accept valid email format', async () => {
      vi.mocked(profileApi.updateProfile).mockResolvedValue({
        ...mockUser,
        email: 'newemail@example.com',
      })

      const user = userEvent.setup()
      render(<ProfileForm user={mockUser} />)

      const emailInput = screen.getByLabelText(/email/i)
      const saveButton = screen.getByRole('button', { name: /save changes/i })

      await user.clear(emailInput)
      await user.type(emailInput, 'newemail@example.com')
      await user.click(saveButton)

      await waitFor(() => {
        expect(profileApi.updateProfile).toHaveBeenCalled()
      })
    })
  })

  describe('Form submission', () => {
    it('should update email when changed', async () => {
      const updatedUser = { ...mockUser, email: 'updated@example.com' }
      vi.mocked(profileApi.updateProfile).mockResolvedValue(updatedUser)

      const user = userEvent.setup()
      render(<ProfileForm user={mockUser} />)

      const emailInput = screen.getByLabelText(/email/i)
      const saveButton = screen.getByRole('button', { name: /save changes/i })

      await user.clear(emailInput)
      await user.type(emailInput, 'updated@example.com')
      await user.click(saveButton)

      await waitFor(() => {
        expect(profileApi.updateProfile).toHaveBeenCalledWith({
          email: 'updated@example.com',
        })
      })
    })

    it('should update region when changed', async () => {
      const updatedUser = { ...mockUser, primary_region: 'lhr' }
      vi.mocked(profileApi.updateProfile).mockResolvedValue(updatedUser)

      const user = userEvent.setup()
      render(<ProfileForm user={mockUser} />)

      const regionSelect = screen.getByLabelText(/primary region/i)
      const saveButton = screen.getByRole('button', { name: /save changes/i })

      await user.selectOptions(regionSelect, 'lhr')
      await user.click(saveButton)

      await waitFor(() => {
        expect(profileApi.updateProfile).toHaveBeenCalledWith({
          email: 'test@example.com', // Email included when updating region
          primary_region: 'lhr',
        })
      })
    })

    it('should not submit if no changes', async () => {
      const user = userEvent.setup()
      render(<ProfileForm user={mockUser} />)

      const saveButton = screen.getByRole('button', { name: /save changes/i })
      await user.click(saveButton)

      expect(profileApi.updateProfile).not.toHaveBeenCalled()
    })

    it('should show success message after successful update', async () => {
      const updatedUser = { ...mockUser, email: 'updated@example.com' }
      vi.mocked(profileApi.updateProfile).mockResolvedValue(updatedUser)

      const user = userEvent.setup()
      render(<ProfileForm user={mockUser} />)

      const emailInput = screen.getByLabelText(/email/i)
      const saveButton = screen.getByRole('button', { name: /save changes/i })

      await user.clear(emailInput)
      await user.type(emailInput, 'updated@example.com')
      await user.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/profile updated successfully/i)).toBeInTheDocument()
      })
    })

    it('should show error message on update failure', async () => {
      vi.mocked(profileApi.updateProfile).mockRejectedValue(new Error('Update failed'))

      const user = userEvent.setup()
      render(<ProfileForm user={mockUser} />)

      const emailInput = screen.getByLabelText(/email/i)
      const saveButton = screen.getByRole('button', { name: /save changes/i })

      await user.clear(emailInput)
      await user.type(emailInput, 'updated@example.com')
      await user.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/failed to update profile/i)).toBeInTheDocument()
      })
    })

    it('should disable form while submitting', async () => {
      vi.mocked(profileApi.updateProfile).mockImplementation(
        () => new Promise(() => {}) // Never resolves
      )

      const user = userEvent.setup()
      render(<ProfileForm user={mockUser} />)

      const emailInput = screen.getByLabelText(/email/i)
      const regionSelect = screen.getByLabelText(/primary region/i)
      const saveButton = screen.getByRole('button', { name: /save changes/i })

      await user.clear(emailInput)
      await user.type(emailInput, 'updated@example.com')
      await user.click(saveButton)

      await waitFor(() => {
        expect(emailInput).toBeDisabled()
        expect(regionSelect).toBeDisabled()
        expect(saveButton).toBeDisabled()
      })
    })

    it('should call onProfileUpdate callback after successful update', async () => {
      const updatedUser = { ...mockUser, email: 'updated@example.com' }
      vi.mocked(profileApi.updateProfile).mockResolvedValue(updatedUser)

      const onProfileUpdate = vi.fn()
      const user = userEvent.setup()
      render(<ProfileForm user={mockUser} onProfileUpdate={onProfileUpdate} />)

      const emailInput = screen.getByLabelText(/email/i)
      const saveButton = screen.getByRole('button', { name: /save changes/i })

      await user.clear(emailInput)
      await user.type(emailInput, 'updated@example.com')
      await user.click(saveButton)

      await waitFor(() => {
        expect(onProfileUpdate).toHaveBeenCalledWith(updatedUser)
      })
    })
  })

  describe('Form state management', () => {
    it('should update local state when editing email', async () => {
      const user = userEvent.setup()
      render(<ProfileForm user={mockUser} />)

      const emailInput = screen.getByLabelText(/email/i)

      await user.clear(emailInput)
      await user.type(emailInput, 'new@example.com')

      expect(emailInput).toHaveValue('new@example.com')
    })

    it('should update local state when selecting region', async () => {
      const user = userEvent.setup()
      render(<ProfileForm user={mockUser} />)

      const regionSelect = screen.getByLabelText(/primary region/i)

      await user.selectOptions(regionSelect, 'lhr')

      expect(regionSelect).toHaveValue('lhr')
    })

    it('should reset to user data after successful update', async () => {
      const updatedUser = { ...mockUser, email: 'final@example.com' }
      vi.mocked(profileApi.updateProfile).mockResolvedValue(updatedUser)

      const user = userEvent.setup()
      render(<ProfileForm user={mockUser} />)

      const emailInput = screen.getByLabelText(/email/i)
      const saveButton = screen.getByRole('button', { name: /save changes/i })

      await user.clear(emailInput)
      await user.type(emailInput, 'final@example.com')
      await user.click(saveButton)

      await waitFor(() => {
        expect(emailInput).toHaveValue('final@example.com')
      })
    })
  })
})
