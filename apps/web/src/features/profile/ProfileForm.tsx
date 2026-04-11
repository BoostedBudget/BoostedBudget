import { useState } from 'react'
import { toast } from 'sonner'
import { useUpdateUserProfile } from '@boostedbudget/api-client/endpoints/profile/profile'
import { useTrackEvent } from '../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../shared/analytics'
import { useRegions } from '../../shared/hooks/useRegions'
import { ConfirmationDialog } from '../../components/ui/confirmation-dialog'
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

  // Form state - initialized from user prop
  // Note: If user prop can change (e.g., after profile update elsewhere),
  // add key={user.id} at the parent component to force remount
  const [email, setEmail] = useState(user.email)
  const [primaryRegion, setPrimaryRegion] = useState(user.primary_region)

  // Check if form has changes
  const hasChanges =
    email !== user.email || primaryRegion !== user.primary_region
  const hasRegionChange = primaryRegion !== user.primary_region

  function getRegionDisplay(code: string): string {
    return regions?.[code]?.display_name ?? code
  }

  function validateEmail(emailValue: string): boolean {
    if (!emailValue || emailValue.trim() === '') {
      setValidationError('Email is required')
      return false
    }

    // Basic email validation regex
    const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/
    if (!emailRegex.test(emailValue)) {
      setValidationError('Invalid email address')
      return false
    }

    setValidationError(null)
    return true
  }

  async function performSave() {
    // Build update data - only send changed fields
    const updateData: UpdateProfileRequest = {}

    if (email !== user.email) {
      updateData.email = email
    }

    if (primaryRegion !== user.primary_region) {
      // When updating region, include email as well
      updateData.email = email
      updateData.primary_region = primaryRegion
    }

    updateProfileMutation.mutate(
      { data: updateData },
      {
        onSuccess: (updatedUser) => {
          // Update form state with saved values
          setEmail(updatedUser.email)
          setPrimaryRegion(updatedUser.primary_region)

          // Show success toast
          toast.success('Profile updated successfully!')

          // Track profile update
          trackEvent(ANALYTICS_EVENTS.PROFILE_UPDATED)

          // Notify parent component
          onProfileUpdate?.(updatedUser)
          setShowRegionConfirm(false)
        },
        onError: (err) => {
          console.error('Failed to update profile:', err)
          toast.error('Failed to update profile. Please try again.')
          setShowRegionConfirm(false)
        },
      }
    )
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()

    // Validate email
    if (!validateEmail(email)) {
      return
    }

    // Clear any previous validation errors
    setValidationError(null)

    // If no changes, don't submit
    if (!hasChanges) {
      return
    }

    // If region is changing, show confirmation dialog
    if (hasRegionChange) {
      setShowRegionConfirm(true)
      return
    }

    // No region change, proceed with save
    await performSave()
  }

  function handleConfirmRegionChange() {
    performSave()
  }

  return (
    <section className="profile-section">
      <h2>Update Profile</h2>
      <form onSubmit={handleSubmit} className="profile-form">
        {validationError && (
          <div className="error-message">{validationError}</div>
        )}

        <div className="form-group">
          <label htmlFor="email">Email</label>
          <input
            type="text"
            id="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            disabled={submitting}
          />
        </div>

        <div className="form-group">
          <label htmlFor="region">Primary Region</label>
          <select
            id="region"
            value={primaryRegion}
            onChange={(e) => setPrimaryRegion(e.target.value)}
            disabled={submitting || regionsLoading}
          >
            {regions && Object.entries(regions).map(([code, info]) => (
              <option key={code} value={code}>
                {info.flag} {info.display_name}
              </option>
            ))}
          </select>
          <p className="helper-text">
            Select where your financial data is stored
          </p>
        </div>

        <button
          type="submit"
          disabled={submitting || !hasChanges}
          className="btn-save"
        >
          {submitting ? 'Saving...' : 'Save Changes'}
        </button>
      </form>

      {/* Region Change Confirmation Dialog — Issue #960 */}
      <ConfirmationDialog
        open={showRegionConfirm}
        onOpenChange={(open) => !open && setShowRegionConfirm(false)}
        onConfirm={handleConfirmRegionChange}
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
