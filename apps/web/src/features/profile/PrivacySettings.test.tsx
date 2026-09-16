/**
 * PrivacySettings Component Tests
 * Issue #960: Add confirmation dialog for critical setting changes
 *
 * Tests that disabling security-critical settings (security_alerts_enabled,
 * data_sharing_enabled) shows a warning confirmation before saving.
 */

import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { PrivacySettings } from './PrivacySettings'
import { privacySettingsQueryKey } from '@/shared/privacy/privacyQueryKey'

import {
  mockUseGetPrivacySettings,
  mockUseUpdatePrivacySettings,
  mockUpdatePrivacySettingsMutateAsync,
} from '@/test/mocks/privacy-api'
import { createMockClerkUser, mockClerkUser, mockUseUser } from '@/test/mocks/clerk'
import {
  mockCaptureProductEvent,
  mockGrantProductAnalytics,
  mockWithdrawProductAnalytics,
} from '@/test/mocks/posthog'

const mockSettingsWithAlertsEnabled = {
  gdpr_necessary_consent: true,
  gdpr_analytics_consent: true,
  marketing_emails_enabled: true,
  product_updates_enabled: true,
  security_alerts_enabled: true,
  data_sharing_enabled: true,
  include_in_aggregate_stats: true,
}

let analyticsGeneration = 1

function renderPrivacySettings() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const result = render(
    <QueryClientProvider client={queryClient}>
      <PrivacySettings analyticsActions={{
        grant: mockGrantProductAnalytics,
        getSnapshot: () => ({ generation: analyticsGeneration, scope: mockClerkUser.id, state: 'denied' }),
        withdraw: mockWithdrawProductAnalytics,
      }} />
    </QueryClientProvider>
  )
  return { ...result, queryClient }
}

describe('PrivacySettings', () => {
  beforeEach(() => {
    analyticsGeneration = 1
    mockUseGetPrivacySettings.mockClear()
    mockUseUpdatePrivacySettings.mockClear()
    mockUpdatePrivacySettingsMutateAsync.mockClear()
    mockGrantProductAnalytics.mockClear()
    mockWithdrawProductAnalytics.mockClear()
    mockCaptureProductEvent.mockClear()
    mockUseUser.mockReturnValue(createMockClerkUser({
      isLoaded: true,
      isSignedIn: true,
      user: mockClerkUser,
    }))

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

  describe('Aggregate stats toggle', () => {
    it('renders include_in_aggregate_stats checkbox with correct initial state', () => {
      renderPrivacySettings()
      const checkbox = screen.getByLabelText(/include my transfers in aggregate statistics/i)
      expect(checkbox).toBeInTheDocument()
      expect((checkbox as HTMLInputElement).checked).toBe(true)
    })

    it('calls update mutation when aggregate stats toggle is changed', async () => {
      renderPrivacySettings()
      const checkbox = screen.getByLabelText(/include my transfers in aggregate statistics/i)
      fireEvent.click(checkbox)
      fireEvent.click(screen.getByRole('button', { name: /save privacy settings/i }))

      await waitFor(() => {
        expect(mockUpdatePrivacySettingsMutateAsync).toHaveBeenCalledWith({
          data: { include_in_aggregate_stats: false },
        })
      })
    })
  })

  describe('analytics consent ordering', () => {
    it('waits for the backend before granting without emitting a privacy event', async () => {
      let resolveSave!: (value: typeof mockSettingsWithAlertsEnabled) => void
      mockUseGetPrivacySettings.mockReturnValue({
        data: { ...mockSettingsWithAlertsEnabled, gdpr_analytics_consent: false },
        isLoading: false,
        error: null,
        refetch: () => Promise.resolve(),
      })
      mockUpdatePrivacySettingsMutateAsync.mockImplementation(() => new Promise(resolve => {
        resolveSave = resolve
      }))
      renderPrivacySettings()

      fireEvent.click(screen.getByLabelText(/analytics cookies/i))
      fireEvent.click(screen.getByRole('button', { name: /save privacy settings/i }))
      expect(mockGrantProductAnalytics).not.toHaveBeenCalled()

      resolveSave({ ...mockSettingsWithAlertsEnabled, gdpr_analytics_consent: true })
      await waitFor(() => expect(mockGrantProductAnalytics).toHaveBeenCalledWith(
        mockClerkUser.id,
        mockClerkUser.id,
        expect.objectContaining({ subscription_tier: 'free', primary_region: 'iad' }),
      ))
      expect(mockCaptureProductEvent).not.toHaveBeenCalledWith('privacy_settings_changed')
    })

    it('treats a successful grant mutation as authority even when background refetch fails', async () => {
      const refetch = mock(() => Promise.reject(new Error('offline after save')))
      mockUseGetPrivacySettings.mockReturnValue({
        data: { ...mockSettingsWithAlertsEnabled, gdpr_analytics_consent: false },
        isLoading: false,
        error: null,
        refetch,
      })
      mockUpdatePrivacySettingsMutateAsync.mockResolvedValue({
        ...mockSettingsWithAlertsEnabled,
        gdpr_analytics_consent: true,
      })
      const { queryClient } = renderPrivacySettings()
      const invalidate = mock(queryClient.invalidateQueries.bind(queryClient))
      queryClient.invalidateQueries = invalidate

      fireEvent.click(screen.getByLabelText(/analytics cookies/i))
      fireEvent.click(screen.getByRole('button', { name: /save privacy settings/i }))

      await waitFor(() => expect(mockGrantProductAnalytics).toHaveBeenCalledWith(
        mockClerkUser.id,
        mockClerkUser.id,
        expect.any(Object),
      ))
      expect(screen.getByText(/settings saved successfully/i)).toBeInTheDocument()
      expect(screen.queryByText(/failed to save privacy settings/i)).not.toBeInTheDocument()
      expect(refetch).not.toHaveBeenCalled()
      expect(invalidate).toHaveBeenCalledWith({
        queryKey: privacySettingsQueryKey(mockClerkUser.id),
        exact: true,
        refetchType: 'none',
      })
    })

    it('does not apply a late backend grant after a newer cross-tab withdrawal', async () => {
      let resolveSave!: (value: typeof mockSettingsWithAlertsEnabled) => void
      mockUseGetPrivacySettings.mockReturnValue({
        data: { ...mockSettingsWithAlertsEnabled, gdpr_analytics_consent: false },
        isLoading: false,
        error: null,
        refetch: () => Promise.resolve(),
      })
      mockUpdatePrivacySettingsMutateAsync.mockImplementation(() => new Promise(resolve => {
        resolveSave = resolve
      }))
      renderPrivacySettings()

      fireEvent.click(screen.getByLabelText(/analytics cookies/i))
      fireEvent.click(screen.getByRole('button', { name: /save privacy settings/i }))
      analyticsGeneration = 2
      resolveSave({ ...mockSettingsWithAlertsEnabled, gdpr_analytics_consent: true })

      await screen.findByText(/settings saved successfully/i)
      expect(mockGrantProductAnalytics).not.toHaveBeenCalled()
    })

    it('withdraws locally before persistence and stays off when persistence fails', async () => {
      const order: string[] = []
      mockWithdrawProductAnalytics.mockImplementation(() => { order.push('withdraw') })
      mockUpdatePrivacySettingsMutateAsync.mockImplementation(() => {
        order.push('persist')
        return Promise.reject(new Error('offline'))
      })
      renderPrivacySettings()

      fireEvent.click(screen.getByLabelText(/analytics cookies/i))
      fireEvent.click(screen.getByRole('button', { name: /save privacy settings/i }))

      expect(order).toEqual(['withdraw', 'persist'])
      expect(mockWithdrawProductAnalytics).toHaveBeenCalledWith(mockClerkUser.id)
      await waitFor(() => {
        expect(screen.getByText(/failed to save privacy settings/i)).toBeInTheDocument()
      })
      expect(mockGrantProductAnalytics).not.toHaveBeenCalled()
      expect(mockCaptureProductEvent).not.toHaveBeenCalledWith('privacy_settings_changed')
    })

    it('recovers a failed withdrawal only after an explicit resume and confirmed server true', async () => {
      const refetch = mock(() => Promise.resolve({
        data: { ...mockSettingsWithAlertsEnabled, gdpr_analytics_consent: true },
      }))
      mockUseGetPrivacySettings.mockReturnValue({
        data: mockSettingsWithAlertsEnabled,
        isLoading: false,
        error: null,
        refetch,
      })
      mockUpdatePrivacySettingsMutateAsync.mockRejectedValue(new Error('offline'))
      renderPrivacySettings()

      fireEvent.click(screen.getByLabelText(/analytics cookies/i))
      fireEvent.click(screen.getByRole('button', { name: /save privacy settings/i }))
      await screen.findByText(/failed to save privacy settings/i)
      expect(mockGrantProductAnalytics).not.toHaveBeenCalled()

      fireEvent.click(screen.getByLabelText(/analytics cookies/i))
      fireEvent.click(screen.getByRole('button', { name: /save privacy settings/i }))

      await waitFor(() => expect(refetch).toHaveBeenCalledTimes(1))
      expect(mockGrantProductAnalytics).toHaveBeenCalledWith(
        mockClerkUser.id,
        mockClerkUser.id,
        expect.any(Object),
      )
      expect(screen.queryByText(/no changes to save/i)).not.toBeInTheDocument()
      expect(mockUpdatePrivacySettingsMutateAsync).toHaveBeenCalledTimes(1)
    })

    it('keeps the withdrawal latch when refetch returns stale true data with an error', async () => {
      const refetch = mock(() => Promise.resolve({
        data: { ...mockSettingsWithAlertsEnabled, gdpr_analytics_consent: true },
        error: new Error('still offline'),
        isError: true,
      }))
      mockUseGetPrivacySettings.mockReturnValue({
        data: mockSettingsWithAlertsEnabled,
        isLoading: false,
        error: null,
        refetch,
      })
      mockUpdatePrivacySettingsMutateAsync.mockRejectedValue(new Error('offline'))
      renderPrivacySettings()

      fireEvent.click(screen.getByLabelText(/analytics cookies/i))
      fireEvent.click(screen.getByRole('button', { name: /save privacy settings/i }))
      await screen.findByText(/failed to save privacy settings/i)

      fireEvent.click(screen.getByLabelText(/analytics cookies/i))
      fireEvent.click(screen.getByRole('button', { name: /save privacy settings/i }))

      await waitFor(() => expect(refetch).toHaveBeenCalledTimes(1))
      expect(mockGrantProductAnalytics).not.toHaveBeenCalled()
      expect(screen.getByText(/could not confirm analytics consent/i)).toBeInTheDocument()
    })

    it('ignores a late successful recovery after a newer cross-tab withdrawal', async () => {
      let resolveRefetch!: (value: {
        data: typeof mockSettingsWithAlertsEnabled
        error: null
      }) => void
      const refetch = mock(() => new Promise(resolve => { resolveRefetch = resolve }))
      mockUseGetPrivacySettings.mockReturnValue({
        data: mockSettingsWithAlertsEnabled,
        isLoading: false,
        error: null,
        refetch,
      })
      mockUpdatePrivacySettingsMutateAsync.mockRejectedValue(new Error('offline'))
      renderPrivacySettings()

      fireEvent.click(screen.getByLabelText(/analytics cookies/i))
      fireEvent.click(screen.getByRole('button', { name: /save privacy settings/i }))
      await screen.findByText(/failed to save privacy settings/i)
      fireEvent.click(screen.getByLabelText(/analytics cookies/i))
      fireEvent.click(screen.getByRole('button', { name: /save privacy settings/i }))
      await waitFor(() => expect(refetch).toHaveBeenCalledTimes(1))
      expect(screen.getByRole('button', { name: /saving/i })).toBeDisabled()

      analyticsGeneration = 2
      resolveRefetch({ data: mockSettingsWithAlertsEnabled, error: null })

      await waitFor(() => expect(screen.getByText(/could not confirm analytics consent/i)).toBeInTheDocument())
      expect(mockGrantProductAnalytics).not.toHaveBeenCalled()
    })

    it('writes a successful mutation to the same identity-scoped privacy cache', async () => {
      const updated = { ...mockSettingsWithAlertsEnabled, marketing_emails_enabled: false }
      mockUpdatePrivacySettingsMutateAsync.mockResolvedValue(updated)
      const { queryClient } = renderPrivacySettings()

      fireEvent.click(screen.getByLabelText(/marketing emails/i))
      fireEvent.click(screen.getByRole('button', { name: /save privacy settings/i }))

      await waitFor(() => expect(queryClient.getQueryData(
        privacySettingsQueryKey(mockClerkUser.id),
      )).toEqual(updated))
      expect(mockUseGetPrivacySettings.mock.calls[0]?.[0]?.query?.queryKey).toEqual(
        privacySettingsQueryKey(mockClerkUser.id),
      )
    })

    it('clears drafts, recovery, and messages when Clerk switches directly from A to B', async () => {
      const refetchA = mock(() => Promise.resolve({ data: mockSettingsWithAlertsEnabled }))
      mockUseGetPrivacySettings.mockReturnValue({
        data: mockSettingsWithAlertsEnabled,
        isLoading: false,
        error: null,
        refetch: refetchA,
      })
      mockUpdatePrivacySettingsMutateAsync.mockRejectedValue(new Error('offline'))
      const rendered = renderPrivacySettings()

      fireEvent.click(screen.getByLabelText(/analytics cookies/i))
      fireEvent.click(screen.getByLabelText(/marketing emails/i))
      fireEvent.click(screen.getByRole('button', { name: /save privacy settings/i }))
      await screen.findByText(/failed to save privacy settings/i)

      const userB = { ...mockClerkUser, id: 'user-b' }
      mockUseUser.mockReturnValue(createMockClerkUser({
        isLoaded: true,
        isSignedIn: true,
        user: userB,
      }))
      const refetchB = mock(() => Promise.resolve({ data: mockSettingsWithAlertsEnabled }))
      mockUseGetPrivacySettings.mockReturnValue({
        data: mockSettingsWithAlertsEnabled,
        isLoading: false,
        error: null,
        refetch: refetchB,
      })
      rendered.rerender(
        <QueryClientProvider client={rendered.queryClient}>
          <PrivacySettings analyticsActions={{
            grant: mockGrantProductAnalytics,
            getSnapshot: () => ({ generation: analyticsGeneration, scope: userB.id, state: 'denied' }),
            withdraw: mockWithdrawProductAnalytics,
          }} />
        </QueryClientProvider>,
      )

      await waitFor(() => {
        expect(screen.queryByText(/failed to save privacy settings/i)).not.toBeInTheDocument()
      })
      expect(screen.getByLabelText(/analytics cookies/i)).toBeChecked()
      expect(screen.getByLabelText(/marketing emails/i)).toBeChecked()
      fireEvent.click(screen.getByRole('button', { name: /save privacy settings/i }))
      expect(await screen.findByText(/no changes to save/i)).toBeInTheDocument()
      expect(refetchB).not.toHaveBeenCalled()
    })

    it('ignores a late save result from user A after switching directly to user B', async () => {
      let resolveSave!: (value: typeof mockSettingsWithAlertsEnabled) => void
      mockUseGetPrivacySettings.mockReturnValue({
        data: { ...mockSettingsWithAlertsEnabled, gdpr_analytics_consent: false },
        isLoading: false,
        error: null,
        refetch: () => Promise.resolve(),
      })
      mockUpdatePrivacySettingsMutateAsync.mockImplementation(() => new Promise(resolve => {
        resolveSave = resolve
      }))
      const rendered = renderPrivacySettings()
      fireEvent.click(screen.getByLabelText(/analytics cookies/i))
      fireEvent.click(screen.getByRole('button', { name: /save privacy settings/i }))

      const userB = { ...mockClerkUser, id: 'user-b' }
      mockUseUser.mockReturnValue(createMockClerkUser({ isLoaded: true, isSignedIn: true, user: userB }))
      mockUseGetPrivacySettings.mockReturnValue({
        data: mockSettingsWithAlertsEnabled,
        isLoading: false,
        error: null,
        refetch: () => Promise.resolve(),
      })
      mockUseUpdatePrivacySettings.mockReturnValue({
        mutate: mock(() => {}),
        mutateAsync: mockUpdatePrivacySettingsMutateAsync,
        isPending: true,
        isError: false,
        isSuccess: false,
        error: null,
        data: null,
        reset: () => {},
      })
      rendered.rerender(
        <QueryClientProvider client={rendered.queryClient}>
          <PrivacySettings analyticsActions={{
            grant: mockGrantProductAnalytics,
            getSnapshot: () => ({ generation: analyticsGeneration, scope: userB.id, state: 'denied' }),
            withdraw: mockWithdrawProductAnalytics,
          }} />
        </QueryClientProvider>,
      )
      const saveButton = screen.getByRole('button', { name: /save privacy settings/i })
      expect(saveButton).not.toBeDisabled()
      fireEvent.click(saveButton)
      expect(await screen.findByText(/no changes to save/i)).toBeInTheDocument()
      expect(saveButton).not.toBeDisabled()
      resolveSave(mockSettingsWithAlertsEnabled)

      await waitFor(() => expect(mockUpdatePrivacySettingsMutateAsync).toHaveBeenCalledTimes(1))
      expect(mockGrantProductAnalytics).not.toHaveBeenCalled()
      expect(screen.queryByText(/settings saved successfully/i)).not.toBeInTheDocument()
    })

    it('ignores a late withdrawal-recovery result from user A after switching to B', async () => {
      let resolveRecovery!: (value: { data: typeof mockSettingsWithAlertsEnabled; error: null }) => void
      const recovery = mock(() => new Promise(resolve => { resolveRecovery = resolve }))
      mockUseGetPrivacySettings.mockReturnValue({
        data: mockSettingsWithAlertsEnabled,
        isLoading: false,
        error: null,
        refetch: recovery,
      })
      mockUpdatePrivacySettingsMutateAsync.mockRejectedValue(new Error('offline'))
      const rendered = renderPrivacySettings()
      fireEvent.click(screen.getByLabelText(/analytics cookies/i))
      fireEvent.click(screen.getByRole('button', { name: /save privacy settings/i }))
      await screen.findByText(/failed to save privacy settings/i)
      fireEvent.click(screen.getByLabelText(/analytics cookies/i))
      fireEvent.click(screen.getByRole('button', { name: /save privacy settings/i }))
      await waitFor(() => expect(recovery).toHaveBeenCalledTimes(1))

      const userB = { ...mockClerkUser, id: 'user-b' }
      mockUseUser.mockReturnValue(createMockClerkUser({ isLoaded: true, isSignedIn: true, user: userB }))
      mockUseGetPrivacySettings.mockReturnValue({
        data: mockSettingsWithAlertsEnabled,
        isLoading: false,
        error: null,
        refetch: () => Promise.resolve(),
      })
      rendered.rerender(
        <QueryClientProvider client={rendered.queryClient}>
          <PrivacySettings />
        </QueryClientProvider>,
      )
      resolveRecovery({ data: mockSettingsWithAlertsEnabled, error: null })
      await new Promise(resolve => setTimeout(resolve, 0))

      expect(screen.queryByText(/could not confirm analytics consent/i)).not.toBeInTheDocument()
      expect(screen.getByLabelText(/analytics cookies/i)).toBeChecked()
    })
  })
})
