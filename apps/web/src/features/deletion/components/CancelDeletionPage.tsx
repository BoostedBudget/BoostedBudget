/**
 * CancelDeletionPage Component
 *
 * Public page for canceling scheduled account deletion.
 * Accessible via JWT token link (no authentication required).
 * Token is validated by backend API.
 *
 * Route: /cancel-deletion/:token
 *
 * Issue #18: Account Deletion Flow (GDPR Article 17)
 */

import { useState, useEffect, useCallback } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useDeletion } from '../hooks/useDeletion'
import './CancelDeletionPage.css'

export function CancelDeletionPage() {
  const { token } = useParams<{ token: string }>()
  const navigate = useNavigate()
  const { cancelAccountDeletion, cancelError } = useDeletion()

  const [cancelSuccess, setCancelSuccess] = useState(false)
  const [autoProcessing, setAutoProcessing] = useState(true)
  const [isProcessing, setIsProcessing] = useState(false)

  const handleCancel = useCallback(async () => {
    if (!token) {
      return
    }

    setAutoProcessing(false)
    setIsProcessing(true)
    const result = await cancelAccountDeletion(token)
    setIsProcessing(false)

    if (result) {
      setCancelSuccess(true)
    }
  }, [token, cancelAccountDeletion])

  useEffect(() => {
    // Automatically attempt cancellation on page load if token exists
    if (token && autoProcessing) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      handleCancel()
    }
  }, [token, autoProcessing, handleCancel])

  function handleGoToProfile() {
    navigate('/profile')
  }

  function handleGoToLogin() {
    navigate('/login')
  }

  // Invalid token (no token in URL)
  if (!token) {
    return (
      <div className="cancel-deletion-page">
        <div className="cancel-deletion-container">
          <h1>Invalid Cancellation Link</h1>
          <p className="error-text">
            This cancellation link is invalid. Please check the link and try again.
          </p>
          <button onClick={handleGoToLogin} className="action-button">
            Go to Login
          </button>
        </div>
      </div>
    )
  }

  // Success state
  if (cancelSuccess) {
    return (
      <div className="cancel-deletion-page">
        <div className="cancel-deletion-container success">
          <div className="success-icon">✓</div>
          <h1>Account Deletion Canceled</h1>
          <p className="success-text">
            Your account deletion has been successfully canceled. Your account and all your data are
            safe.
          </p>
          <p>You can continue using BoostedBudget normally.</p>
          <button onClick={handleGoToProfile} className="action-button primary">
            Go to Profile
          </button>
        </div>
      </div>
    )
  }

  // Error state
  if (cancelError) {
    return (
      <div className="cancel-deletion-page">
        <div className="cancel-deletion-container error">
          <div className="error-icon">✗</div>
          <h1>Cancellation Failed</h1>
          <p className="error-text">
            We couldn't cancel your account deletion. This may be because:
          </p>
          <ul className="error-reasons">
            <li>The cancellation link has expired</li>
            <li>The link has already been used</li>
            <li>Your account deletion has already been processed</li>
          </ul>
          <p className="error-details">{cancelError.message}</p>
          <div className="error-actions">
            <button onClick={handleGoToLogin} className="action-button">
              Go to Login
            </button>
            <button onClick={() => setAutoProcessing(true)} className="action-button secondary">
              Try Again
            </button>
          </div>
        </div>
      </div>
    )
  }

  // Loading state
  if (isProcessing) {
    return (
      <div className="cancel-deletion-page">
        <div className="cancel-deletion-container loading">
          <div className="spinner"></div>
          <h1>Processing Cancellation...</h1>
          <p>Please wait while we cancel your account deletion.</p>
        </div>
      </div>
    )
  }

  // Should never reach here, but return loading state as fallback
  return (
    <div className="cancel-deletion-page">
      <div className="cancel-deletion-container loading">
        <div className="spinner"></div>
        <h1>Processing...</h1>
        <p>Please wait...</p>
      </div>
    </div>
  )
}
