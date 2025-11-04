import { useState, useEffect } from 'react'
import { PlaidLinkButton } from './PlaidLinkButton'
import { AccountCard } from './AccountCard'
import { UpgradeBanner } from './UpgradeBanner'
import { SyncStatsCard } from './SyncStatsCard'
import { fetchAccounts, deleteAccount } from './api/plaidApi'
import { fetchProfile } from '../profile/api/profileApi'
import { useSubscriptionTiers } from '../subscription/hooks/useSubscriptionTiers'
import { useTierLimits } from '../subscription/hooks/useTierLimits'
import type { Account } from './types'
import type { User } from '../profile/types'
import './AccountsPage.css'

export function AccountsPage() {
  const [accounts, setAccounts] = useState<Account[]>([])
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // Fetch subscription tiers for limit enforcement
  const { tiers, loading: tiersLoading } = useSubscriptionTiers()

  // Load accounts and user profile on mount
  useEffect(() => {
    loadAccountsAndProfile()
  }, [])

  async function loadAccountsAndProfile() {
    try {
      setLoading(true)
      setError(null)

      // Fetch both accounts and user profile in parallel
      const [accountsResponse, userProfile] = await Promise.all([
        fetchAccounts(),
        fetchProfile(),
      ])

      setAccounts(accountsResponse.accounts)
      setUser(userProfile)
    } catch (err) {
      console.error('Failed to load accounts:', err)
      setError('Failed to load accounts. Please try again.')
    } finally {
      setLoading(false)
    }
  }

  // Handle successful account linking - reload accounts and profile
  function handleAccountsLinked() {
    loadAccountsAndProfile()
  }

  // Handle account deletion
  async function handleDeleteAccount(accountId: string) {
    try {
      await deleteAccount(accountId)
      // Reload accounts after successful deletion
      await loadAccountsAndProfile()
    } catch (err) {
      console.error('Failed to delete account:', err)
      setError('Failed to delete account. Please try again.')
    }
  }

  // Calculate tier limits using hook
  const { isAtLimit, shouldShowUpgradeBanner, accountCountDisplay } = useTierLimits({
    user,
    tiers,
    currentAccountCount: accounts.length,
  })

  if (loading || tiersLoading) {
    return (
      <div className="accounts-page responsive">
        <div className="loading">Loading your accounts...</div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="accounts-page responsive">
        <div className="error-state">
          <p className="error-message">{error}</p>
          <button onClick={loadAccountsAndProfile} className="btn-retry">
            Retry
          </button>
        </div>
      </div>
    )
  }

  return (
    <div className="accounts-page responsive">
      <div className="accounts-header">
        <h1>My Accounts</h1>
        <PlaidLinkButton
          onSuccess={handleAccountsLinked}
          disabled={isAtLimit}
        />
      </div>

      {shouldShowUpgradeBanner && <UpgradeBanner />}

      {/* Issue #242: Monthly sync limits - show sync statistics */}
      <div className="sync-stats-section">
        <SyncStatsCard />
      </div>

      {accounts.length === 0 ? (
        <div className="empty-state">
          <p className="empty-state__message">
            No accounts connected yet. Click "Connect Bank Account" above to get started.
          </p>
        </div>
      ) : (
        <div className="accounts-list">
          <p className="accounts-count">
            {accountCountDisplay}
          </p>
          <div className="accounts-grid">
            {accounts.map((account) => (
              <AccountCard
                key={account.id}
                account={account}
                onDelete={handleDeleteAccount}
              />
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
