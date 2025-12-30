import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ProfileForm } from './ProfileForm'
import type { User } from './types'

// Mock the generated useUpdateUserProfile hook with configurable behavior
const mockMutate = vi.fn()
let mockIsPending = false
vi.mock('@/api/generated/endpoints/profile/profile', () => ({
  useUpdateUserProfile: vi.fn(() => ({
    mutate: mockMutate,
    mutateAsync: vi.fn(),
    get isPending() {
      return mockIsPending
    },
    isError: false,
    isSuccess: false,
    error: null,
    data: null,
    reset: vi.fn(),
  })),
}))

// Mock sonner toast
const mockToast = vi.hoisted(() => ({
  success: vi.fn(),
  error: vi.fn(),
}))
vi.mock('sonner', () => ({
  toast: mockToast,
}))

describe('ProfileForm', () => {
  const mockUser: User = {
    id: 'user-123',
    clerk_user_id: 'auth0|123',
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
    mockIsPending = false
  })

  // Helper to make mockMutate call onSuccess with the given result
  function configureMutateSuccess(result: User) {
    mockMutate.mockImplementation((_data, options) => {
      options?.onSuccess?.(result)
    })
  }

  // Helper to make mockMutate call onError with the given error
  function configureMutateError(error: Error) {
    mockMutate.mockImplementation((_data, options) => {
      options?.onError?.(error)
    })
  }

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
      const user = userEvent.setup({ delay: null })
      render(<ProfileForm user={mockUser} />)

      const emailInput = screen.getByLabelText(/email/i) as HTMLInputElement
      const saveButton = screen.getByRole('button', { name: /save changes/i })

      // Clear email by selecting all and deleting (more reliable than clear() in vitest 4.x)
      await user.tripleClick(emailInput)
      await user.keyboard('{Backspace}')
      await user.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/email is required/i)).toBeInTheDocument()
      })
    })

    it('should show error for invalid email format', async () => {
      const user = userEvent.setup({ delay: null })
      render(<ProfileForm user={mockUser} />)

      const emailInput = screen.getByLabelText(/email/i)
      const saveButton = screen.getByRole('button', { name: /save changes/i })

      // Enter invalid email
      // Triple-click to select all, then type to replace (more reliable than clear() in vitest 4.x)
      await user.tripleClick(emailInput)
      await user.keyboard( 'invalid-email')
      await user.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/invalid email address/i)).toBeInTheDocument()
      })
    })

    it('should accept valid email format', async () => {
      configureMutateSuccess({
        ...mockUser,
        email: 'newemail@example.com',
      })

      const user = userEvent.setup({ delay: null })
      render(<ProfileForm user={mockUser} />)

      const emailInput = screen.getByLabelText(/email/i)
      const saveButton = screen.getByRole('button', { name: /save changes/i })

      // Triple-click to select all, then type to replace (more reliable than clear() in vitest 4.x)
      await user.tripleClick(emailInput)
      await user.keyboard('newemail@example.com')
      await user.click(saveButton)

      await waitFor(() => {
        expect(mockMutate).toHaveBeenCalled()
      })
    })
  })

  describe('Form submission', () => {
    it('should update email when changed', async () => {
      const updatedUser = { ...mockUser, email: 'updated@example.com' }
      configureMutateSuccess(updatedUser)

      const user = userEvent.setup({ delay: null })
      render(<ProfileForm user={mockUser} />)

      const emailInput = screen.getByLabelText(/email/i)
      const saveButton = screen.getByRole('button', { name: /save changes/i })

      // Triple-click to select all, then type to replace (more reliable than clear() in vitest 4.x)
      await user.tripleClick(emailInput)
      await user.keyboard('updated@example.com')
      await user.click(saveButton)

      await waitFor(() => {
        expect(mockMutate).toHaveBeenCalledWith(
          { data: { email: 'updated@example.com' } },
          expect.any(Object)
        )
      })
    })

    it('should update region when changed after confirmation', async () => {
      const updatedUser = { ...mockUser, primary_region: 'lhr' }
      configureMutateSuccess(updatedUser)

      const user = userEvent.setup({ delay: null })
      render(<ProfileForm user={mockUser} />)

      const regionSelect = screen.getByLabelText(/primary region/i)
      const saveButton = screen.getByRole('button', { name: /save changes/i })

      await user.selectOptions(regionSelect, 'lhr')
      await user.click(saveButton)

      // Confirmation dialog should appear
      await waitFor(() => {
        expect(screen.getByText(/change data region/i)).toBeInTheDocument()
      })

      // Click confirm button
      const confirmButton = screen.getByRole('button', { name: /confirm change/i })
      await user.click(confirmButton)

      await waitFor(() => {
        expect(mockMutate).toHaveBeenCalledWith(
          { data: { email: 'test@example.com', primary_region: 'lhr' } },
          expect.any(Object)
        )
      })
    })

    it('should not submit if no changes', async () => {
      const user = userEvent.setup({ delay: null })
      render(<ProfileForm user={mockUser} />)

      const saveButton = screen.getByRole('button', { name: /save changes/i })
      await user.click(saveButton)

      expect(mockMutate).not.toHaveBeenCalled()
    })

    it('should show success toast after successful update', async () => {
      const updatedUser = { ...mockUser, email: 'updated@example.com' }
      configureMutateSuccess(updatedUser)
      mockToast.success.mockClear()

      const user = userEvent.setup({ delay: null })
      render(<ProfileForm user={mockUser} />)

      const emailInput = screen.getByLabelText(/email/i)
      const saveButton = screen.getByRole('button', { name: /save changes/i })

      // Triple-click to select all, then type to replace (more reliable than clear() in vitest 4.x)
      await user.tripleClick(emailInput)
      await user.keyboard('updated@example.com')
      await user.click(saveButton)

      await waitFor(() => {
        expect(mockToast.success).toHaveBeenCalledWith('Profile updated successfully!')
      })
    })

    it('should show error toast on update failure', async () => {
      configureMutateError(new Error('Update failed'))
      mockToast.error.mockClear()

      const user = userEvent.setup({ delay: null })
      render(<ProfileForm user={mockUser} />)

      const emailInput = screen.getByLabelText(/email/i)
      const saveButton = screen.getByRole('button', { name: /save changes/i })

      // Triple-click to select all, then type to replace (more reliable than clear() in vitest 4.x)
      await user.tripleClick(emailInput)
      await user.keyboard('updated@example.com')
      await user.click(saveButton)

      await waitFor(() => {
        expect(mockToast.error).toHaveBeenCalledWith('Failed to update profile. Please try again.')
      })
    })

    it('should disable form while submitting', async () => {
      // Configure mutate to set isPending flag when called
      mockMutate.mockImplementation(() => {
        mockIsPending = true
      })

      const user = userEvent.setup({ delay: null })
      const { rerender } = render(<ProfileForm user={mockUser} />)

      const emailInput = screen.getByLabelText(/email/i)
      const regionSelect = screen.getByLabelText(/primary region/i)
      const saveButton = screen.getByRole('button', { name: /save changes/i })

      // Triple-click to select all, then type to replace (more reliable than clear() in vitest 4.x)
      await user.tripleClick(emailInput)
      await user.keyboard('updated@example.com')
      await user.click(saveButton)

      // Rerender to reflect the pending state
      rerender(<ProfileForm user={mockUser} />)

      await waitFor(() => {
        expect(emailInput).toBeDisabled()
        expect(regionSelect).toBeDisabled()
        expect(screen.getByRole('button', { name: /saving/i })).toBeDisabled()
      })
    })

    it('should call onProfileUpdate callback after successful update', async () => {
      const updatedUser = { ...mockUser, email: 'updated@example.com' }
      configureMutateSuccess(updatedUser)

      const onProfileUpdate = vi.fn()
      const user = userEvent.setup({ delay: null })
      render(<ProfileForm user={mockUser} onProfileUpdate={onProfileUpdate} />)

      const emailInput = screen.getByLabelText(/email/i)
      const saveButton = screen.getByRole('button', { name: /save changes/i })

      // Triple-click to select all, then type to replace (more reliable than clear() in vitest 4.x)
      await user.tripleClick(emailInput)
      await user.keyboard('updated@example.com')
      await user.click(saveButton)

      await waitFor(() => {
        expect(onProfileUpdate).toHaveBeenCalledWith(updatedUser)
      })
    })
  })

  describe('Form state management', () => {
    it('should update local state when editing email', async () => {
      const user = userEvent.setup({ delay: null })
      render(<ProfileForm user={mockUser} />)

      const emailInput = screen.getByLabelText(/email/i) as HTMLInputElement

      // Triple-click to select all, then type to replace
      await user.tripleClick(emailInput)
      await user.keyboard('new@example.com')

      expect(emailInput).toHaveValue('new@example.com')
    })

    it('should update local state when selecting region', async () => {
      const user = userEvent.setup({ delay: null })
      render(<ProfileForm user={mockUser} />)

      const regionSelect = screen.getByLabelText(/primary region/i)

      await user.selectOptions(regionSelect, 'lhr')

      expect(regionSelect).toHaveValue('lhr')
    })

    it('should reset to user data after successful update', async () => {
      const updatedUser = { ...mockUser, email: 'final@example.com' }
      configureMutateSuccess(updatedUser)

      const user = userEvent.setup({ delay: null })
      render(<ProfileForm user={mockUser} />)

      const emailInput = screen.getByLabelText(/email/i)
      const saveButton = screen.getByRole('button', { name: /save changes/i })

      // Triple-click to select all, then type to replace (more reliable than clear() in vitest 4.x)
      await user.tripleClick(emailInput)
      await user.keyboard( 'final@example.com')
      await user.click(saveButton)

      await waitFor(() => {
        expect(emailInput).toHaveValue('final@example.com')
      })
    })
  })
})
