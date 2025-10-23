import { useState, useEffect } from 'react'
import { PlaidLinkButton } from './PlaidLinkButton'
import { AccountCard } from './AccountCard'
import { fetchAccounts } from './api/plaidApi'
import type { Account } from './types'
import './AccountsPage.css'

export function AccountsPage() {
  const [accounts, setAccounts] = useState<Account[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // Load accounts on mount
  useEffect(() => {
    loadAccounts()
  }, [])

  async function loadAccounts() {
    try {
      setLoading(true)
      setError(null)
      const response = await fetchAccounts()
      setAccounts(response.accounts)
    } catch (err) {
      console.error('Failed to load accounts:', err)
      setError('Failed to load accounts. Please try again.')
    } finally {
      setLoading(false)
    }
  }

  // Handle successful account linking - reload accounts
  function handleAccountsLinked() {
    loadAccounts()
  }

  if (loading) {
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
          <button onClick={loadAccounts} className="btn-retry">
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
        <PlaidLinkButton onSuccess={handleAccountsLinked} />
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
            {accounts.length} {accounts.length === 1 ? 'account' : 'accounts'} connected
          </p>
          <div className="accounts-grid">
            {accounts.map((account) => (
              <AccountCard key={account.id} account={account} />
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
