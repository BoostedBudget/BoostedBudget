/**
 * ProfileForm Component Tests
 * Issue #575: Migrated to bun:test with centralized profile-api mocks
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { mock } from 'bun:test'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ProfileForm } from './ProfileForm'
import type { User } from './types'
import {
  mockUpdateUserProfileMutate,
  setMockUpdateUserProfileIsPending,
} from '@/test/mocks/profile-api'
import { mockToastSuccess, mockToastError } from '@/test/mocks/sonner'

describe('ProfileForm', () => {
  const mockUser: User = {
    id: 'user-123',
    clerk_user_id: 'user_123',
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
    mockUpdateUserProfileMutate.mockClear()
    mockToastSuccess.mockClear()
    mockToastError.mockClear()
    setMockUpdateUserProfileIsPending(false)
  })

  function configureMutateSuccess(result: User) {
    mockUpdateUserProfileMutate.mockImplementation((_data: unknown, options?: { onSuccess?: (r: User) => void }) => {
      options?.onSuccess?.(result)
    })
  }

  function configureMutateError(error: Error) {
    mockUpdateUserProfileMutate.mockImplementation((_data: unknown, options?: { onError?: (e: Error) => void }) => {
      options?.onError?.(error)
    })
  }

  async function updateEmailField(user: ReturnType<typeof userEvent.setup>, newEmail: string) {
    const emailInput = screen.getByLabelText(/email/i)
    await user.tripleClick(emailInput)
    await user.keyboard(newEmail)
  }

  describe('Form rendering', () => {
    it('should render profile form with user data', () => {
      render(<ProfileForm user={mockUser} />)

      expect(screen.getByText(/Update Profile/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/email/i)).toHaveValue('test@example.com')
      expect(screen.getByLabelText(/primary region/i)).toHaveValue('iad')
      expect(screen.getByRole('button', { name: /save changes/i })).toBeInTheDocument()
    })

    it('should render all valid region options from API', () => {
      render(<ProfileForm user={mockUser} />)

      const regionSelect = screen.getByLabelText(/primary region/i) as HTMLSelectElement
      const options = Array.from(regionSelect.options).map((o) => o.value)

      expect(options).toContain('iad')
      expect(options).toContain('ewr')
    })
  })

  describe('Email validation', () => {
    it('should validate empty email', async () => {
      const user = userEvent.setup({ delay: null })
      render(<ProfileForm user={mockUser} />)

      const emailInput = screen.getByLabelText(/email/i)
      const saveButton = screen.getByRole('button', { name: /save changes/i })

      await user.tripleClick(emailInput)
      await user.keyboard('{Backspace}')
      await user.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/email is required/i)).toBeInTheDocument()
      })
    })

    it('should validate invalid email format', async () => {
      const user = userEvent.setup({ delay: null })
      render(<ProfileForm user={mockUser} />)

      const emailInput = screen.getByLabelText(/email/i)
      const saveButton = screen.getByRole('button', { name: /save changes/i })

      await user.tripleClick(emailInput)
      await user.keyboard('invalid-email')
      await user.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/invalid email address/i)).toBeInTheDocument()
      })
    })

    it('should accept valid email and submit', async () => {
      configureMutateSuccess({ ...mockUser, email: 'newemail@example.com' })

      const user = userEvent.setup({ delay: null })
      render(<ProfileForm user={mockUser} />)

      await updateEmailField(user, 'newemail@example.com')
      await user.click(screen.getByRole('button', { name: /save changes/i }))

      await waitFor(() => {
        expect(mockUpdateUserProfileMutate).toHaveBeenCalled()
      })
    })
  })

  describe('Form submission', () => {
    it('should submit email changes and show success toast', async () => {
      configureMutateSuccess({ ...mockUser, email: 'updated@example.com' })

      const user = userEvent.setup({ delay: null })
      render(<ProfileForm user={mockUser} />)

      await updateEmailField(user, 'updated@example.com')
      await user.click(screen.getByRole('button', { name: /save changes/i }))

      await waitFor(() => {
        expect(mockUpdateUserProfileMutate).toHaveBeenCalledWith(
          { data: { email: 'updated@example.com' } },
          expect.any(Object)
        )
        expect(mockToastSuccess).toHaveBeenCalledWith('Profile updated successfully!')
      })
    })

    it('should update region when changed after confirmation', async () => {
      const updatedUser = { ...mockUser, primary_region: 'ewr' }
      configureMutateSuccess(updatedUser)

      const user = userEvent.setup({ delay: null })
      render(<ProfileForm user={mockUser} />)

      const regionSelect = screen.getByLabelText(/primary region/i)
      const saveButton = screen.getByRole('button', { name: /save changes/i })

      await user.selectOptions(regionSelect, 'ewr')
      await user.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/change data region/i)).toBeInTheDocument()
      })

      const confirmButton = screen.getByRole('button', { name: /confirm change/i })
      await user.click(confirmButton)

      await waitFor(() => {
        expect(mockUpdateUserProfileMutate).toHaveBeenCalledWith(
          { data: { email: 'test@example.com', primary_region: 'ewr' } },
          expect.any(Object)
        )
      })
    })

    it('should not submit if no changes', async () => {
      const user = userEvent.setup({ delay: null })
      render(<ProfileForm user={mockUser} />)

      const saveButton = screen.getByRole('button', { name: /save changes/i })
      await user.click(saveButton)

      expect(mockUpdateUserProfileMutate).not.toHaveBeenCalled()
    })

    it('should show error toast on update failure', async () => {
      configureMutateError(new Error('Update failed'))

      const user = userEvent.setup({ delay: null })
      render(<ProfileForm user={mockUser} />)

      await updateEmailField(user, 'updated@example.com')
      await user.click(screen.getByRole('button', { name: /save changes/i }))

      await waitFor(() => {
        expect(mockToastError).toHaveBeenCalledWith('Failed to update profile. Please try again.')
      })
    })

    it('should disable form while submitting', async () => {
      mockUpdateUserProfileMutate.mockImplementation(() => {
        setMockUpdateUserProfileIsPending(true)
      })

      const user = userEvent.setup({ delay: null })
      const { rerender } = render(<ProfileForm user={mockUser} />)

      await updateEmailField(user, 'updated@example.com')
      await user.click(screen.getByRole('button', { name: /save changes/i }))

      rerender(<ProfileForm user={mockUser} />)

      await waitFor(() => {
        expect(screen.getByLabelText(/email/i)).toBeDisabled()
        expect(screen.getByLabelText(/primary region/i)).toBeDisabled()
        expect(screen.getByRole('button', { name: /saving/i })).toBeDisabled()
      })
    })

    it('should call onProfileUpdate callback after successful update', async () => {
      const updatedUser = { ...mockUser, email: 'updated@example.com' }
      configureMutateSuccess(updatedUser)
      const onProfileUpdate = mock(() => {})

      const user = userEvent.setup({ delay: null })
      render(<ProfileForm user={mockUser} onProfileUpdate={onProfileUpdate} />)

      await updateEmailField(user, 'updated@example.com')
      await user.click(screen.getByRole('button', { name: /save changes/i }))

      await waitFor(() => {
        expect(onProfileUpdate).toHaveBeenCalledWith(updatedUser)
      })
    })
  })

  describe('Form state management', () => {
    it('should track form field changes', async () => {
      const user = userEvent.setup({ delay: null })
      render(<ProfileForm user={mockUser} />)

      const emailInput = screen.getByLabelText(/email/i)
      const regionSelect = screen.getByLabelText(/primary region/i)

      await updateEmailField(user, 'new@example.com')
      expect(emailInput).toHaveValue('new@example.com')

      await user.selectOptions(regionSelect, 'ewr')
      expect(regionSelect).toHaveValue('ewr')
    })

    it('should reset form to updated user data after successful submission', async () => {
      const updatedUser = { ...mockUser, email: 'final@example.com' }
      configureMutateSuccess(updatedUser)

      const user = userEvent.setup({ delay: null })
      render(<ProfileForm user={mockUser} />)

      const emailInput = screen.getByLabelText(/email/i)
      await updateEmailField(user, 'final@example.com')
      await user.click(screen.getByRole('button', { name: /save changes/i }))

      await waitFor(() => {
        expect(emailInput).toHaveValue('final@example.com')
      })
    })
  })
})
