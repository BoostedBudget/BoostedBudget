import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { PrivacySettings } from './PrivacySettings'
import * as privacyApi from './api/privacyApi'
import type { PrivacySettings as PrivacySettingsType } from './types'

// Mock the privacy API
vi.mock('./api/privacyApi')

describe('PrivacySettings', () => {
  const mockSettings: PrivacySettingsType = {
    id: '123e4567-e89b-12d3-a456-426614174000',
    user_id: 'user-uuid-123',
    gdpr_necessary_consent: true,
    gdpr_analytics_consent: true,
    gdpr_advertising_consent: false,
    gdpr_consent_timestamp: '2025-01-15T10:00:00Z',
    marketing_emails_enabled: false,
    product_updates_enabled: true,
    security_alerts_enabled: true,
    data_sharing_enabled: false,
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-15T10:00:00Z',
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Loading and data display', () => {
    it('should show loading state initially', () => {
      vi.mocked(privacyApi.fetchPrivacySettings).mockImplementation(
        () => new Promise(() => {}) // Never resolves
      )

      render(<PrivacySettings />)

      expect(screen.getByText(/loading/i)).toBeInTheDocument()

      // Clean up the never-resolving mock for subsequent tests
      vi.mocked(privacyApi.fetchPrivacySettings).mockReset()
    })

    it('should render privacy settings after loading', async () => {
      vi.mocked(privacyApi.fetchPrivacySettings).mockResolvedValue(mockSettings)

      render(<PrivacySettings />)

      await waitFor(() => {
        expect(screen.queryByText(/loading/i)).not.toBeInTheDocument()
      })

      // Check for GDPR section
      expect(screen.getByText(/GDPR Cookie Consent/i)).toBeInTheDocument()
      expect(screen.getByRole('checkbox', { name: /necessary/i })).toBeInTheDocument()
      expect(screen.getByRole('checkbox', { name: /analytics/i })).toBeInTheDocument()
      expect(screen.getByRole('checkbox', { name: /advertising/i })).toBeInTheDocument()

      // Check for Email Preferences section
      expect(screen.getByText(/Email Preferences/i)).toBeInTheDocument()
      expect(screen.getByText(/Marketing Emails/i)).toBeInTheDocument()
      expect(screen.getByText(/Product Updates/i)).toBeInTheDocument()
      expect(screen.getByText(/Security Alerts/i)).toBeInTheDocument()

      // Check for Data Sharing section
      expect(screen.getByRole('heading', { name: /Data Sharing/i })).toBeInTheDocument()
    })

    it('should display correct toggle states', async () => {
      vi.mocked(privacyApi.fetchPrivacySettings).mockResolvedValue(mockSettings)

      render(<PrivacySettings />)

      await waitFor(() => {
        expect(screen.queryByText(/loading/i)).not.toBeInTheDocument()
      })

      // GDPR toggles
      const analyticsCheckbox = screen.getByRole('checkbox', { name: /analytics/i })
      const advertisingCheckbox = screen.getByRole('checkbox', { name: /advertising/i })

      expect(analyticsCheckbox).toBeChecked()
      expect(advertisingCheckbox).not.toBeChecked()

      // Email preferences
      const productUpdatesCheckbox = screen.getByRole('checkbox', { name: /product updates/i })
      const marketingCheckbox = screen.getByRole('checkbox', { name: /marketing/i })

      expect(productUpdatesCheckbox).toBeChecked()
      expect(marketingCheckbox).not.toBeChecked()
    })

    it('should show necessary consent as disabled', async () => {
      vi.mocked(privacyApi.fetchPrivacySettings).mockResolvedValue(mockSettings)

      render(<PrivacySettings />)

      await waitFor(() => {
        expect(screen.queryByText(/loading/i)).not.toBeInTheDocument()
      })

      const necessaryCheckbox = screen.getByRole('checkbox', { name: /necessary/i })
      expect(necessaryCheckbox).toBeChecked()
      expect(necessaryCheckbox).toBeDisabled()
    })

    it('should handle loading error', async () => {
      vi.mocked(privacyApi.fetchPrivacySettings).mockRejectedValue(new Error('Failed to load'))

      render(<PrivacySettings />)

      await waitFor(() => {
        expect(screen.getByText(/failed to load privacy settings/i)).toBeInTheDocument()
      })
    })
  })

  describe('Toggle functionality', () => {
    it('should toggle analytics consent', async () => {
      vi.mocked(privacyApi.fetchPrivacySettings).mockResolvedValue(mockSettings)
      const user = userEvent.setup()

      render(<PrivacySettings />)

      await waitFor(() => {
        expect(screen.queryByText(/loading/i)).not.toBeInTheDocument()
      })

      const analyticsCheckbox = screen.getByRole('checkbox', { name: /analytics/i })
      expect(analyticsCheckbox).toBeChecked()

      await user.click(analyticsCheckbox)

      expect(analyticsCheckbox).not.toBeChecked()
    })

    it('should toggle advertising consent', async () => {
      vi.mocked(privacyApi.fetchPrivacySettings).mockResolvedValue(mockSettings)
      const user = userEvent.setup()

      render(<PrivacySettings />)

      await waitFor(() => {
        expect(screen.queryByText(/loading/i)).not.toBeInTheDocument()
      })

      const advertisingCheckbox = screen.getByRole('checkbox', { name: /advertising/i })
      expect(advertisingCheckbox).not.toBeChecked()

      await user.click(advertisingCheckbox)

      expect(advertisingCheckbox).toBeChecked()
    })

    it('should toggle email preferences', async () => {
      vi.mocked(privacyApi.fetchPrivacySettings).mockResolvedValue(mockSettings)
      const user = userEvent.setup()

      render(<PrivacySettings />)

      await waitFor(() => {
        expect(screen.queryByText(/loading/i)).not.toBeInTheDocument()
      })

      const marketingCheckbox = screen.getByRole('checkbox', { name: /marketing/i })
      expect(marketingCheckbox).not.toBeChecked()

      await user.click(marketingCheckbox)

      expect(marketingCheckbox).toBeChecked()
    })

    it('should not allow toggling necessary consent', async () => {
      vi.mocked(privacyApi.fetchPrivacySettings).mockResolvedValue(mockSettings)
      const user = userEvent.setup()

      render(<PrivacySettings />)

      await waitFor(() => {
        expect(screen.queryByText(/loading/i)).not.toBeInTheDocument()
      })

      const necessaryCheckbox = screen.getByRole('checkbox', { name: /necessary/i })
      expect(necessaryCheckbox).toBeChecked()
      expect(necessaryCheckbox).toBeDisabled()

      // Attempt to click (should be ignored)
      await user.click(necessaryCheckbox)

      expect(necessaryCheckbox).toBeChecked()
    })
  })

  describe('Save functionality', () => {
    it('should save changes on Save button click', async () => {
      vi.mocked(privacyApi.fetchPrivacySettings).mockResolvedValue(mockSettings)
      const updatedSettings = { ...mockSettings, gdpr_advertising_consent: true }
      vi.mocked(privacyApi.updatePrivacySettings).mockResolvedValue(updatedSettings)

      const user = userEvent.setup()

      render(<PrivacySettings />)

      await waitFor(() => {
        expect(screen.queryByText(/loading/i)).not.toBeInTheDocument()
      })

      // Toggle advertising
      const advertisingCheckbox = screen.getByRole('checkbox', { name: /advertising/i })
      await user.click(advertisingCheckbox)

      // Click Save button
      const saveButton = screen.getByRole('button', { name: /save/i })
      await user.click(saveButton)

      await waitFor(() => {
        expect(privacyApi.updatePrivacySettings).toHaveBeenCalledWith({
          gdpr_advertising_consent: true,
        })
      })
    })

    it('should show success message after save', async () => {
      vi.mocked(privacyApi.fetchPrivacySettings).mockResolvedValue(mockSettings)
      const updatedSettings = { ...mockSettings, gdpr_analytics_consent: false }
      vi.mocked(privacyApi.updatePrivacySettings).mockResolvedValue(updatedSettings)

      const user = userEvent.setup()

      render(<PrivacySettings />)

      await waitFor(() => {
        expect(screen.queryByText(/loading/i)).not.toBeInTheDocument()
      })

      // Toggle analytics
      const analyticsCheckbox = screen.getByRole('checkbox', { name: /analytics/i })
      await user.click(analyticsCheckbox)

      // Save
      const saveButton = screen.getByRole('button', { name: /save/i })
      await user.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/settings saved successfully/i)).toBeInTheDocument()
      })
    })

    it('should call onSettingsChange callback after successful save', async () => {
      vi.mocked(privacyApi.fetchPrivacySettings).mockResolvedValue(mockSettings)
      const updatedSettings = { ...mockSettings, marketing_emails_enabled: true }
      vi.mocked(privacyApi.updatePrivacySettings).mockResolvedValue(updatedSettings)

      const onSettingsChange = vi.fn()
      const user = userEvent.setup()

      render(<PrivacySettings onSettingsChange={onSettingsChange} />)

      await waitFor(() => {
        expect(screen.queryByText(/loading/i)).not.toBeInTheDocument()
      })

      // Toggle marketing emails
      const marketingCheckbox = screen.getByRole('checkbox', { name: /marketing/i })
      await user.click(marketingCheckbox)

      // Save
      const saveButton = screen.getByRole('button', { name: /save/i })
      await user.click(saveButton)

      await waitFor(() => {
        expect(onSettingsChange).toHaveBeenCalledWith(updatedSettings)
      })
    })

    it('should handle save errors', async () => {
      vi.mocked(privacyApi.fetchPrivacySettings).mockResolvedValue(mockSettings)
      vi.mocked(privacyApi.updatePrivacySettings).mockRejectedValue(new Error('Save failed'))

      const user = userEvent.setup()

      render(<PrivacySettings />)

      await waitFor(() => {
        expect(screen.queryByText(/loading/i)).not.toBeInTheDocument()
      })

      // Toggle something
      const analyticsCheckbox = screen.getByRole('checkbox', { name: /analytics/i })
      await user.click(analyticsCheckbox)

      // Save
      const saveButton = screen.getByRole('button', { name: /save/i })
      await user.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/failed to save privacy settings/i)).toBeInTheDocument()
      })
    })

    it('should disable save button when submitting', async () => {
      vi.mocked(privacyApi.fetchPrivacySettings).mockResolvedValue(mockSettings)
      vi.mocked(privacyApi.updatePrivacySettings).mockImplementation(
        () => new Promise(() => {}) // Never resolves
      )

      const user = userEvent.setup()

      render(<PrivacySettings />)

      await waitFor(() => {
        expect(screen.queryByText(/loading/i)).not.toBeInTheDocument()
      })

      // Toggle something
      const analyticsCheckbox = screen.getByRole('checkbox', { name: /analytics/i })
      await user.click(analyticsCheckbox)

      // Save
      const saveButton = screen.getByRole('button', { name: /save/i })
      await user.click(saveButton)

      await waitFor(() => {
        expect(saveButton).toBeDisabled()
      })
    })

    it('should only send changed fields in update request', async () => {
      vi.mocked(privacyApi.fetchPrivacySettings).mockResolvedValue(mockSettings)
      vi.mocked(privacyApi.updatePrivacySettings).mockResolvedValue(mockSettings)

      const user = userEvent.setup()

      render(<PrivacySettings />)

      await waitFor(() => {
        expect(screen.queryByText(/loading/i)).not.toBeInTheDocument()
      })

      // Toggle only advertising
      const advertisingCheckbox = screen.getByRole('checkbox', { name: /advertising/i })
      await user.click(advertisingCheckbox) // false → true

      // Save
      const saveButton = screen.getByRole('button', { name: /save/i })
      await user.click(saveButton)

      await waitFor(() => {
        expect(privacyApi.updatePrivacySettings).toHaveBeenCalledWith({
          gdpr_advertising_consent: true,
        })
      })

      // Should NOT include unchanged fields
      expect(privacyApi.updatePrivacySettings).not.toHaveBeenCalledWith(
        expect.objectContaining({
          gdpr_analytics_consent: expect.anything(),
        })
      )
    })
  })

  describe('Callback notifications', () => {
    it('should call onSettingsChange callback with updated settings after successful save', async () => {
      const onSettingsChange = vi.fn()
      vi.mocked(privacyApi.fetchPrivacySettings).mockResolvedValue(mockSettings)

      const updatedSettings: PrivacySettingsType = {
        ...mockSettings,
        gdpr_analytics_consent: false,
        updated_at: '2025-01-20T12:00:00Z',
      }
      vi.mocked(privacyApi.updatePrivacySettings).mockResolvedValue(updatedSettings)

      render(<PrivacySettings onSettingsChange={onSettingsChange} />)

      await waitFor(() => {
        expect(screen.getByLabelText(/analytics cookies/i)).toBeInTheDocument()
      })

      // Toggle analytics
      const analyticsCheckbox = screen.getByLabelText(/analytics cookies/i)
      await userEvent.click(analyticsCheckbox)

      // Save
      const saveButton = screen.getByRole('button', { name: /save privacy settings/i })
      await userEvent.click(saveButton)

      await waitFor(() => {
        expect(onSettingsChange).toHaveBeenCalledWith(updatedSettings)
      })
    })

    it('should not call onSettingsChange if save fails', async () => {
      const onSettingsChange = vi.fn()
      vi.mocked(privacyApi.fetchPrivacySettings).mockResolvedValue(mockSettings)
      vi.mocked(privacyApi.updatePrivacySettings).mockRejectedValue(new Error('Save failed'))

      render(<PrivacySettings onSettingsChange={onSettingsChange} />)

      await waitFor(() => {
        expect(screen.getByLabelText(/analytics cookies/i)).toBeInTheDocument()
      })

      // Toggle analytics
      const analyticsCheckbox = screen.getByLabelText(/analytics cookies/i)
      await userEvent.click(analyticsCheckbox)

      // Save
      const saveButton = screen.getByRole('button', { name: /save privacy settings/i })
      await userEvent.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/failed to save/i)).toBeInTheDocument()
      })

      expect(onSettingsChange).not.toHaveBeenCalled()
    })
  })
})
