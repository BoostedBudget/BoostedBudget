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
      <div className="max-w-4xl mx-auto px-4 sm:px-8 py-8">
        <div className="text-center py-12 text-lg text-muted-foreground">Loading your profile...</div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="max-w-4xl mx-auto px-4 sm:px-8 py-8">
        <div className="text-center py-12">
          <p className="text-destructive bg-destructive/10 px-4 py-3 rounded-md mb-4">{error.message || 'Failed to load profile. Please try again.'}</p>
          <button
            onClick={() => refetch()}
            className="bg-primary text-primary-foreground border-none px-6 py-3 rounded-md text-base cursor-pointer hover:opacity-90 transition-opacity"
          >
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
      <div className="max-w-4xl mx-auto px-4 sm:px-8 py-8">
        <div className="text-center py-12">
          <p className="text-destructive bg-destructive/10 px-4 py-3 rounded-md mb-4">Failed to load profile data</p>
          <button
            onClick={() => refetch()}
            className="bg-primary text-primary-foreground border-none px-6 py-3 rounded-md text-base cursor-pointer hover:opacity-90 transition-opacity"
          >
            Retry
          </button>
        </div>
      </div>
    )
  }

  // Get region display name with flag emoji
  const regionDisplay = getRegionWithFlag(user.primary_region)

  return (
    <div className="max-w-4xl mx-auto px-4 sm:px-8 py-8">
      <h1 className="text-3xl font-bold text-foreground mb-6">My Profile</h1>

      <div className="flex gap-2 mb-8 border-b-2 border-border">
        <button
          className={`bg-transparent border-none border-b-[3px] px-6 py-3 text-base font-medium cursor-pointer transition-all duration-200 -mb-[2px] ${
            activeTab === 'profile'
              ? 'text-primary border-b-primary font-semibold'
              : 'text-muted-foreground border-b-transparent hover:text-foreground hover:bg-muted'
          }`}
          onClick={() => setActiveTab('profile')}
        >
          Profile
        </button>
        <button
          className={`bg-transparent border-none border-b-[3px] px-6 py-3 text-base font-medium cursor-pointer transition-all duration-200 -mb-[2px] ${
            activeTab === 'privacy'
              ? 'text-primary border-b-primary font-semibold'
              : 'text-muted-foreground border-b-transparent hover:text-foreground hover:bg-muted'
          }`}
          onClick={() => setActiveTab('privacy')}
        >
          Privacy Settings
        </button>
      </div>

      {activeTab === 'profile' && (
        <>
          <section className="bg-card border border-border rounded-lg p-6 mb-8">
            <h2 className="text-2xl font-semibold text-foreground mt-0 mb-6 border-b-2 border-muted pb-2">Account Information</h2>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-6">
              <div className="flex flex-col gap-2 p-4 bg-muted/50 rounded-lg border border-border">
                <div className="flex items-center gap-2">
                  <Mail className="w-4 h-4 text-muted-foreground shrink-0" aria-hidden="true" />
                  <span className="font-semibold text-muted-foreground text-xs uppercase tracking-wide">Email</span>
                </div>
                <div className="flex items-center gap-2 text-base text-foreground">
                  <span>{user.email}</span>
                  {user.email_verified && (
                    <span title="Email verified">
                      <CheckCircle2
                        className="w-4 h-4 text-success shrink-0"
                        aria-label="Verified"
                      />
                    </span>
                  )}
                </div>
              </div>
              <div className="flex flex-col gap-2 p-4 bg-muted/50 rounded-lg border border-border">
                <div className="flex items-center gap-2">
                  <Globe className="w-4 h-4 text-muted-foreground shrink-0" aria-hidden="true" />
                  <span className="font-semibold text-muted-foreground text-xs uppercase tracking-wide">Data Region</span>
                </div>
                <span className="flex items-center gap-2 text-base text-foreground">{regionDisplay}</span>
              </div>
              <div className="flex flex-col gap-2 p-4 bg-muted/50 rounded-lg border border-border">
                <div className="flex items-center gap-2">
                  <CheckCircle2 className="w-4 h-4 text-muted-foreground shrink-0" aria-hidden="true" />
                  <span className="font-semibold text-muted-foreground text-xs uppercase tracking-wide">Account Status</span>
                </div>
                <span className="flex items-center gap-2 text-base text-foreground">
                  {user.is_active ? 'Active' : 'Inactive'}
                </span>
              </div>
              <div className="flex flex-col gap-2 p-4 bg-muted/50 rounded-lg border border-border">
                <div className="flex items-center gap-2">
                  <Shield className="w-4 h-4 text-muted-foreground shrink-0" aria-hidden="true" />
                  <span className="font-semibold text-muted-foreground text-xs uppercase tracking-wide">User ID</span>
                </div>
                <div className="flex items-center gap-2 text-base text-foreground">
                  <span className="font-mono text-sm text-muted-foreground">{abbreviateUUID(user.id)}</span>
                  <button
                    onClick={() => copy(user.id, 'User ID copied!')}
                    className="inline-flex items-center justify-center p-1.5 bg-transparent border border-border rounded cursor-pointer hover:bg-muted hover:border-muted-foreground active:bg-muted transition-all duration-200"
                    aria-label="Copy User ID to clipboard"
                    title={copied ? 'Copied!' : 'Copy User ID'}
                  >
                    <Copy className="w-3.5 h-3.5 text-muted-foreground" aria-hidden="true" />
                  </button>
                </div>
              </div>
            </div>
          </section>

          <section className="bg-card border border-border rounded-lg p-6 mb-8">
            <h2 className="text-2xl font-semibold text-foreground mt-0 mb-6 border-b-2 border-muted pb-2">Subscription</h2>
            <SubscriptionCard
              tier={user.effective_tier ?? user.subscription_tier}
              status={user.subscription_status}
              trialDaysRemaining={user.trial_days_remaining}
            />
          </section>

          <section className="bg-card border border-border rounded-lg p-6 mb-8">
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
