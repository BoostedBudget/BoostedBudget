import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { ProfilePage } from './ProfilePage'
import * as profileApi from './api/profileApi'
import type { User } from './types'
import * as useSubscriptionTiersHook from '../subscription/hooks/useSubscriptionTiers'
import type { SubscriptionTiersResponse } from '../subscription/types'

// Mock the profile API
vi.mock('./api/profileApi')

// Mock the subscription tiers hook
vi.mock('../subscription/hooks/useSubscriptionTiers')

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

  const mockTiersResponse: SubscriptionTiersResponse = {
    tiers: {
      free: {
        display_name: 'Free',
        description: 'Basic plan',
        price_monthly: 0,
        price_yearly: 0,
        limits: {
          accounts: 3,
          rules: 1,
          executions_per_day: 1,
          min_execution_interval_minutes: 1440,
        },
        features: {
          ads_enabled: true,
          ai_insights: false,
        },
      },
      premium_lite: {
        display_name: 'Premium Lite',
        description: 'Lite plan',
        price_monthly: 4.99,
        price_yearly: 49.99,
        limits: {
          accounts: 15,
          rules: 5,
          executions_per_day: 2,
          min_execution_interval_minutes: 720,
        },
        features: {
          ads_enabled: false,
          ai_insights: false,
        },
      },
      premium: {
        display_name: 'Premium',
        description: 'Premium plan',
        price_monthly: 9.99,
        price_yearly: 99.99,
        limits: {
          accounts: 30,
          rules: 15,
          executions_per_day: 6,
          min_execution_interval_minutes: 240,
        },
        features: {
          ads_enabled: false,
          ai_insights: false,
        },
      },
      premium_plus: {
        display_name: 'Premium+',
        description: 'Premium plus plan',
        price_monthly: 19.99,
        price_yearly: 199.99,
        limits: {
          accounts: -1,
          rules: -1,
          executions_per_day: 144,
          min_execution_interval_minutes: 10,
        },
        features: {
          ads_enabled: false,
          ai_insights: true,
          api_access: true,
          priority_support: true,
        },
      },
    },
    default_tier: 'free',
  }

  beforeEach(() => {
    vi.clearAllMocks()
    mockUseAuth0.mockReturnValue({
      isAuthenticated: true,
      user: { email: 'test@example.com' },
    })

    // Mock subscription tiers hook to return tier data
    vi.mocked(useSubscriptionTiersHook.useSubscriptionTiers).mockReturnValue({
      tiers: mockTiersResponse,
      loading: false,
      error: null,
      refetch: vi.fn(),
    })
  })

  describe('Loading and data display', () => {
    it('should show loading state initially', () => {
      vi.mocked(profileApi.fetchProfile).mockImplementationOnce(
        () => new Promise(() => {}) // Never resolves
      )

      renderWithRouter(<ProfilePage />)

      expect(screen.getByText(/loading/i)).toBeInTheDocument()
    })

    it('should render user profile data after loading', async () => {
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)

      renderWithRouter(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByText('test@example.com')).toBeInTheDocument()
      })

      // Check for new geographic display format
      expect(screen.getByText(/USA \(Virginia\)/i)).toBeInTheDocument()
    })

    it('should display user email from profile', async () => {
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)

      renderWithRouter(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })
    })

    it('should show data region correctly', async () => {
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)

      renderWithRouter(<ProfilePage />)

      await waitFor(() => {
        // Check for new geographic display format (iad maps to "USA (Virginia)")
        expect(screen.getByText(/USA \(Virginia\)/i)).toBeInTheDocument()
      })
    })

    it('should show current subscription tier', async () => {
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)

      renderWithRouter(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByText(/premium/i)).toBeInTheDocument()
      })
    })
  })

  describe('Error handling', () => {
    it('should show error message when API fails', async () => {
      vi.mocked(profileApi.fetchProfile).mockRejectedValue(new Error('API Error'))

      renderWithRouter(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByText(/failed to load profile/i)).toBeInTheDocument()
      })
    })

    it('should show retry button on error', async () => {
      vi.mocked(profileApi.fetchProfile).mockRejectedValue(new Error('API Error'))

      renderWithRouter(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument()
      })
    })

    it('should retry fetching profile when retry button clicked', async () => {
      const fetchProfileMock = vi.mocked(profileApi.fetchProfile)
      fetchProfileMock.mockRejectedValueOnce(new Error('API Error'))
      fetchProfileMock.mockResolvedValueOnce(mockUser)

      renderWithRouter(<ProfilePage />)

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

      renderWithRouter(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })

      const user = userEvent.setup()
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
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)

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
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)

      renderWithRouter(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })

      const user = userEvent.setup()
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
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)
      vi.mocked(profileApi.updateProfile).mockResolvedValue({
        ...mockUser,
        email: 'newemail@example.com',
      })

      renderWithRouter(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })

      const user = userEvent.setup()
      const emailInput = screen.getByLabelText(/email/i) as HTMLInputElement

      // Triple-click to select all, then type to replace (more reliable than clear() in vitest 4.x)
      await user.tripleClick(emailInput)
      await user.keyboard('newemail@example.com')

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
        primary_region: 'lhr',
      })

      renderWithRouter(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })

      const user = userEvent.setup()
      const regionSelect = screen.getByLabelText(/region/i)

      await user.selectOptions(regionSelect, 'lhr')

      const saveButton = screen.getByRole('button', { name: /save/i })
      await user.click(saveButton)

      await waitFor(() => {
        expect(profileApi.updateProfile).toHaveBeenCalledWith({
          email: 'test@example.com',
          primary_region: 'lhr',
        })
      })
    })

    it('should show success message after update', async () => {
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)
      vi.mocked(profileApi.updateProfile).mockResolvedValue({
        ...mockUser,
        email: 'newemail@example.com',
      })

      renderWithRouter(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })

      const user = userEvent.setup()
      const emailInput = screen.getByLabelText(/email/i)

      await user.tripleClick(emailInput)
      await user.keyboard('newemail@example.com')

      const saveButton = screen.getByRole('button', { name: /save/i })
      await user.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/profile updated successfully/i)).toBeInTheDocument()
      })
    })

    it('should show error message when update fails', async () => {
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)
      vi.mocked(profileApi.updateProfile).mockRejectedValue(new Error('Update failed'))

      renderWithRouter(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })

      const user = userEvent.setup()
      const emailInput = screen.getByLabelText(/email/i) as HTMLInputElement

      // Triple-click to select all, then type to replace (more reliable than clear() in vitest 4.x)
      await user.tripleClick(emailInput)
      await user.keyboard('newemail@example.com')

      const saveButton = screen.getByRole('button', { name: /save/i })
      await user.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/failed to update profile/i)).toBeInTheDocument()
      })
    })

    it('should disable save button while submitting', async () => {
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)
      vi.mocked(profileApi.updateProfile).mockImplementationOnce(
        () => new Promise(() => {}) // Never resolves
      )

      renderWithRouter(<ProfilePage />)

      await waitFor(() => {
        expect(screen.getByDisplayValue('test@example.com')).toBeInTheDocument()
      })

      const user = userEvent.setup()
      const emailInput = screen.getByLabelText(/email/i) as HTMLInputElement

      // Triple-click to select all, then type to replace (more reliable than clear() in vitest 4.x)
      await user.tripleClick(emailInput)
      await user.keyboard('newemail@example.com')

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
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)

      renderWithRouter(<ProfilePage />, { initialUrl: '/profile' })

      await waitFor(() => {
        expect(screen.getByText('test@example.com')).toBeInTheDocument()
      })

      // Profile tab should be active (has active class)
      const profileTabButton = screen.getByRole('button', { name: /^Profile$/i })
      expect(profileTabButton).toHaveClass('active')

      // Privacy tab should not be active
      const privacyTabButton = screen.getByRole('button', { name: /Privacy Settings/i })
      expect(privacyTabButton).not.toHaveClass('active')

      // Profile content should be visible
      expect(screen.getByText('Account Information')).toBeInTheDocument()
    })

    it('should activate Privacy tab when URL has ?tab=privacy', async () => {
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)

      renderWithRouter(<ProfilePage />, { initialUrl: '/profile?tab=privacy' })

      await waitFor(() => {
        expect(screen.getByText('My Profile')).toBeInTheDocument()
      })

      // Privacy tab should be active
      const privacyTabButton = screen.getByRole('button', { name: /Privacy Settings/i })
      expect(privacyTabButton).toHaveClass('active')

      // Profile tab should not be active
      const profileTabButton = screen.getByRole('button', { name: /^Profile$/i })
      expect(profileTabButton).not.toHaveClass('active')

      // Privacy Settings content should be visible
      expect(screen.getByText('Privacy Settings')).toBeInTheDocument()
    })

    it('should fallback to Profile tab for invalid URL tab param', async () => {
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)

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
      vi.mocked(profileApi.fetchProfile).mockResolvedValue(mockUser)

      renderWithRouter(<ProfilePage />, { initialUrl: '/profile' })

      await waitFor(() => {
        expect(screen.getByText('test@example.com')).toBeInTheDocument()
      })

      // Click Privacy Settings tab
      const user = userEvent.setup()
      const privacyTabButton = screen.getByRole('button', { name: /Privacy Settings/i })
      await user.click(privacyTabButton)

      // Privacy tab should now be active
      await waitFor(() => {
        expect(privacyTabButton).toHaveClass('active')
      })

      // Privacy content should be visible
      expect(screen.getByText('Privacy Settings')).toBeInTheDocument()
    })
  })
})
