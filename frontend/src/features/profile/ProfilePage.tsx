import { useState, useEffect } from 'react'
import { SubscriptionCard } from './components/SubscriptionCard'
import { ProfileForm } from './ProfileForm'
import { PrivacySettings } from './PrivacySettings'
import { fetchProfile } from './api/profileApi'
import { getRegionDisplay } from '../../config/regions'
import type { User } from './types'
import './ProfilePage.css'

type TabType = 'profile' | 'privacy'

export function ProfilePage() {
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [activeTab, setActiveTab] = useState<TabType>('profile')

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
    } catch (err) {
      console.error('Failed to load profile:', err)
      setError('Failed to load profile. Please try again.')
    } finally {
      setLoading(false)
    }
  }

  function handleProfileUpdate(updatedUser: User) {
    setUser(updatedUser)
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
  const regionDisplay = getRegionDisplay(user.primary_region)

  return (
    <div className="profile-page responsive">
      <h1>My Profile</h1>

      {/* Tab Navigation */}
      <div className="profile-tabs">
        <button
          className={`tab-button ${activeTab === 'profile' ? 'active' : ''}`}
          onClick={() => setActiveTab('profile')}
        >
          Profile
        </button>
        <button
          className={`tab-button ${activeTab === 'privacy' ? 'active' : ''}`}
          onClick={() => setActiveTab('privacy')}
        >
          Privacy Settings
        </button>
      </div>

      {/* Profile Tab Content */}
      {activeTab === 'profile' && (
        <>
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
          <ProfileForm user={user} onProfileUpdate={handleProfileUpdate} />
        </>
      )}

      {/* Privacy Settings Tab Content */}
      {activeTab === 'privacy' && <PrivacySettings />}
    </div>
  )
}
