import { useState, useEffect } from 'react'
import { toast } from 'sonner'
import { updateProfile } from './api/profileApi'
import { REGIONS, getRegionWithFlag } from '../../config/regions'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../../components/ui/dialog'
import { Button } from '../../components/ui/button'
import type { User, UpdateProfileRequest } from './types'

interface ProfileFormProps {
  user: User
  onProfileUpdate?: (user: User) => void
}

export function ProfileForm({ user, onProfileUpdate }: ProfileFormProps) {
  const [submitting, setSubmitting] = useState(false)
  const [validationError, setValidationError] = useState<string | null>(null)
  const [showRegionConfirm, setShowRegionConfirm] = useState(false)

  // Form state
  const [email, setEmail] = useState(user.email)
  const [primaryRegion, setPrimaryRegion] = useState(user.primary_region)

  // Update form state when user prop changes
  useEffect(() => {
    setEmail(user.email)
    setPrimaryRegion(user.primary_region)
  }, [user.email, user.primary_region])

  // Check if form has changes
  const hasChanges =
    email !== user.email || primaryRegion !== user.primary_region
  const hasRegionChange = primaryRegion !== user.primary_region

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
    try {
      setSubmitting(true)

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

      const updatedUser = await updateProfile(updateData)

      // Update form state with saved values
      setEmail(updatedUser.email)
      setPrimaryRegion(updatedUser.primary_region)

      // Show success toast
      toast.success('Profile updated successfully!')

      // Notify parent component
      onProfileUpdate?.(updatedUser)
    } catch (err) {
      console.error('Failed to update profile:', err)
      toast.error('Failed to update profile. Please try again.')
    } finally {
      setSubmitting(false)
      setShowRegionConfirm(false)
    }
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
            disabled={submitting}
          >
            {Object.entries(REGIONS).map(([code, info]) => (
              <option key={code} value={code}>
                {info.flag} {info.displayName}
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

      {/* Region Change Confirmation Dialog */}
      <Dialog open={showRegionConfirm} onOpenChange={setShowRegionConfirm}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Change Data Region?</DialogTitle>
            <DialogDescription>
              You&apos;re changing your data region from{' '}
              <strong>{getRegionWithFlag(user.primary_region)}</strong> to{' '}
              <strong>{getRegionWithFlag(primaryRegion)}</strong>.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setShowRegionConfirm(false)}
              disabled={submitting}
            >
              Cancel
            </Button>
            <Button onClick={handleConfirmRegionChange} disabled={submitting}>
              {submitting ? 'Saving...' : 'Confirm Change'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  )
}
