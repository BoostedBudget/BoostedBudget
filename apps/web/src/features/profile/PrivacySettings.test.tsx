/**
 * PrivacySettings Component Tests
 * Issue #960: Add confirmation dialog for critical setting changes
 *
 * Tests that disabling security-critical settings (security_alerts_enabled,
 * data_sharing_enabled) shows a warning confirmation before saving.
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { PrivacySettings } from './PrivacySettings'

import {
  mockUseGetPrivacySettings,
  mockUseUpdatePrivacySettings,
  mockUpdatePrivacySettingsMutateAsync,
} from '@/test/mocks/privacy-api'

const mockSettingsWithAlertsEnabled = {
  gdpr_necessary_consent: true,
  gdpr_analytics_consent: true,
  marketing_emails_enabled: true,
  product_updates_enabled: true,
  security_alerts_enabled: true,
  data_sharing_enabled: true,
}

function renderPrivacySettings() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <PrivacySettings />
    </QueryClientProvider>
  )
}

describe('PrivacySettings', () => {
  beforeEach(() => {
    mockUseGetPrivacySettings.mockClear()
    mockUseUpdatePrivacySettings.mockClear()
    mockUpdatePrivacySettingsMutateAsync.mockClear()

    mockUseGetPrivacySettings.mockReturnValue({
      data: mockSettingsWithAlertsEnabled,
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })

    mockUseUpdatePrivacySettings.mockReturnValue({
      mutateAsync: mockUpdatePrivacySettingsMutateAsync,
      isPending: false,
      isError: false,
      isSuccess: false,
      error: null,
      data: null,
      reset: () => {},
    })

    mockUpdatePrivacySettingsMutateAsync.mockResolvedValue(mockSettingsWithAlertsEnabled)
  })

  describe('Normal save (no critical changes)', () => {
    it('should save without confirmation when toggling non-critical settings', async () => {
      renderPrivacySettings()

      // Toggle marketing emails (non-critical)
      fireEvent.click(screen.getByLabelText(/marketing emails/i))
      fireEvent.click(screen.getByRole('button', { name: /save privacy settings/i }))

      // No confirmation dialog should appear
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()

      await waitFor(() => {
        expect(mockUpdatePrivacySettingsMutateAsync).toHaveBeenCalled()
      })
    })
  })

  describe('Security alerts confirmation', () => {
    it('should show warning dialog when disabling security alerts', async () => {
      renderPrivacySettings()

      // Disable security alerts
      fireEvent.click(screen.getByLabelText(/security alerts/i))
      fireEvent.click(screen.getByRole('button', { name: /save privacy settings/i }))

      // Warning confirmation dialog should appear
      await waitFor(() => {
        expect(screen.getByRole('alertdialog')).toBeInTheDocument()
      })
      // Should NOT have saved yet
      expect(mockUpdatePrivacySettingsMutateAsync).not.toHaveBeenCalled()
    })

    it('should save settings after confirming security alerts warning', async () => {
      renderPrivacySettings()

      fireEvent.click(screen.getByLabelText(/security alerts/i))
      fireEvent.click(screen.getByRole('button', { name: /save privacy settings/i }))

      await waitFor(() => {
        expect(screen.getByRole('alertdialog')).toBeInTheDocument()
      })

      // Click confirm
      fireEvent.click(screen.getByRole('button', { name: /confirm/i }))

      await waitFor(() => {
        expect(mockUpdatePrivacySettingsMutateAsync).toHaveBeenCalledWith({
          data: { security_alerts_enabled: false },
        })
      })
    })

    it('should not save when cancelling security alerts warning', async () => {
      renderPrivacySettings()

      fireEvent.click(screen.getByLabelText(/security alerts/i))
      fireEvent.click(screen.getByRole('button', { name: /save privacy settings/i }))

      await waitFor(() => {
        expect(screen.getByRole('alertdialog')).toBeInTheDocument()
      })

      // Click cancel
      fireEvent.click(screen.getByRole('button', { name: /cancel/i }))

      await waitFor(() => {
        expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
      })
      expect(mockUpdatePrivacySettingsMutateAsync).not.toHaveBeenCalled()
    })

    it('should NOT show dialog when enabling security alerts (only when disabling)', async () => {
      // Start with security alerts disabled
      mockUseGetPrivacySettings.mockReturnValue({
        data: { ...mockSettingsWithAlertsEnabled, security_alerts_enabled: false },
        isLoading: false,
        error: null,
        refetch: () => Promise.resolve(),
      })
      renderPrivacySettings()

      // Enable security alerts
      fireEvent.click(screen.getByLabelText(/security alerts/i))
      fireEvent.click(screen.getByRole('button', { name: /save privacy settings/i }))

      // No confirmation needed to enable
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()

      await waitFor(() => {
        expect(mockUpdatePrivacySettingsMutateAsync).toHaveBeenCalled()
      })
    })
  })
})
