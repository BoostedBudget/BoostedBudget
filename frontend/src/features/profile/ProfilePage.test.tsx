/**
 * ProfilePage Tests
 *
 * Tests the ProfilePage component including loading states, error handling,
 * form validation, and submission.
 *
 * Issue #575: Migrated to bun:test with centralized API mocks
 */

import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ProfilePage } from './ProfilePage'
import type { User } from './types'
import { mockSubscriptionTiersResponse } from '@/test/fixtures/subscriptionFixtures'

// Import centralized mocks for configuration
import {
  mockUseGetUserProfile,
  mockRefetchUserProfile,
  mockUpdateUserProfileMutate,
  setMockUpdateUserProfileIsPending,
} from '@/test/mocks/profile-api'
import {
  mockUseGetSubscriptionTiers,
  mockRefetchSubscriptionTiers,
} from '@/test/mocks/subscription-api'
import {
  mockUseRequestDeletion,
  mockCancelDeletion,
} from '@/test/mocks/user-api'
import {
  mockUseGetPrivacySettings,
  mockUseUpdatePrivacySettings,
} from '@/test/mocks/privacy-api'
import { mockUseAuth } from '@/test/mocks/clerk.tsx'
import { mockToastSuccess, mockToastError } from '@/test/mocks/sonner'

// Create a fresh QueryClient for each test
function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  })
}

// Test wrapper with React Router and QueryClient
function renderWithProviders(ui: React.ReactElement, { initialUrl = '/profile' } = {}) {
  const queryClient = createTestQueryClient()
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initialUrl]}>
        <Routes>
          <Route path="/profile" element={ui} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  )
}

describe('ProfilePage', () => {
  const mockUser: User = {
    id: '123e4567-e89b-12d3-a456-426614174000',
    clerk_user_id: 'user_507f1f77bcf86cd799439011',
    email: 'test@example.com',
    email_verified: true,
    subscription_tier: 'premium',
    subscription_status: 'active',
    primary_region: 'iad',
    is_active: true,
    is_deleted: false,
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
    last_login_at: '2025-01-01T00:00:00Z',
  }

  beforeEach(() => {
    // Clear all mocks
    mockUseGetUserProfile.mockClear()
    mockRefetchUserProfile.mockClear()
    mockUpdateUserProfileMutate.mockClear()
    mockUseGetSubscriptionTiers.mockClear()
    mockRefetchSubscriptionTiers.mockClear()
    mockUseRequestDeletion.mockClear()
    mockCancelDeletion.mockClear()
    mockUseGetPrivacySettings.mockClear()
    mockUseUpdatePrivacySettings.mockClear()
    mockUseAuth.mockClear()
    mockToastSuccess.mockClear()
    mockToastError.mockClear()

    // Reset isPending state
    setMockUpdateUserProfileIsPending(false)

    // Configure Clerk mock
    mockUseAuth.mockReturnValue({
      isSignedIn: true,
      isLoaded: true,
      userId: 'user_123',
      sessionId: 'sess_123',
      getToken: mock(() => Promise.resolve('mock-token')),
    })

    // Configure subscription tiers mock
    mockUseGetSubscriptionTiers.mockReturnValue({
      data: mockSubscriptionTiersResponse,
      isLoading: false,
      error: null,
      refetch: mockRefetchSubscriptionTiers,
    })

    // Configure profile mock - default to loaded state with user
    mockUseGetUserProfile.mockReturnValue({
      data: mockUser,
      isLoading: false,
      error: null,
      refetch: mockRefetchUserProfile,
    })

    // Configure deletion mock
    mockUseRequestDeletion.mockReturnValue({
      mutateAsync: mock(() => Promise.resolve({
        message: 'Account deletion scheduled',
        scheduled_at: '2025-11-26T00:00:00Z',
        cancellation_token: 'test-token',
      })),
      isPending: false,
      isError: false,
      error: null,
    })
  })

  // Helper to configure mockUpdateUserProfileMutate to call onSuccess
  function configureMutateSuccess(result: User) {
    mockUpdateUserProfileMutate.mockImplementation((_data: unknown, options?: { onSuccess?: (data: User) => void }) => {
      options?.onSuccess?.(result)
    })
  }

  // Helper to configure mockUpdateUserProfileMutate to call onError
  function configureMutateError(error: Error) {
    mockUpdateUserProfileMutate.mockImplementation((_data: unknown, options?: { onError?: (err: Error) => void }) => {
      options?.onError?.(error)
    })
  }

  describe('Loading and data display', () => {
    it('should show loading state initially', () => {
      mockUseGetUserProfile.mockReturnValue({
        data: null,
        isLoading: true,
        error: null,
        refetch: mockRefetchUserProfile,
      })

      renderWithProviders(<ProfilePage />)

      expect(screen.getByText(/loading/i)).toBeInTheDocument()
    })

    it('should render user profile data after loading', async () => {
      renderWithProviders(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByText('test@example.com')).toBeInTheDocument()
      })

      const regionElements = screen.getAllByText(/🇺🇸\s*USA \(Virginia\)/i)
      expect(regionElements.length).toBeGreaterThan(0)
    })

    it('should display user email from profile', async () => {
      renderWithProviders(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })
    })

    it('should show data region correctly', async () => {
      renderWithProviders(<ProfilePage />)

      await waitFor(() => {
        const regionElements = screen.getAllByText(/🇺🇸\s*USA \(Virginia\)/i)
        expect(regionElements.length).toBeGreaterThan(0)
      })
    })

    it('should show current subscription tier', async () => {
      renderWithProviders(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByText(/premium/i)).toBeInTheDocument()
      })
    })
  })

  describe('Error handling', () => {
    it('should show error message when API fails', async () => {
      mockUseGetUserProfile.mockReturnValue({
        data: null,
        isLoading: false,
        error: new Error('API Error'),
        refetch: mockRefetchUserProfile,
      })

      renderWithProviders(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByText('API Error')).toBeInTheDocument()
      })
    })

    it('should show retry button on error', async () => {
      mockUseGetUserProfile.mockReturnValue({
        data: null,
        isLoading: false,
        error: new Error('API Error'),
        refetch: mockRefetchUserProfile,
      })

      renderWithProviders(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument()
      })
    })

    it('should retry fetching profile when retry button clicked', async () => {
      const mockRefetch = mock(() => {})
      mockUseGetUserProfile.mockReturnValue({
        data: null,
        isLoading: false,
        error: new Error('API Error'),
        refetch: mockRefetch,
      })

      renderWithProviders(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument()
      })

      const user = userEvent.setup({ delay: null })
      const retryButton = screen.getByRole('button', { name: /retry/i })
      await user.click(retryButton)

      expect(mockRefetch).toHaveBeenCalled()
    })
  })

  describe('Edit form validation', () => {
    it('should validate email format', async () => {
      renderWithProviders(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })

      const user = userEvent.setup({ delay: null })
      const emailInput = screen.getByLabelText(/email/i) as HTMLInputElement

      await user.tripleClick(emailInput)
      await user.keyboard('invalid-email')

      const saveButton = screen.getByRole('button', { name: /save/i })
      await user.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/invalid email/i)).toBeInTheDocument()
      })
    })

    it('should require valid region selection', async () => {
      renderWithProviders(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })

      const regionSelect = screen.getByLabelText(/region/i)
      expect(regionSelect).toBeInTheDocument()
      expect(regionSelect.tagName).toBe('SELECT')
    })

    it('should show validation error for empty email', async () => {
      renderWithProviders(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })

      const user = userEvent.setup({ delay: null })
      const emailInput = screen.getByLabelText(/email/i)

      await user.tripleClick(emailInput)
      await user.keyboard('{Backspace}')

      const saveButton = screen.getByRole('button', { name: /save/i })
      await user.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/email is required/i)).toBeInTheDocument()
      })
    })
  })

  describe('Form submission', () => {
    it('should submit updated email via API', async () => {
      configureMutateSuccess({
        ...mockUser,
        email: 'newemail@example.com',
      })

      renderWithProviders(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })

      const user = userEvent.setup({ delay: null })
      const emailInput = screen.getByLabelText(/email/i) as HTMLInputElement

      await user.tripleClick(emailInput)
      await user.keyboard('newemail@example.com')

      const saveButton = screen.getByRole('button', { name: /save/i })
      await user.click(saveButton)

      await waitFor(() => {
        expect(mockUpdateUserProfileMutate).toHaveBeenCalledWith(
          { data: { email: 'newemail@example.com' } },
          expect.any(Object)
        )
      })
    })

    it('should submit updated region via API after confirmation', async () => {
      configureMutateSuccess({
        ...mockUser,
        primary_region: 'lhr',
      })

      renderWithProviders(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })

      const user = userEvent.setup({ delay: null })
      const regionSelect = screen.getByLabelText(/region/i)

      await user.selectOptions(regionSelect, 'lhr')

      await waitFor(() => {
        expect((regionSelect as HTMLSelectElement).value).toBe('lhr')
      })

      const saveButton = screen.getByRole('button', { name: /save/i })
      await user.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/change data region/i)).toBeInTheDocument()
      })

      const confirmButton = screen.getByRole('button', { name: /confirm change/i })
      await user.click(confirmButton)

      await waitFor(() => {
        expect(mockUpdateUserProfileMutate).toHaveBeenCalledWith(
          { data: { email: 'test@example.com', primary_region: 'lhr' } },
          expect.any(Object)
        )
      })
    })

    it('should show success toast after update', async () => {
      configureMutateSuccess({
        ...mockUser,
        email: 'newemail@example.com',
      })

      renderWithProviders(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })

      const user = userEvent.setup({ delay: null })
      const emailInput = screen.getByLabelText(/email/i)

      await user.tripleClick(emailInput)
      await user.keyboard('newemail@example.com')

      const saveButton = screen.getByRole('button', { name: /save/i })
      await user.click(saveButton)

      await waitFor(() => {
        expect(mockToastSuccess).toHaveBeenCalledWith('Profile updated successfully!')
      })
    })

    it('should show error toast when update fails', async () => {
      configureMutateError(new Error('Update failed'))

      renderWithProviders(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })

      const user = userEvent.setup({ delay: null })
      const emailInput = screen.getByLabelText(/email/i) as HTMLInputElement

      await user.tripleClick(emailInput)
      await user.keyboard('newemail@example.com')

      const saveButton = screen.getByRole('button', { name: /save/i })
      await user.click(saveButton)

      await waitFor(() => {
        expect(mockToastError).toHaveBeenCalledWith('Failed to update profile. Please try again.')
      })
    })

    it('should disable save button while submitting', async () => {
      mockUpdateUserProfileMutate.mockImplementation(() => {
        setMockUpdateUserProfileIsPending(true)
      })

      const { rerender } = renderWithProviders(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })

      const user = userEvent.setup({ delay: null })
      const emailInput = screen.getByLabelText(/email/i) as HTMLInputElement

      await user.tripleClick(emailInput)
      await user.keyboard('newemail@example.com')

      const saveButton = screen.getByRole('button', { name: /save/i })
      await user.click(saveButton)

      // Rerender to reflect the pending state
      const queryClient = createTestQueryClient()
      rerender(
        <QueryClientProvider client={queryClient}>
          <MemoryRouter initialEntries={['/profile']}>
            <Routes>
              <Route path="/profile" element={<ProfilePage />} />
            </Routes>
          </MemoryRouter>
        </QueryClientProvider>
      )

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /saving/i })).toBeDisabled()
      })
    }, 15000)
  })

  describe('Responsive design', () => {
    it('should render mobile layout on small screens', async () => {
      const { container } = renderWithProviders(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByText('test@example.com')).toBeInTheDocument()
      })

      const profilePage = container.querySelector('.profile-page')
      expect(profilePage).toHaveClass('responsive')
    })
  })

  describe('Tab state persistence', () => {
    it('should default to Profile tab when no URL param is present', async () => {
      renderWithProviders(<ProfilePage />, { initialUrl: '/profile' })

      await waitFor(() => {
        expect(screen.getByText('test@example.com')).toBeInTheDocument()
      })

      const profileTabButton = screen.getByRole('button', { name: /^Profile$/i })
      expect(profileTabButton).toHaveClass('active')

      const privacyTabButton = screen.getByRole('button', { name: 'Privacy Settings' })
      expect(privacyTabButton).not.toHaveClass('active')

      expect(screen.getByText('Account Information')).toBeInTheDocument()
    })

    it('should activate Privacy tab when URL has ?tab=privacy', async () => {
      renderWithProviders(<ProfilePage />, { initialUrl: '/profile?tab=privacy' })

      await waitFor(() => {
        expect(screen.getByText('My Profile')).toBeInTheDocument()
      })

      const privacyTabButton = screen.getByRole('button', { name: 'Privacy Settings' })
      expect(privacyTabButton).toHaveClass('active')

      const profileTabButton = screen.getByRole('button', { name: /^Profile$/i })
      expect(profileTabButton).not.toHaveClass('active')

      expect(screen.getByRole('heading', { name: 'Privacy Settings' })).toBeInTheDocument()
    })

    it('should fallback to Profile tab for invalid URL tab param', async () => {
      renderWithProviders(<ProfilePage />, { initialUrl: '/profile?tab=invalid' })

      await waitFor(() => {
        expect(screen.getByText('test@example.com')).toBeInTheDocument()
      })

      const profileTabButton = screen.getByRole('button', { name: /^Profile$/i })
      expect(profileTabButton).toHaveClass('active')

      expect(screen.getByText('Account Information')).toBeInTheDocument()
    })

    it('should update URL when switching tabs', async () => {
      renderWithProviders(<ProfilePage />, { initialUrl: '/profile' })

      await waitFor(() => {
        expect(screen.getByText('test@example.com')).toBeInTheDocument()
      })

      const user = userEvent.setup({ delay: null })
      const privacyTabButton = screen.getByRole('button', { name: 'Privacy Settings' })
      await user.click(privacyTabButton)

      await waitFor(() => {
        expect(privacyTabButton).toHaveClass('active')
      })

      expect(screen.getByRole('heading', { name: 'Privacy Settings' })).toBeInTheDocument()
    })
  })
})
