import { Mail, CheckCircle2, Globe, Shield, Copy } from 'lucide-react'
import { SubscriptionCard } from './components/SubscriptionCard'
import { ThemeSelector } from './components/ThemeSelector'
import { ProfileForm } from './ProfileForm'
import { PrivacySettings } from './PrivacySettings'
import { useProfile } from './hooks/useProfile'
import { useIsAdmin, useAdminMode, AdminSettingsSection } from '@/features/admin'
import { useRegions } from '../../shared/hooks/useRegions'
import { useTabState, useCopyToClipboard } from '../../shared/hooks'
import { abbreviateUUID } from '../../shared/utils'
import type { User, PrivacySettings as PrivacySettingsType } from './types'
import './ProfilePage.css'

interface ProfilePageProps {
  onPrivacySettingsChange?: (settings: PrivacySettingsType) => void
}

export function ProfilePage({ onPrivacySettingsChange }: ProfilePageProps) {
  const { user, loading, error, refetch } = useProfile()
  const { isAdmin, loading: adminLoading } = useIsAdmin()
  const { adminModeEnabled, toggleAdminMode } = useAdminMode()
  const [activeTab, setActiveTab] = useTabState('tab', 'profile', ['profile', 'privacy'] as const)
  const { copy, copied } = useCopyToClipboard()
  const { getRegionWithFlag } = useRegions()

  function handleProfileUpdate(_updatedUser: User) {
    // Refetch to get the updated user data from server
    refetch()
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
          <p className="error-message">{error.message || 'Failed to load profile. Please try again.'}</p>
          <button onClick={() => refetch()} className="btn-retry">
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
          <button onClick={() => refetch()} className="btn-retry">
            Retry
          </button>
        </div>
      </div>
    )
  }

  // Get region display name with flag emoji
  const regionDisplay = getRegionWithFlag(user.primary_region)

  return (
    <div className="profile-page responsive">
      <h1>My Profile</h1>

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

      {activeTab === 'profile' && (
        <>
          <section className="profile-section">
            <h2>Account Information</h2>
            <div className="info-grid">
              <div className="info-item">
                <div className="info-item-header">
                  <Mail className="info-icon" aria-hidden="true" />
                  <span className="info-label">Email</span>
                </div>
                <div className="info-item-value">
                  <span>{user.email}</span>
                  {user.email_verified && (
                    <span title="Email verified">
                      <CheckCircle2
                        className="verified-icon"
                        aria-label="Verified"
                      />
                    </span>
                  )}
                </div>
              </div>
              <div className="info-item">
                <div className="info-item-header">
                  <Globe className="info-icon" aria-hidden="true" />
                  <span className="info-label">Data Region</span>
                </div>
                <span className="info-item-value">{regionDisplay}</span>
              </div>
              <div className="info-item">
                <div className="info-item-header">
                  <CheckCircle2 className="info-icon" aria-hidden="true" />
                  <span className="info-label">Account Status</span>
                </div>
                <span className="info-item-value">
                  {user.is_active ? 'Active' : 'Inactive'}
                </span>
              </div>
              <div className="info-item">
                <div className="info-item-header">
                  <Shield className="info-icon" aria-hidden="true" />
                  <span className="info-label">User ID</span>
                </div>
                <div className="info-item-value user-id-row">
                  <span className="user-id">{abbreviateUUID(user.id)}</span>
                  <button
                    onClick={() => copy(user.id, 'User ID copied!')}
                    className="copy-button"
                    aria-label="Copy User ID to clipboard"
                    title={copied ? 'Copied!' : 'Copy User ID'}
                  >
                    <Copy className="copy-icon" aria-hidden="true" />
                  </button>
                </div>
              </div>
            </div>
          </section>

          <section className="profile-section">
            <h2>Subscription</h2>
            <SubscriptionCard
              tier={user.effective_tier ?? user.subscription_tier}
              status={user.subscription_status}
              trialDaysRemaining={user.trial_days_remaining}
            />
          </section>

          <section className="profile-section">
            <div className="bg-card border border-border rounded-lg p-6">
              <h2 className="text-base font-semibold text-foreground mb-1">Appearance</h2>
              <p className="text-sm text-muted-foreground mb-4">Choose how BoostedBudget looks to you.</p>
              <ThemeSelector />
            </div>
          </section>

          <ProfileForm user={user} onProfileUpdate={handleProfileUpdate} />

          {!adminLoading && isAdmin && (
            <AdminSettingsSection
              adminModeEnabled={adminModeEnabled}
              onToggle={toggleAdminMode}
            />
          )}
        </>
      )}

      {activeTab === 'privacy' && <PrivacySettings onSettingsChange={onPrivacySettingsChange} />}
    </div>
  )
}
