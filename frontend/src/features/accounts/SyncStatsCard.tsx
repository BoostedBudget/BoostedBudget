import { useState, useEffect } from 'react'
import { fetchSyncStats } from './api/syncStatsApi'
import type { SyncStats } from './types'
import './SyncStatsCard.css'

/**
 * Displays monthly sync statistics for subscription tier limits
 * Shows users how many accounts they've synced and their remaining capacity
 *
 * Issue #242: Monthly sync limits
 *
 * Features:
 * - Visual progress indicator (accounts synced / limit)
 * - Reset date countdown
 * - Upgrade prompt when at limit (free tier)
 * - Unlimited tier badge
 */
export function SyncStatsCard() {
  const [stats, setStats] = useState<SyncStats | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    loadSyncStats()
  }, [])

  async function loadSyncStats() {
    try {
      setIsLoading(true)
      setError(null)
      const data = await fetchSyncStats()
      setStats(data)
    } catch (err) {
      console.error('Failed to load sync stats:', err)
      setError('Failed to load sync statistics')
    } finally {
      setIsLoading(false)
    }
  }

  if (isLoading) {
    return (
      <div className="sync-stats-card">
        <div className="sync-stats-card__header">
          <h2 className="sync-stats-card__title">Monthly Sync Limits</h2>
        </div>
        <div className="sync-stats-card__content">
          <p className="sync-stats-card__loading">Loading...</p>
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="sync-stats-card">
        <div className="sync-stats-card__header">
          <h2 className="sync-stats-card__title">Monthly Sync Limits</h2>
        </div>
        <div className="sync-stats-card__content">
          <div className="sync-stats-card__alert sync-stats-card__alert--error">
            <span className="sync-stats-card__alert-icon">⚠️</span>
            <p className="sync-stats-card__alert-message">
              Failed to load sync statistics. Please try again later.
            </p>
          </div>
        </div>
      </div>
    )
  }

  if (!stats) {
    return null
  }

  const isUnlimited = stats.accounts_limit === -1
  const isAtLimit = !isUnlimited && stats.accounts_synced >= stats.accounts_limit
  const progressPercentage = isUnlimited
    ? 100
    : Math.min(100, (stats.accounts_synced / stats.accounts_limit) * 100)

  // Format reset date as readable string
  const resetDate = new Date(stats.reset_date)
  const formattedResetDate = resetDate.toLocaleDateString('en-US', {
    month: 'long',
    day: 'numeric',
    year: 'numeric',
  })

  return (
    <div className="sync-stats-card">
      <div className="sync-stats-card__header">
        <h2 className="sync-stats-card__title">Monthly Sync Limits</h2>
        {isUnlimited && (
          <span className="sync-stats-card__badge">
            ✓ Unlimited
          </span>
        )}
      </div>

      <div className="sync-stats-card__content">
        {/* Progress Section */}
        <div className="sync-stats-card__progress-section">
          <div className="sync-stats-card__progress-label">
            <span className="sync-stats-card__label">Accounts Synced This Month</span>
            <span className="sync-stats-card__value">
              {stats.accounts_synced} {!isUnlimited && `of ${stats.accounts_limit}`}
            </span>
          </div>

          {/* Progress Bar */}
          {!isUnlimited && (
            <div className="sync-stats-card__progress-bar">
              <div
                className={`sync-stats-card__progress-fill ${
                  isAtLimit ? 'sync-stats-card__progress-fill--warning' : ''
                }`}
                style={{ width: `${progressPercentage}%` }}
              />
            </div>
          )}
        </div>

        {/* At Limit Warning */}
        {isAtLimit && (
          <div className="sync-stats-card__alert sync-stats-card__alert--warning">
            <span className="sync-stats-card__alert-icon">⚠️</span>
            <p className="sync-stats-card__alert-message">
              You've reached your monthly sync limit. Upgrade to sync more accounts or wait
              until {formattedResetDate}.
            </p>
          </div>
        )}

        {/* Reset Date */}
        <div className="sync-stats-card__reset-info">
          <span className="sync-stats-card__icon">🕐</span>
          <span className="sync-stats-card__reset-text">Resets on {formattedResetDate}</span>
        </div>

        {/* Transactions Summary */}
        {stats.total_transactions > 0 && (
          <p className="sync-stats-card__transaction-summary">
            {stats.total_transactions} transactions synced this month
          </p>
        )}
      </div>
    </div>
  )
}
