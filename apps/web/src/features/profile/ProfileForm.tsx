import { useState } from 'react'
import { toast } from 'sonner'
import { useUpdateUserProfile } from '@boostedbudget/api-client/endpoints/profile/profile'
import { useTrackEvent } from '../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../shared/analytics'
import { useRegions } from '../../shared/hooks/useRegions'
import { ConfirmationDialog } from '../../components/ui/confirmation-dialog'
import { Button } from '../../components/ui/button'
import { AlertCircle } from 'lucide-react'
import { posthog } from '../../shared/lib/posthog'
import type { User, UpdateProfileRequest } from './types'

interface ProfileFormProps {
  user: User
  onProfileUpdate?: (user: User) => void
}

export function ProfileForm({ user, onProfileUpdate }: ProfileFormProps) {
  const [validationError, setValidationError] = useState<string | null>(null)
  const [showRegionConfirm, setShowRegionConfirm] = useState(false)
  const trackEvent = useTrackEvent()
  const { regions, loading: regionsLoading } = useRegions()
  const updateProfileMutation = useUpdateUserProfile()
  const submitting = updateProfileMutation.isPending

  const [email, setEmail] = useState(user.email)
  const [primaryRegion, setPrimaryRegion] = useState(user.primary_region)

  const hasChanges = email !== user.email || primaryRegion !== user.primary_region
  const hasRegionChange = primaryRegion !== user.primary_region

  function getRegionDisplay(code: string): string {
    return regions?.[code]?.display_name ?? code
  }

  function validateEmail(emailValue: string): boolean {
    if (!emailValue || emailValue.trim() === '') {
      setValidationError('Email is required')
      return false
    }
    const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/
    if (!emailRegex.test(emailValue)) {
      setValidationError('Invalid email address')
      return false
    }
    setValidationError(null)
    return true
  }

  async function performSave() {
    const updateData: UpdateProfileRequest = {}
    const emailChanging = email !== user.email
    const regionChanging = primaryRegion !== user.primary_region

    if (emailChanging) {
      updateData.email = email
    }
    if (regionChanging) {
      updateData.primary_region = primaryRegion
    }

    updateProfileMutation.mutate(
      { data: updateData },
      {
        onSuccess: (updatedUser) => {
          setEmail(updatedUser.email)
          setPrimaryRegion(updatedUser.primary_region)

          if (emailChanging) {
            toast.success(
              'Profile updated. Check your inbox to verify your new email address.',
            )
            // Re-identify in PostHog with new email to prevent analytics identity split
            posthog.identify(updatedUser.id, { email: updatedUser.email })
          } else {
            toast.success('Profile updated successfully!')
          }

          trackEvent(ANALYTICS_EVENTS.PROFILE_UPDATED)
          onProfileUpdate?.(updatedUser)
          setShowRegionConfirm(false)
        },
        onError: (err) => {
          console.error('Failed to update profile:', err)
          // TODO: Replace with typed status check (e.g., err?.status === 409) once
          // the API client exposes structured HTTP errors (currently embeds status in message string)
          const message =
            err?.message?.includes('409') || err?.message?.includes('already in use')
              ? 'That email address is already in use.'
              : 'Failed to update profile. Please try again.'
          toast.error(message)
          setShowRegionConfirm(false)
        },
      },
    )
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!validateEmail(email)) return
    setValidationError(null)
    if (!hasChanges) return
    if (hasRegionChange) {
      setShowRegionConfirm(true)
      return
    }
    await performSave()
  }

  return (
    <section className="bg-card border border-border rounded-lg p-6 mb-8">
      <h2 className="text-2xl font-semibold text-foreground mt-0 mb-6 border-b-2 border-muted pb-2">
        Update Profile
      </h2>

      {/* Email verification pending notice */}
      {!user.email_verified && (
        <div className="flex items-start gap-3 p-4 mb-6 bg-warning/10 border border-warning/30 rounded-lg">
          <AlertCircle className="w-5 h-5 text-warning shrink-0 mt-0.5" aria-hidden="true" />
          <div>
            <p className="text-sm font-medium text-foreground">Verify your email</p>
            <p className="text-sm text-muted-foreground mt-0.5">
              Check your inbox at{' '}
              <span data-ph-mask className="font-medium">
                {user.email}
              </span>{' '}
              and click the verification link to confirm your email address.
              You&apos;ll continue signing in with your previous email until the new one is verified.
            </p>
          </div>
        </div>
      )}

      <form onSubmit={handleSubmit} noValidate className="space-y-6">
        {validationError && (
          <div
            role="alert"
            className="flex items-center gap-2 p-3 text-sm text-destructive bg-destructive/10 border border-destructive/30 rounded-md"
          >
            <AlertCircle className="w-4 h-4 shrink-0" aria-hidden="true" />
            {validationError}
          </div>
        )}

        <div className="space-y-2">
          <label htmlFor="email" className="text-sm font-medium text-foreground block">
            Email
          </label>
          <input
            type="email"
            id="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            disabled={submitting}
            className="w-full bg-background border border-input rounded-md px-3 py-2 text-base text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring focus:border-transparent disabled:opacity-60 disabled:cursor-not-allowed"
            data-ph-mask
          />
          {email !== user.email && (
            <p className="text-xs text-muted-foreground">
              You will receive a verification email at the new address.
            </p>
          )}
        </div>

        <div className="space-y-2">
          <label htmlFor="region" className="text-sm font-medium text-foreground block">
            Primary Region
          </label>
          <select
            id="region"
            value={primaryRegion}
            onChange={(e) => setPrimaryRegion(e.target.value)}
            disabled={submitting || regionsLoading}
            className="w-full bg-background border border-input rounded-md px-3 py-2 text-base text-foreground focus:outline-none focus:ring-2 focus:ring-ring focus:border-transparent disabled:opacity-60 disabled:cursor-not-allowed"
          >
            {regions &&
              Object.entries(regions).map(([code, info]) => (
                <option key={code} value={code}>
                  {info.flag} {info.display_name}
                </option>
              ))}
          </select>
          <p className="text-sm text-muted-foreground">
            Select where your financial data is stored
          </p>
        </div>

        <Button
          type="submit"
          disabled={submitting || !hasChanges}
          className="w-full sm:w-auto"
        >
          {submitting ? 'Saving...' : 'Save Changes'}
        </Button>
      </form>

      {/* Region Change Confirmation Dialog — Issue #960 */}
      <ConfirmationDialog
        open={showRegionConfirm}
        onOpenChange={(open) => !open && setShowRegionConfirm(false)}
        onConfirm={() => performSave()}
        title="Change Data Region?"
        description={
          <>
            You&apos;re changing your data region from{' '}
            <strong>{getRegionDisplay(user.primary_region)}</strong> to{' '}
            <strong>{getRegionDisplay(primaryRegion)}</strong>.
          </>
        }
        variant="info"
        confirmButtonText="Confirm Change"
        loading={submitting}
      />

    </section>
  )
}
