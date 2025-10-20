import { useState, useEffect } from 'react'
import { updateProfile } from './api/profileApi'
import { VALID_REGIONS } from './types'
import type { User, UpdateProfileRequest } from './types'

interface ProfileFormProps {
  user: User
  onProfileUpdate?: (user: User) => void
}

export function ProfileForm({ user, onProfileUpdate }: ProfileFormProps) {
  const [submitting, setSubmitting] = useState(false)
  const [successMessage, setSuccessMessage] = useState<string | null>(null)
  const [validationError, setValidationError] = useState<string | null>(null)

  // Form state
  const [email, setEmail] = useState(user.email)
  const [primaryRegion, setPrimaryRegion] = useState(user.primary_region)

  // Update form state when user prop changes
  useEffect(() => {
    setEmail(user.email)
    setPrimaryRegion(user.primary_region)
  }, [user.email, user.primary_region])

  function validateEmail(email: string): boolean {
    if (!email || email.trim() === '') {
      setValidationError('Email is required')
      return false
    }

    // Basic email validation regex
    const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/
    if (!emailRegex.test(email)) {
      setValidationError('Invalid email address')
      return false
    }

    setValidationError(null)
    return true
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()

    // Validate email
    if (!validateEmail(email)) {
      return
    }

    // Clear any previous messages
    setValidationError(null)
    setSuccessMessage(null)

    try {
      setSubmitting(true)

      // Build update data - only send changed fields
      const updateData: UpdateProfileRequest = {}
      let hasChanges = false

      if (email !== user.email) {
        updateData.email = email
        hasChanges = true
      }

      if (primaryRegion !== user.primary_region) {
        // When updating region, include email as well
        updateData.email = email
        updateData.primary_region = primaryRegion
        hasChanges = true
      }

      // If no changes detected, don't submit
      if (!hasChanges) {
        return
      }

      const updatedUser = await updateProfile(updateData)

      // Update form state with saved values
      setEmail(updatedUser.email)
      setPrimaryRegion(updatedUser.primary_region)
      setSuccessMessage('Profile updated successfully!')

      // Notify parent component
      onProfileUpdate?.(updatedUser)

      // Clear success message after 3 seconds
      setTimeout(() => setSuccessMessage(null), 3000)
    } catch (err) {
      console.error('Failed to update profile:', err)
      setValidationError('Failed to update profile. Please try again.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <section className="profile-section">
      <h2>Update Profile</h2>
      <form onSubmit={handleSubmit} className="profile-form">
        {successMessage && <div className="success-message">{successMessage}</div>}
        {validationError && <div className="error-message">{validationError}</div>}

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
            {VALID_REGIONS.map((region) => (
              <option key={region} value={region}>
                {region}
              </option>
            ))}
          </select>
        </div>

        <button type="submit" disabled={submitting} className="btn-save">
          {submitting ? 'Saving...' : 'Save Changes'}
        </button>
      </form>
    </section>
  )
}
