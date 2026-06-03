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
      <div className="my-8 p-8 border-2 border-destructive rounded-lg bg-destructive/5">
        <h2 className="mt-0 text-destructive text-2xl mb-4">Account Deletion Scheduled</h2>

        <div className="bg-warning/10 border-l-4 border-warning px-6 py-4 mb-6 rounded">
          <p className="my-2 text-warning-foreground">
            <strong>Your account is scheduled for permanent deletion.</strong>
          </p>
          <p className="my-4 px-2 py-2 bg-warning/5 rounded text-warning-foreground font-[1.05rem]">
            Deletion date: <strong>{new Date(scheduledDate!).toLocaleDateString()}</strong>
          </p>
          <p className="my-2 text-warning-foreground">
            All your data (transactions, accounts, budgets, rules) will be permanently deleted on
            this date.
          </p>
        </div>

        <div className="bg-info/5 border border-info rounded-lg p-6">
          <h3 className="mt-0 text-info">Changed your mind?</h3>
          <p className="my-3 text-info/80">
            You can cancel the deletion at any time before the scheduled date using this link:
          </p>
          <div className="bg-info/10 border border-dashed border-info px-4 py-4 my-4 rounded break-all">
            <a
              href={cancellationUrl}
              className="text-primary no-underline font-mono text-sm hover:underline"
              target="_blank"
              rel="noopener noreferrer"
            >
              {cancellationUrl}
            </a>
          </div>
          <p className="bg-warning/10 border-l-4 border-warning px-3 py-3 mt-4 rounded text-warning-foreground text-sm">
            <strong>Important:</strong> Save this link! It's the only way to cancel your account
            deletion.
            {/* When SendGrid is integrated (Issue #258), this link will be emailed to you. */}
          </p>
        </div>
      </div>
    )
  }

  return (
    <div className="my-8 p-8 border-2 border-destructive rounded-lg bg-destructive/5">
      <h2 className="mt-0 text-destructive text-2xl mb-4">Delete Account</h2>

      <div className="bg-destructive/5 border-l-4 border-destructive px-6 py-4 mb-6 rounded">
        <h3 className="mt-0 text-destructive text-lg">⚠️ Danger Zone</h3>
        <p className="my-2 text-destructive/80">
          Deleting your account is <strong>permanent and irreversible</strong>. All your data will
          be lost, including:
        </p>
        <ul className="my-2 pl-6 text-destructive/80">
          <li className="my-1">Linked bank accounts and transactions</li>
          <li className="my-1">Budget data and automation rules</li>
          <li className="my-1">Profile and privacy settings</li>
          <li className="my-1">Subscription and payment history</li>
        </ul>
      </div>

      {!showConfirmation ? (
        <button
          onClick={handleDeleteClick}
          className="bg-destructive text-white border-none px-8 py-3 text-base font-bold rounded-md cursor-pointer hover:bg-destructive/80 transition-colors disabled:bg-destructive/40 disabled:cursor-not-allowed"
          disabled={isRequesting}
        >
          Delete My Account
        </button>
      ) : (
        <div className="bg-card border-2 border-destructive rounded-lg p-6 mt-4">
          <h3 className="mt-0 text-destructive/80">Are you absolutely sure?</h3>
          <p className="text-foreground">
            This action <strong>cannot be undone</strong>. Your account will be scheduled for
            deletion after a 30-day grace period.
          </p>

          <label className="flex items-center gap-2 my-6 cursor-pointer text-[0.95rem]">
            <input
              type="checkbox"
              className="w-[18px] h-[18px] cursor-pointer"
              checked={confirmChecked}
              onChange={(e) => setConfirmChecked(e.target.checked)}
            />
            <span>I understand that all my data will be permanently deleted</span>
          </label>

          {requestError && (
            <div className="bg-destructive/5 border-l-4 border-destructive text-destructive px-4 py-3 my-4 rounded">
              {requestError.message}
            </div>
          )}

          <div className="flex gap-4 mt-6 justify-end">
            <button
              onClick={handleCancel}
              className="bg-muted-foreground text-white border-none px-6 py-3 text-base rounded-md cursor-pointer hover:bg-foreground transition-colors disabled:opacity-60 disabled:cursor-not-allowed"
              disabled={isRequesting}
            >
              Cancel
            </button>
            <button
              onClick={handleConfirmDelete}
              className="bg-destructive text-white border-none px-6 py-3 text-base font-bold rounded-md cursor-pointer hover:bg-destructive/80 transition-colors disabled:bg-destructive/40 disabled:cursor-not-allowed"
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
