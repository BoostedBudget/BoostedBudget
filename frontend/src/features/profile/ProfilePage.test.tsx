import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { ProfilePage } from './ProfilePage'
import * as useProfileHook from './hooks/useProfile'
import type { User } from './types'
import * as useSubscriptionTiersHook from '../subscription/hooks/useSubscriptionTiers'
import { mockSubscriptionTiersResponse } from '@/test/fixtures/subscriptionFixtures'
// Test utilities available: import { fillInputByLabel } from '@/test/utils/formTestHelpers'

// Mock the subscription tiers hook
vi.mock('../subscription/hooks/useSubscriptionTiers')

// Mock the deletion hooks to avoid React Query provider issues
vi.mock('../deletion/hooks/useDeletion', () => ({
  useDeletion: vi.fn().mockReturnValue({
    requestDeletion: vi.fn(),
    cancelDeletion: vi.fn(),
    isRequesting: false,
    isCancelling: false,
    requestError: null,
    cancelError: null,
  }),
}))

// Mock the profile hook to avoid React Query provider issues
vi.mock('./hooks/useProfile', () => ({
  useProfile: vi.fn(),
}))

// Mock the generated useUpdateUserProfile hook used by ProfileForm
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

// Mock the generated privacy hooks used by PrivacySettings component
vi.mock('@/api/generated/endpoints/privacy/privacy', () => ({
  useGetPrivacySettings: vi.fn(() => ({
    data: {
      analytics_enabled: true,
      data_sharing_enabled: false,
      ad_personalization_enabled: true,
    },
    isLoading: false,
    error: null,
    refetch: vi.fn(),
  })),
  useUpdatePrivacySettings: vi.fn(() => ({
    mutate: vi.fn(),
    mutateAsync: vi.fn(),
    isPending: false,
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

// Mock Auth0
const mockUseAuth0 = vi.hoisted(() => vi.fn())
vi.mock('@auth0/auth0-react', () => ({
  useAuth0: mockUseAuth0,
}))

/**
 * Test wrapper component that provides React Router context
 * Required for components using useSearchParams
 */
function renderWithRouter(ui: React.ReactElement, { initialUrl = '/profile' } = {}) {
  return render(
    <MemoryRouter initialEntries={[initialUrl]}>
      <Routes>
        <Route path="/profile" element={ui} />
      </Routes>
    </MemoryRouter>
  )
}

describe('ProfilePage', () => {
  const mockUser: User = {
    id: '123e4567-e89b-12d3-a456-426614174000',
    auth0_user_id: 'auth0|507f1f77bcf86cd799439011',
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
    vi.clearAllMocks()
    mockIsPending = false
    mockUseAuth0.mockReturnValue({
      isAuthenticated: true,
      user: { email: 'test@example.com' },
    })

    // Mock subscription tiers hook to return tier data
    vi.mocked(useSubscriptionTiersHook.useSubscriptionTiers).mockReturnValue({
      tiers: mockSubscriptionTiersResponse,
      loading: false,
      error: null,
      refetch: vi.fn(),
    })

    // Mock useProfile hook to return user data
    vi.mocked(useProfileHook.useProfile).mockReturnValue({
      user: mockUser,
      loading: false,
      error: null,
      refetch: vi.fn(),
    })
  })

  // Helper to make mockMutate call onSuccess with the given result
  function configureMutateSuccess(result: User) {
    mockMutate.mockImplementation((_data: unknown, options?: { onSuccess?: (data: User) => void }) => {
      options?.onSuccess?.(result)
    })
  }

  // Helper to make mockMutate call onError with the given error
  function configureMutateError(error: Error) {
    mockMutate.mockImplementation((_data: unknown, options?: { onError?: (err: Error) => void }) => {
      options?.onError?.(error)
    })
  }

  describe('Loading and data display', () => {
    it('should show loading state initially', () => {
      vi.mocked(useProfileHook.useProfile).mockReturnValue({
        user: null,
        loading: true,
        error: null,
        refetch: vi.fn(),
      })

      renderWithRouter(<ProfilePage />)

      expect(screen.getByText(/loading/i)).toBeInTheDocument()
    })

    it('should render user profile data after loading', async () => {
      // Uses default mock from beforeEach

      renderWithRouter(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByText('test@example.com')).toBeInTheDocument()
      })

      // Check for new geographic display format with flag emoji
      // Multiple elements may match (account info + dropdown options)
      const regionElements = screen.getAllByText(/🇺🇸\s*USA \(Virginia\)/i)
      expect(regionElements.length).toBeGreaterThan(0)
    })

    it('should display user email from profile', async () => {
      // Uses default mock from beforeEach

      renderWithRouter(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })
    })

    it('should show data region correctly', async () => {
      // Uses default mock from beforeEach

      renderWithRouter(<ProfilePage />)

      await waitFor(() => {
        // Check for new geographic display format with flag (iad maps to "🇺🇸 USA (Virginia)")
        // Multiple elements may match (account info + dropdown options)
        const regionElements = screen.getAllByText(/🇺🇸\s*USA \(Virginia\)/i)
        expect(regionElements.length).toBeGreaterThan(0)
      })
    })

    it('should show current subscription tier', async () => {
      // Uses default mock from beforeEach

      renderWithRouter(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByText(/premium/i)).toBeInTheDocument()
      })
    })
  })

  describe('Error handling', () => {
    it('should show error message when API fails', async () => {
      vi.mocked(useProfileHook.useProfile).mockReturnValue({
        user: null,
        loading: false,
        error: new Error('API Error'),
        refetch: vi.fn(),
      })

      renderWithRouter(<ProfilePage />)

      await waitFor(() => {
        // The component shows error.message if available
        expect(screen.getByText('API Error')).toBeInTheDocument()
      })
    })

    it('should show retry button on error', async () => {
      vi.mocked(useProfileHook.useProfile).mockReturnValue({
        user: null,
        loading: false,
        error: new Error('API Error'),
        refetch: vi.fn(),
      })

      renderWithRouter(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument()
      })
    })

    it('should retry fetching profile when retry button clicked', async () => {
      const mockRefetch = vi.fn()

      // Render shows error state
      vi.mocked(useProfileHook.useProfile).mockReturnValue({
        user: null,
        loading: false,
        error: new Error('API Error'),
        refetch: mockRefetch,
      })

      renderWithRouter(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument()
      })

      const user = userEvent.setup({ delay: null })
      const retryButton = screen.getByRole('button', { name: /retry/i })
      await user.click(retryButton)

      // Verify that refetch was called
      expect(mockRefetch).toHaveBeenCalled()
    })
  })

  describe('Edit form validation', () => {
    it('should validate email format', async () => {
      // Uses default mock from beforeEach

      renderWithRouter(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })

      const user = userEvent.setup({ delay: null })
      const emailInput = screen.getByLabelText(/email/i) as HTMLInputElement

      // Triple-click to select all, then type to replace (more reliable than clear() in vitest 4.x)
      await user.tripleClick(emailInput)
      await user.keyboard('invalid-email')

      const saveButton = screen.getByRole('button', { name: /save/i })
      await user.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/invalid email/i)).toBeInTheDocument()
      })
    })

    it('should require valid region selection', async () => {
      // Uses default mock from beforeEach

      renderWithRouter(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })

      // Region should be a select dropdown
      const regionSelect = screen.getByLabelText(/region/i)
      expect(regionSelect).toBeInTheDocument()
      expect(regionSelect.tagName).toBe('SELECT')
    })

    it('should show validation error for empty email', async () => {
      // Uses default mock from beforeEach

      renderWithRouter(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })

      const user = userEvent.setup({ delay: null })
      const emailInput = screen.getByLabelText(/email/i)

      // Use tripleClick + Backspace to reliably clear controlled input
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
      // Uses default mock from beforeEach
      configureMutateSuccess({
        ...mockUser,
        email: 'newemail@example.com',
      })

      renderWithRouter(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })

      const user = userEvent.setup({ delay: null })
      const emailInput = screen.getByLabelText(/email/i) as HTMLInputElement

      // Triple-click to select all, then type to replace (more reliable than clear() in vitest 4.x)
      await user.tripleClick(emailInput)
      await user.keyboard('newemail@example.com')

      const saveButton = screen.getByRole('button', { name: /save/i })
      await user.click(saveButton)

      await waitFor(() => {
        expect(mockMutate).toHaveBeenCalledWith(
          { data: { email: 'newemail@example.com' } },
          expect.any(Object)
        )
      })
    })

    it('should submit updated region via API after confirmation', async () => {
      // Uses default mock from beforeEach
      configureMutateSuccess({
        ...mockUser,
        primary_region: 'lhr',
      })

      renderWithRouter(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })

      const user = userEvent.setup({ delay: null })
      const regionSelect = screen.getByLabelText(/region/i)

      await user.selectOptions(regionSelect, 'lhr')

      // Wait for form state to update after selection
      await waitFor(() => {
        expect((regionSelect as HTMLSelectElement).value).toBe('lhr')
      })

      const saveButton = screen.getByRole('button', { name: /save/i })
      await user.click(saveButton)

      // Confirmation dialog should appear for region change
      await waitFor(() => {
        expect(screen.getByText(/change data region/i)).toBeInTheDocument()
      })

      // Click confirm button in dialog
      const confirmButton = screen.getByRole('button', { name: /confirm change/i })
      await user.click(confirmButton)

      await waitFor(() => {
        expect(mockMutate).toHaveBeenCalledWith(
          { data: { email: 'test@example.com', primary_region: 'lhr' } },
          expect.any(Object)
        )
      })
    })

    it('should show success toast after update', async () => {
      // Uses default mock from beforeEach
      configureMutateSuccess({
        ...mockUser,
        email: 'newemail@example.com',
      })
      mockToast.success.mockClear()

      renderWithRouter(<ProfilePage />)

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
        expect(mockToast.success).toHaveBeenCalledWith('Profile updated successfully!')
      })
    })

    it('should show error toast when update fails', async () => {
      // Uses default mock from beforeEach
      configureMutateError(new Error('Update failed'))
      mockToast.error.mockClear()

      renderWithRouter(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })

      const user = userEvent.setup({ delay: null })
      const emailInput = screen.getByLabelText(/email/i) as HTMLInputElement

      // Triple-click to select all, then type to replace (more reliable than clear() in vitest 4.x)
      await user.tripleClick(emailInput)
      await user.keyboard('newemail@example.com')

      const saveButton = screen.getByRole('button', { name: /save/i })
      await user.click(saveButton)

      await waitFor(() => {
        expect(mockToast.error).toHaveBeenCalledWith('Failed to update profile. Please try again.')
      })
    })

    it('should disable save button while submitting', async () => {
      // Configure mutate to set isPending flag when called
      mockMutate.mockImplementation(() => {
        mockIsPending = true
      })

      const { rerender } = renderWithRouter(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })

      const user = userEvent.setup({ delay: null })
      const emailInput = screen.getByLabelText(/email/i) as HTMLInputElement

      // Triple-click to select all, then type to replace (more reliable than clear() in vitest 4.x)
      await user.tripleClick(emailInput)
      await user.keyboard('newemail@example.com')

      const saveButton = screen.getByRole('button', { name: /save/i })
      await user.click(saveButton)

      // Rerender to reflect the pending state
      rerender(
        <MemoryRouter initialEntries={['/profile']}>
          <Routes>
            <Route path="/profile" element={<ProfilePage />} />
          </Routes>
        </MemoryRouter>
      )

      // The button text changes to "Saving..." when pending
      await waitFor(() => {
        expect(screen.getByRole('button', { name: /saving/i })).toBeDisabled()
      })
    }, 15000)
  })

  describe('Responsive design', () => {
    it('should render mobile layout on small screens', async () => {
      // Uses default mock from beforeEach

      const { container } = renderWithRouter(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByText('test@example.com')).toBeInTheDocument()
      })

      const profilePage = container.querySelector('.profile-page')
      expect(profilePage).toHaveClass('responsive')
    })
  })

  describe('Tab state persistence', () => {
    it('should default to Profile tab when no URL param is present', async () => {
      // Uses default mock from beforeEach

      renderWithRouter(<ProfilePage />, { initialUrl: '/profile' })

      await waitFor(() => {
        expect(screen.getByText('test@example.com')).toBeInTheDocument()
      })

      // Profile tab should be active (has active class)
      const profileTabButton = screen.getByRole('button', { name: /^Profile$/i })
      expect(profileTabButton).toHaveClass('active')

      // Privacy tab should not be active
      const privacyTabButton = screen.getByRole('button', { name: 'Privacy Settings' })
      expect(privacyTabButton).not.toHaveClass('active')

      // Profile content should be visible
      expect(screen.getByText('Account Information')).toBeInTheDocument()
    })

    it('should activate Privacy tab when URL has ?tab=privacy', async () => {
      // Uses default mock from beforeEach

      renderWithRouter(<ProfilePage />, { initialUrl: '/profile?tab=privacy' })

      await waitFor(() => {
        expect(screen.getByText('My Profile')).toBeInTheDocument()
      })

      // Privacy tab should be active (use exact match to avoid matching "Save Privacy Settings" button)
      const privacyTabButton = screen.getByRole('button', { name: 'Privacy Settings' })
      expect(privacyTabButton).toHaveClass('active')

      // Profile tab should not be active
      const profileTabButton = screen.getByRole('button', { name: /^Profile$/i })
      expect(profileTabButton).not.toHaveClass('active')

      // Privacy Settings content should be visible (use heading role to avoid matching tab button)
      expect(screen.getByRole('heading', { name: 'Privacy Settings' })).toBeInTheDocument()
    })

    it('should fallback to Profile tab for invalid URL tab param', async () => {
      // Uses default mock from beforeEach

      renderWithRouter(<ProfilePage />, { initialUrl: '/profile?tab=invalid' })

      await waitFor(() => {
        expect(screen.getByText('test@example.com')).toBeInTheDocument()
      })

      // Profile tab should be active (fallback to default)
      const profileTabButton = screen.getByRole('button', { name: /^Profile$/i })
      expect(profileTabButton).toHaveClass('active')

      // Profile content should be visible
      expect(screen.getByText('Account Information')).toBeInTheDocument()
    })

    it('should update URL when switching tabs', async () => {
      // Uses default mock from beforeEach

      renderWithRouter(<ProfilePage />, { initialUrl: '/profile' })

      await waitFor(() => {
        expect(screen.getByText('test@example.com')).toBeInTheDocument()
      })

      // Click Privacy Settings tab
      const user = userEvent.setup({ delay: null })
      const privacyTabButton = screen.getByRole('button', { name: 'Privacy Settings' })
      await user.click(privacyTabButton)

      // Privacy tab should now be active
      await waitFor(() => {
        expect(privacyTabButton).toHaveClass('active')
      })

      // Privacy content should be visible (use heading role to avoid matching tab button)
      expect(screen.getByRole('heading', { name: 'Privacy Settings' })).toBeInTheDocument()
    })
  })
})
