/**
 * DeleteAccountSection Component
 *
 * Displays account deletion UI in user profile/settings page.
 * Shows deletion confirmation form with:
 * - Warning about permanent data loss
 * - Confirmation checkbox
 * - Delete button
 * - Cancellation token (after deletion request)
 *
 * Issue #18: Account Deletion Flow (GDPR Article 17)
 */

import { useState } from 'react'
import { useDeletion } from '../hooks/useDeletion'
import './DeleteAccountSection.css'

export function DeleteAccountSection() {
  const { requestAccountDeletion, isRequesting, requestError } = useDeletion()

  const [showConfirmation, setShowConfirmation] = useState(false)
  const [confirmChecked, setConfirmChecked] = useState(false)
  const [deletionScheduled, setDeletionScheduled] = useState(false)
  const [scheduledDate, setScheduledDate] = useState<string | null>(null)
  const [cancellationToken, setCancellationToken] = useState<string | null>(null)

  async function handleDeleteClick() {
    setShowConfirmation(true)
  }

  async function handleConfirmDelete() {
    if (!confirmChecked) {
      return
    }

    const result = await requestAccountDeletion()
    if (result) {
      setDeletionScheduled(true)
      setScheduledDate(result.scheduled_at)
      setCancellationToken(result.cancellation_token)
      setShowConfirmation(false)
    }
  }

  function handleCancel() {
    setShowConfirmation(false)
    setConfirmChecked(false)
  }

  // Show cancellation link after deletion is scheduled
  if (deletionScheduled && cancellationToken) {
    const cancellationUrl = `${window.location.origin}/cancel-deletion/${cancellationToken}`

    return (
      <div className="delete-account-section">
        <h2 className="section-title">Account Deletion Scheduled</h2>

        <div className="deletion-scheduled-notice">
          <p className="notice-text">
            <strong>Your account is scheduled for permanent deletion.</strong>
          </p>
          <p className="scheduled-date">
            Deletion date: <strong>{new Date(scheduledDate!).toLocaleDateString()}</strong>
          </p>
          <p className="notice-text">
            All your data (transactions, accounts, budgets, rules) will be permanently deleted on
            this date.
          </p>
        </div>

        <div className="cancellation-info">
          <h3>Changed your mind?</h3>
          <p>
            You can cancel the deletion at any time before the scheduled date using this link:
          </p>
          <div className="cancellation-link-box">
            <a href={cancellationUrl} className="cancellation-link" target="_blank" rel="noopener noreferrer">
              {cancellationUrl}
            </a>
          </div>
          <p className="link-instructions">
            <strong>Important:</strong> Save this link! It's the only way to cancel your account
            deletion.
            {/* When SendGrid is integrated (Issue #258), this link will be emailed to you. */}
          </p>
        </div>
      </div>
    )
  }

  return (
    <div className="delete-account-section">
      <h2 className="section-title">Delete Account</h2>

      <div className="warning-box">
        <h3>⚠️ Danger Zone</h3>
        <p>
          Deleting your account is <strong>permanent and irreversible</strong>. All your data will
          be lost, including:
        </p>
        <ul>
          <li>Linked bank accounts and transactions</li>
          <li>Budget data and automation rules</li>
          <li>Profile and privacy settings</li>
          <li>Subscription and payment history</li>
        </ul>
      </div>

      {!showConfirmation ? (
        <button onClick={handleDeleteClick} className="delete-button" disabled={isRequesting}>
          Delete My Account
        </button>
      ) : (
        <div className="confirmation-dialog">
          <h3>Are you absolutely sure?</h3>
          <p>
            This action <strong>cannot be undone</strong>. Your account will be scheduled for
            deletion after a 30-day grace period.
          </p>

          <label className="confirmation-checkbox">
            <input
              type="checkbox"
              checked={confirmChecked}
              onChange={(e) => setConfirmChecked(e.target.checked)}
            />
            <span>I understand that all my data will be permanently deleted</span>
          </label>

          {requestError && <div className="error-message">{requestError.message}</div>}

          <div className="confirmation-actions">
            <button onClick={handleCancel} className="cancel-button" disabled={isRequesting}>
              Cancel
            </button>
            <button
              onClick={handleConfirmDelete}
              className="confirm-delete-button"
              disabled={!confirmChecked || isRequesting}
            >
              {isRequesting ? 'Processing...' : 'Yes, Delete My Account'}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
