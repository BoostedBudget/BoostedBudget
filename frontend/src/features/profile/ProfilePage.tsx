import { useState, useEffect } from 'react'
import { SubscriptionCard } from './components/SubscriptionCard'
import { fetchProfile, updateProfile } from './api/profileApi'
import { VALID_REGIONS } from './types'
import type { User, UpdateProfileRequest } from './types'
import './ProfilePage.css'

// Region name mapping for proper geographic display
const REGION_NAMES: Record<string, string> = {
  'us-east-1': 'USA (Virginia)',
  'eu-west-1': 'Europe (Ireland)',
  'ap-southeast-1': 'Asia Pacific (Singapore)',
}

export function ProfilePage() {
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [successMessage, setSuccessMessage] = useState<string | null>(null)

  // Form state
  const [email, setEmail] = useState('')
  const [primaryRegion, setPrimaryRegion] = useState('')
  const [validationError, setValidationError] = useState<string | null>(null)

  // Load user profile on mount
  useEffect(() => {
    loadProfile()
  }, [])

  async function loadProfile() {
    try {
      setLoading(true)
      setError(null)
      const profileData = await fetchProfile()
      setUser(profileData)
      setEmail(profileData.email)
      setPrimaryRegion(profileData.primary_region)
    } catch (err) {
      console.error('Failed to load profile:', err)
      setError('Failed to load profile. Please try again.')
    } finally {
      setLoading(false)
    }
  }

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

    // Validate email - this sets validation error if invalid
    if (!validateEmail(email)) {
      return
    }

    // Clear any previous messages since validation passed
    setValidationError(null)
    setSuccessMessage(null)

    try {
      setSubmitting(true)

      // Build update data - only send changed fields
      const updateData: UpdateProfileRequest = {}
      let hasChanges = false

      if (user && email !== user.email) {
        updateData.email = email
        hasChanges = true
      }

      if (user && primaryRegion !== user.primary_region) {
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
      setUser(updatedUser)
      setEmail(updatedUser.email)
      setPrimaryRegion(updatedUser.primary_region)
      setSuccessMessage('Profile updated successfully!')

      // Clear success message after 3 seconds
      setTimeout(() => setSuccessMessage(null), 3000)
    } catch (err) {
      console.error('Failed to update profile:', err)
      setValidationError('Failed to update profile. Please try again.')
    } finally {
      setSubmitting(false)
    }
  }

  if (loading) {
    return (
      <div className="profile-page responsive">
        <div className="loading">Loading your profile...</div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="profile-page responsive">
        <div className="error-state">
          <p className="error-message">{error}</p>
          <button onClick={loadProfile} className="btn-retry">
            Retry
          </button>
        </div>
      </div>
    )
  }

  // If we get here, user should exist (TypeScript knows this from the loading/error guards above)
  // But add a safety check just in case
  if (!user) {
    return (
      <div className="profile-page responsive">
        <div className="error-state">
          <p className="error-message">Failed to load profile data</p>
          <button onClick={loadProfile} className="btn-retry">
            Retry
          </button>
        </div>
      </div>
    )
  }

  // Get region display name with proper geographic labels
  const regionDisplay = REGION_NAMES[user.primary_region] || user.primary_region

  return (
    <div className="profile-page responsive">
      <h1>My Profile</h1>

      {/* User Info Section */}
      <section className="profile-section">
        <h2>Account Information</h2>
        <div className="info-grid">
          <div className="info-item">
            <label>User ID:</label>
            <span>{user.id}</span>
          </div>
          <div className="info-item">
            <label>Email:</label>
            <span>{user.email}</span>
          </div>
          <div className="info-item">
            <label>Email Verified:</label>
            <span>{user.email_verified ? '✅ Yes' : '❌ No'}</span>
          </div>
          <div className="info-item">
            <label>Data Region:</label>
            <span>{regionDisplay}</span>
          </div>
          <div className="info-item">
            <label>Account Status:</label>
            <span>{user.is_active ? '✅ Active' : '❌ Inactive'}</span>
          </div>
        </div>
      </section>

      {/* Subscription Section */}
      <section className="profile-section">
        <h2>Subscription</h2>
        <SubscriptionCard tier={user.subscription_tier} status={user.subscription_status} />
      </section>

      {/* Edit Profile Form */}
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
    </div>
  )
}
