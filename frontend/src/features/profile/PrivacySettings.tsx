import { useState, useEffect } from 'react'
import { fetchPrivacySettings, updatePrivacySettings } from './api/privacyApi'
import type { PrivacySettings as PrivacySettingsType, UpdatePrivacySettingsRequest } from './types'
import './PrivacySettings.css'

interface PrivacySettingsProps {
  onSettingsChange?: (settings: PrivacySettingsType) => void
}

export function PrivacySettings({ onSettingsChange }: PrivacySettingsProps) {
  const [settings, setSettings] = useState<PrivacySettingsType | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [successMessage, setSuccessMessage] = useState<string | null>(null)

  // Local form state (modified values)
  const [formState, setFormState] = useState<Partial<PrivacySettingsType>>({})

  // Load privacy settings on mount
  useEffect(() => {
    loadSettings()
  }, [])

  async function loadSettings() {
    try {
      setLoading(true)
      setError(null)
      const data = await fetchPrivacySettings()
      setSettings(data)
      // Initialize form state with loaded settings
      setFormState(data)
    } catch (err) {
      console.error('Failed to load privacy settings:', err)
      setError('Failed to load privacy settings. Please try again.')
    } finally {
      setLoading(false)
    }
  }

  function handleToggle(field: keyof PrivacySettingsType) {
    setFormState(prev => ({
      ...prev,
      [field]: !(prev[field] as boolean),
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
    if (formState.gdpr_advertising_consent !== undefined && formState.gdpr_advertising_consent !== settings.gdpr_advertising_consent) {
      updates.gdpr_advertising_consent = formState.gdpr_advertising_consent
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
      setSubmitting(true)
      setError(null)
      setSuccessMessage(null)

      const updatedSettings = await updatePrivacySettings(updates)

      setSettings(updatedSettings)
      setFormState(updatedSettings)
      setSuccessMessage('Settings saved successfully!')

      // Notify parent component
      onSettingsChange?.(updatedSettings)
    } catch (err) {
      console.error('Failed to save privacy settings:', err)
      setError('Failed to save privacy settings. Please try again.')
    } finally {
      setSubmitting(false)
    }
  }

  if (loading) {
    return (
      <div className="privacy-settings">
        <p>Loading privacy settings...</p>
      </div>
    )
  }

  if (error && !settings) {
    return (
      <div className="privacy-settings">
        <p className="error">{error}</p>
        <button onClick={loadSettings}>Retry</button>
      </div>
    )
  }

  if (!settings) {
    return null
  }

  return (
    <div className="privacy-settings">
      <h2>Privacy Settings</h2>

      {/* GDPR Cookie Consent Section */}
      <section className="privacy-section">
        <h3>GDPR Cookie Consent</h3>
        <p className="section-description">
          Manage your cookie preferences. Necessary cookies are required for the app to function.
        </p>

        <div className="privacy-option">
          <label>
            <input
              type="checkbox"
              checked={formState.gdpr_necessary_consent ?? false}
              disabled={true}
              aria-label="Necessary cookies"
            />
            <span className="option-label">Necessary</span>
          </label>
          <p className="option-description">
            Required for basic app functionality. Cannot be disabled.
          </p>
        </div>

        <div className="privacy-option">
          <label>
            <input
              type="checkbox"
              checked={formState.gdpr_analytics_consent ?? false}
              onChange={() => handleToggle('gdpr_analytics_consent')}
              aria-label="Analytics cookies"
            />
            <span className="option-label">Analytics</span>
          </label>
          <p className="option-description">
            Help us improve the app by collecting anonymous usage statistics.
          </p>
        </div>

        <div className="privacy-option">
          <label>
            <input
              type="checkbox"
              checked={formState.gdpr_advertising_consent ?? false}
              onChange={() => handleToggle('gdpr_advertising_consent')}
              aria-label="Advertising cookies"
            />
            <span className="option-label">Advertising</span>
          </label>
          <p className="option-description">
            Show personalized ads (free tier only). Supports free version of the app.
          </p>
        </div>
      </section>

      {/* Email Preferences Section */}
      <section className="privacy-section">
        <h3>Email Preferences</h3>
        <p className="section-description">
          Choose which emails you'd like to receive from us.
        </p>

        <div className="privacy-option">
          <label>
            <input
              type="checkbox"
              checked={formState.marketing_emails_enabled ?? false}
              onChange={() => handleToggle('marketing_emails_enabled')}
              aria-label="Marketing emails"
            />
            <span className="option-label">Marketing Emails</span>
          </label>
          <p className="option-description">
            Receive promotional offers, tips, and feature highlights.
          </p>
        </div>

        <div className="privacy-option">
          <label>
            <input
              type="checkbox"
              checked={formState.product_updates_enabled ?? false}
              onChange={() => handleToggle('product_updates_enabled')}
              aria-label="Product updates"
            />
            <span className="option-label">Product Updates</span>
          </label>
          <p className="option-description">
            Get notified about new features and improvements.
          </p>
        </div>

        <div className="privacy-option">
          <label>
            <input
              type="checkbox"
              checked={formState.security_alerts_enabled ?? false}
              onChange={() => handleToggle('security_alerts_enabled')}
              aria-label="Security alerts"
            />
            <span className="option-label">Security Alerts</span>
          </label>
          <p className="option-description">
            Important security notifications and account activity.
          </p>
        </div>
      </section>

      {/* Data Sharing Section */}
      <section className="privacy-section">
        <h3>Data Sharing</h3>
        <p className="section-description">
          Control how your data is shared with third parties.
        </p>

        <div className="privacy-option">
          <label>
            <input
              type="checkbox"
              checked={formState.data_sharing_enabled ?? false}
              onChange={() => handleToggle('data_sharing_enabled')}
              aria-label="Data sharing"
            />
            <span className="option-label">Third-Party Data Sharing</span>
          </label>
          <p className="option-description">
            Share anonymized data with partners to improve financial services.
          </p>
        </div>
      </section>

      {/* Status Messages */}
      {successMessage && (
        <p className="success-message">{successMessage}</p>
      )}
      {error && (
        <p className="error-message">{error}</p>
      )}

      {/* Save Button */}
      <div className="privacy-actions">
        <button
          onClick={handleSave}
          disabled={submitting}
          className="btn-save"
        >
          {submitting ? 'Saving...' : 'Save Privacy Settings'}
        </button>
      </div>
    </div>
  )
}
