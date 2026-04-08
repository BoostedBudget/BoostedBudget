import { useState, useMemo } from 'react'
import {
  useGetPrivacySettings,
  useUpdatePrivacySettings,
} from '@boostedbudget/api-client/endpoints/privacy/privacy'
import { useTrackEvent } from '../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../shared/analytics'
import type { PrivacySettings as PrivacySettingsType, UpdatePrivacySettingsRequest } from './types'
import { DeleteAccountSection } from '../deletion/components/DeleteAccountSection'

interface PrivacySettingsProps {
  onSettingsChange?: (settings: PrivacySettingsType) => void
}

export function PrivacySettings({ onSettingsChange }: PrivacySettingsProps) {
  const [successMessage, setSuccessMessage] = useState<string | null>(null)
  const [saveError, setSaveError] = useState<string | null>(null)
  const trackEvent = useTrackEvent()

  // Use generated query hook for loading settings
  const {
    data: settings,
    isLoading: loading,
    error: queryError,
    refetch,
  } = useGetPrivacySettings()

  // Use generated mutation hook for updating settings
  const updateMutation = useUpdatePrivacySettings()

  // Track only user modifications (not the full state)
  const [modifications, setModifications] = useState<Partial<PrivacySettingsType>>({})

  // Derive form state from settings merged with user modifications
  const formState = useMemo<Partial<PrivacySettingsType>>(() => ({
    ...(settings as PrivacySettingsType | undefined),
    ...modifications,
  }), [settings, modifications])

  // Compute loading error message
  const error = queryError ? 'Failed to load privacy settings. Please try again.' : saveError

  function handleToggle(field: keyof PrivacySettingsType) {
    setModifications(prev => ({
      ...prev,
      [field]: !(formState[field] as boolean),
    }))
    // Clear success message when user makes changes
    setSuccessMessage(null)
  }

  async function handleSave() {
    if (!settings) return

    // Build update request with only changed fields
    const updates: UpdatePrivacySettingsRequest = {}

    // Compare formState with original settings
    if (formState.gdpr_necessary_consent !== undefined && formState.gdpr_necessary_consent !== settings.gdpr_necessary_consent) {
      updates.gdpr_necessary_consent = formState.gdpr_necessary_consent
    }
    if (formState.gdpr_analytics_consent !== undefined && formState.gdpr_analytics_consent !== settings.gdpr_analytics_consent) {
      updates.gdpr_analytics_consent = formState.gdpr_analytics_consent
    }
    if (formState.marketing_emails_enabled !== undefined && formState.marketing_emails_enabled !== settings.marketing_emails_enabled) {
      updates.marketing_emails_enabled = formState.marketing_emails_enabled
    }
    if (formState.product_updates_enabled !== undefined && formState.product_updates_enabled !== settings.product_updates_enabled) {
      updates.product_updates_enabled = formState.product_updates_enabled
    }
    if (formState.security_alerts_enabled !== undefined && formState.security_alerts_enabled !== settings.security_alerts_enabled) {
      updates.security_alerts_enabled = formState.security_alerts_enabled
    }
    if (formState.data_sharing_enabled !== undefined && formState.data_sharing_enabled !== settings.data_sharing_enabled) {
      updates.data_sharing_enabled = formState.data_sharing_enabled
    }

    // If no changes, don't make the request
    if (Object.keys(updates).length === 0) {
      setSuccessMessage('No changes to save')
      return
    }

    try {
      setSaveError(null)
      setSuccessMessage(null)

      const updatedSettings = await updateMutation.mutateAsync({ data: updates })

      // Clear modifications since they're now saved
      setModifications({})
      setSuccessMessage('Settings saved successfully!')

      // Track privacy settings change
      trackEvent(ANALYTICS_EVENTS.PRIVACY_SETTINGS_CHANGED)

      // Notify parent component
      onSettingsChange?.(updatedSettings as PrivacySettingsType)
    } catch (err) {
      console.error('Failed to save privacy settings:', err)
      setSaveError('Failed to save privacy settings. Please try again.')
    }
  }

  // Derive submitting state from mutation
  const submitting = updateMutation.isPending

  if (loading) {
    return (
      <div className="max-w-3xl">
        <p className="text-muted-foreground">Loading privacy settings...</p>
      </div>
    )
  }

  if (error && !settings) {
    return (
      <div className="max-w-3xl">
        <p className="text-destructive bg-destructive/10 px-4 py-3 rounded-md mb-4">{error}</p>
        <button
          onClick={() => refetch()}
          className="bg-primary text-primary-foreground border-none px-6 py-3 rounded-md text-base font-semibold cursor-pointer hover:opacity-90 transition-opacity"
        >
          Retry
        </button>
      </div>
    )
  }

  if (!settings) {
    return null
  }

  return (
    <div className="max-w-3xl">
      <h2 className="text-2xl font-bold text-foreground mt-0 mb-6">Privacy Settings</h2>

      {/* GDPR Cookie Consent Section */}
      <section className="bg-card border border-border rounded-lg p-6 mb-6">
        <h3 className="text-xl font-semibold text-foreground mt-0 mb-2">GDPR Cookie Consent</h3>
        <p className="text-muted-foreground mb-6 leading-relaxed">
          Manage your cookie preferences. Necessary cookies are required for the app to function.
        </p>

        <div className="mb-6 pb-6 border-b border-muted last:border-b-0 last:mb-0 last:pb-0">
          <label className="flex items-center cursor-pointer font-medium text-foreground">
            <input
              type="checkbox"
              className="w-5 h-5 mr-3 cursor-not-allowed opacity-60"
              checked={formState.gdpr_necessary_consent ?? false}
              disabled={true}
              aria-label="Necessary cookies"
            />
            <span className="text-base font-semibold">Necessary</span>
          </label>
          <p className="mt-2 ml-8 text-muted-foreground text-sm leading-relaxed">
            Required for basic app functionality. Cannot be disabled.
          </p>
        </div>

        <div className="mb-6 pb-6 border-b border-muted last:border-b-0 last:mb-0 last:pb-0">
          <label className="flex items-center cursor-pointer font-medium text-foreground">
            <input
              type="checkbox"
              className="w-5 h-5 mr-3 cursor-pointer"
              checked={formState.gdpr_analytics_consent ?? false}
              onChange={() => handleToggle('gdpr_analytics_consent')}
              aria-label="Analytics cookies"
            />
            <span className="text-base font-semibold">Analytics</span>
          </label>
          <p className="mt-2 ml-8 text-muted-foreground text-sm leading-relaxed">
            Help us improve the app by collecting anonymous usage statistics.
          </p>
        </div>

      </section>

      {/* Email Preferences Section */}
      <section className="bg-card border border-border rounded-lg p-6 mb-6">
        <h3 className="text-xl font-semibold text-foreground mt-0 mb-2">Email Preferences</h3>
        <p className="text-muted-foreground mb-6 leading-relaxed">
          Choose which emails you'd like to receive from us.
        </p>

        <div className="mb-6 pb-6 border-b border-muted last:border-b-0 last:mb-0 last:pb-0">
          <label className="flex items-center cursor-pointer font-medium text-foreground">
            <input
              type="checkbox"
              className="w-5 h-5 mr-3 cursor-pointer"
              checked={formState.marketing_emails_enabled ?? false}
              onChange={() => handleToggle('marketing_emails_enabled')}
              aria-label="Marketing emails"
            />
            <span className="text-base font-semibold">Marketing Emails</span>
          </label>
          <p className="mt-2 ml-8 text-muted-foreground text-sm leading-relaxed">
            Receive promotional offers, tips, and feature highlights.
          </p>
        </div>

        <div className="mb-6 pb-6 border-b border-muted last:border-b-0 last:mb-0 last:pb-0">
          <label className="flex items-center cursor-pointer font-medium text-foreground">
            <input
              type="checkbox"
              className="w-5 h-5 mr-3 cursor-pointer"
              checked={formState.product_updates_enabled ?? false}
              onChange={() => handleToggle('product_updates_enabled')}
              aria-label="Product updates"
            />
            <span className="text-base font-semibold">Product Updates</span>
          </label>
          <p className="mt-2 ml-8 text-muted-foreground text-sm leading-relaxed">
            Get notified about new features and improvements.
          </p>
        </div>

        <div className="mb-6 pb-6 border-b border-muted last:border-b-0 last:mb-0 last:pb-0">
          <label className="flex items-center cursor-pointer font-medium text-foreground">
            <input
              type="checkbox"
              className="w-5 h-5 mr-3 cursor-pointer"
              checked={formState.security_alerts_enabled ?? false}
              onChange={() => handleToggle('security_alerts_enabled')}
              aria-label="Security alerts"
            />
            <span className="text-base font-semibold">Security Alerts</span>
          </label>
          <p className="mt-2 ml-8 text-muted-foreground text-sm leading-relaxed">
            Important security notifications and account activity.
          </p>
        </div>
      </section>

      {/* Data Sharing Section */}
      <section className="bg-card border border-border rounded-lg p-6 mb-6">
        <h3 className="text-xl font-semibold text-foreground mt-0 mb-2">Data Sharing</h3>
        <p className="text-muted-foreground mb-6 leading-relaxed">
          Control how your data is shared with third parties.
        </p>

        <div className="mb-6 pb-6 border-b border-muted last:border-b-0 last:mb-0 last:pb-0">
          <label className="flex items-center cursor-pointer font-medium text-foreground">
            <input
              type="checkbox"
              className="w-5 h-5 mr-3 cursor-pointer"
              checked={formState.data_sharing_enabled ?? false}
              onChange={() => handleToggle('data_sharing_enabled')}
              aria-label="Data sharing"
            />
            <span className="text-base font-semibold">Third-Party Data Sharing</span>
          </label>
          <p className="mt-2 ml-8 text-muted-foreground text-sm leading-relaxed">
            Share anonymized data with partners to improve financial services.
          </p>
        </div>
      </section>

      {/* Status Messages */}
      {successMessage && (
        <p className="text-success bg-success/10 px-4 py-3 rounded-md mb-4 font-medium">{successMessage}</p>
      )}
      {error && (
        <p className="text-destructive bg-destructive/10 px-4 py-3 rounded-md mb-4 font-medium">{error}</p>
      )}

      {/* Save Button */}
      <div className="mt-8">
        <button
          onClick={handleSave}
          disabled={submitting}
          className="bg-success text-white border-none px-8 py-3 rounded-md text-base font-semibold cursor-pointer hover:opacity-90 transition-opacity disabled:opacity-60 disabled:cursor-not-allowed"
        >
          {submitting ? 'Saving...' : 'Save Privacy Settings'}
        </button>
      </div>

      {/* Account Deletion Section - Issue #18 */}
      <DeleteAccountSection />
    </div>
  )
}
