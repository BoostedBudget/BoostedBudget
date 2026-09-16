import { useEffect, useMemo, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useUser } from '@clerk/react-router'
import {
  useGetPrivacySettings,
  useUpdatePrivacySettings,
} from '@boostedbudget/api-client/endpoints/privacy/privacy'
import {
  getAnalyticsLifecycleSnapshot,
  grantProductAnalytics,
  withdrawProductAnalytics,
} from '../../shared/lib/posthog'
import type { PrivacySettings as PrivacySettingsType, UpdatePrivacySettingsRequest } from './types'
import { DeleteAccountSection } from '../deletion/components/DeleteAccountSection'
import { DataExportSection } from './components/DataExportSection'
import { ConfirmationDialog } from '../../components/ui/confirmation-dialog'
import { Card } from '../../components/ui/card'
import { privacySettingsQueryKey } from '../../shared/privacy/privacyQueryKey'

interface PrivacySettingsProps {
  onSettingsChange?: (settings: PrivacySettingsType) => void
  analyticsActions?: AnalyticsActions
}

interface AnalyticsActions {
  grant: typeof grantProductAnalytics
  getSnapshot: typeof getAnalyticsLifecycleSnapshot
  withdraw: typeof withdrawProductAnalytics
}

const defaultAnalyticsActions: AnalyticsActions = {
  grant: grantProductAnalytics,
  getSnapshot: getAnalyticsLifecycleSnapshot,
  withdraw: withdrawProductAnalytics,
}

export function PrivacySettings({
  onSettingsChange,
  analyticsActions = defaultAnalyticsActions,
}: PrivacySettingsProps) {
  const [successMessage, setSuccessMessage] = useState<string | null>(null)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [showCriticalConfirm, setShowCriticalConfirm] = useState(false)
  const [analyticsRecoveryNeeded, setAnalyticsRecoveryNeeded] = useState(false)
  const [analyticsRecoveryPending, setAnalyticsRecoveryPending] = useState(false)
  const recoveryAttempt = useRef(0)
  const { user } = useUser()
  const renderedOwner = useRef(user?.id)
  const mutationOwner = useRef<string | undefined>(undefined)
  const [localStateOwner, setLocalStateOwner] = useState(user?.id)
  if (renderedOwner.current !== user?.id) {
    renderedOwner.current = user?.id
    recoveryAttempt.current += 1
  }
  const ownsLocalState = localStateOwner === user?.id
  const queryClient = useQueryClient()
  const privacyQueryKey = privacySettingsQueryKey(user?.id)

  // Use generated query hook for loading settings
  const {
    data: settings,
    isLoading: loading,
    error: queryError,
    refetch,
  } = useGetPrivacySettings({
    query: {
      enabled: Boolean(user?.id),
      queryKey: privacyQueryKey,
    },
  })

  // Use generated mutation hook for updating settings
  const updateMutation = useUpdatePrivacySettings()

  // Track only user modifications (not the full state)
  const [modifications, setModifications] = useState<Partial<PrivacySettingsType>>({})

  useEffect(() => {
    setModifications({})
    setAnalyticsRecoveryNeeded(false)
    setAnalyticsRecoveryPending(false)
    setSaveError(null)
    setSuccessMessage(null)
    setShowCriticalConfirm(false)
    setLocalStateOwner(user?.id)
  }, [user?.id])

  // Derive form state from settings merged with user modifications
  const formState = useMemo<Partial<PrivacySettingsType>>(() => ({
    ...(settings as PrivacySettingsType | undefined),
    ...(ownsLocalState ? modifications : {}),
  }), [settings, modifications, ownsLocalState])

  // Compute loading error message
  let error: string | null = null
  if (queryError) {
    error = 'Failed to load privacy settings. Please try again.'
  } else if (ownsLocalState) {
    error = saveError
  }

  function handleToggle(field: keyof PrivacySettingsType) {
    setModifications(prev => ({
      ...prev,
      [field]: !(formState[field] as boolean),
    }))
    // Clear success message when user makes changes
    setSuccessMessage(null)
  }

  function buildUpdates(): UpdatePrivacySettingsRequest {
    const updates: UpdatePrivacySettingsRequest = {}
    if (!settings) return updates
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
    if (formState.include_in_aggregate_stats !== undefined && formState.include_in_aggregate_stats !== settings.include_in_aggregate_stats) {
      updates.include_in_aggregate_stats = formState.include_in_aggregate_stats
    }
    return updates
  }

  function grantAnalytics(): void {
    if (!user) return
    analyticsActions.grant(user.id, user.id, {
      subscription_tier: user.publicMetadata?.subscription_tier,
      primary_region: user.publicMetadata?.primary_region,
    })
  }

  async function reconcileFailedWithdrawal(): Promise<void> {
    const attempt = ++recoveryAttempt.current
    const authorityGeneration = analyticsActions.getSnapshot().generation
    setAnalyticsRecoveryPending(true)
    setSaveError(null)
    setSuccessMessage(null)
    try {
      const confirmation = await refetch()
      const isCurrentAttempt = attempt === recoveryAttempt.current
      const isSameAuthority = analyticsActions.getSnapshot().generation === authorityGeneration
      const consentConfirmed = confirmation.error == null
        && confirmation.data?.gdpr_analytics_consent === true
      if (!isCurrentAttempt) return
      if (!isSameAuthority || !consentConfirmed) {
        setSaveError('Could not confirm analytics consent. Please try again.')
        return
      }
      grantAnalytics()
      setAnalyticsRecoveryNeeded(false)
      setModifications({})
      setSuccessMessage('Analytics resumed successfully!')
    } catch {
      if (attempt !== recoveryAttempt.current) return
      setSaveError('Could not confirm analytics consent. Please try again.')
    } finally {
      if (attempt === recoveryAttempt.current) setAnalyticsRecoveryPending(false)
    }
  }

  async function performSave() {
    const identityAttempt = recoveryAttempt.current
    const updates = buildUpdates()
    if (Object.keys(updates).length === 0) {
      if (ownsLocalState && analyticsRecoveryNeeded && formState.gdpr_analytics_consent === true) {
        await reconcileFailedWithdrawal()
        return
      }
      setSuccessMessage('No changes to save')
      return
    }
    const withdrawingAnalytics = updates.gdpr_analytics_consent === false
    const grantGeneration = updates.gdpr_analytics_consent === true
      ? analyticsActions.getSnapshot().generation
      : null
    if (withdrawingAnalytics && user) analyticsActions.withdraw(user.id)

    try {
      setSaveError(null)
      setSuccessMessage(null)
      mutationOwner.current = user?.id
      const updatedSettings = await updateMutation.mutateAsync({ data: updates })
      queryClient.setQueryData(privacyQueryKey, updatedSettings)
      void queryClient.invalidateQueries({
        queryKey: privacyQueryKey,
        exact: true,
        refetchType: 'none',
      })
      if (identityAttempt !== recoveryAttempt.current) return
      setModifications({})
      const grantIsCurrent = grantGeneration !== null
        && analyticsActions.getSnapshot().generation === grantGeneration
      if (updates.gdpr_analytics_consent === true && user && grantIsCurrent) {
        grantAnalytics()
        setAnalyticsRecoveryNeeded(false)
      }
      setSuccessMessage('Settings saved successfully!')
      onSettingsChange?.(updatedSettings as PrivacySettingsType)
    } catch (err) {
      console.error('Failed to save privacy settings:', err)
      if (identityAttempt !== recoveryAttempt.current) return
      setSaveError('Failed to save privacy settings. Please try again.')
      if (withdrawingAnalytics) setAnalyticsRecoveryNeeded(true)
    }
  }

  function handleSave() {
    if (!settings) return
    const updates = buildUpdates()
    // Issue #960: Warn before disabling security alerts
    if (updates.security_alerts_enabled === false) {
      setShowCriticalConfirm(true)
      return
    }
    void performSave()
  }

  // Derive submitting state from mutation
  const submitting = (mutationOwner.current === user?.id && updateMutation.isPending)
    || (ownsLocalState && analyticsRecoveryPending)

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
      <Card className="mb-6">
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
            Help us improve the app with pseudonymous usage statistics when you choose analytics.
          </p>
        </div>

      </Card>

      {/* Email Preferences Section */}
      <Card className="mb-6">
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
      </Card>

      {/* Data Sharing Section */}
      <Card className="mb-6">
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

        <div className="mb-6 pb-6 border-b border-muted last:border-b-0 last:mb-0 last:pb-0">
          <label className="flex items-center cursor-pointer font-medium text-foreground">
            <input
              type="checkbox"
              className="w-5 h-5 mr-3 cursor-pointer"
              checked={formState.include_in_aggregate_stats ?? false}
              onChange={() => handleToggle('include_in_aggregate_stats')}
              aria-label="Include my transfers in aggregate statistics"
            />
            <span className="text-base font-semibold">Include My Transfers in Aggregate Statistics</span>
          </label>
          <p className="mt-2 ml-8 text-muted-foreground text-sm leading-relaxed">
            Your transfer totals (anonymized) help us show social proof on our public page. Opt out at any time.
          </p>
        </div>
      </Card>

      {/* Status Messages */}
      {ownsLocalState && successMessage && (
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

      {/* Data Export Section - Issue #1587 (GDPR Article 20).
          Placed immediately BEFORE deletion, deliberately: a user who has come here to erase their
          account should be offered the copy of their history before the button that ends it. */}
      <DataExportSection />

      {/* Account Deletion Section - Issue #18 */}
      <DeleteAccountSection />

      {/* Security alerts disable confirmation — Issue #960 */}
      <ConfirmationDialog
        open={showCriticalConfirm}
        onOpenChange={(open) => !open && setShowCriticalConfirm(false)}
        onConfirm={() => {
          setShowCriticalConfirm(false)
          void performSave()
        }}
        title="Disable Security Alerts?"
        description="You are about to disable security alerts. This means you won't receive notifications about unusual account activity or security issues."
        variant="warning"
        confirmButtonText="Confirm"
        loading={submitting}
      />
    </div>
  )
}
