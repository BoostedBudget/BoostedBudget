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
      <div className="min-h-screen flex items-center justify-center bg-muted px-8 py-8">
        <div className="max-w-lg w-full bg-card rounded-xl px-12 py-12 shadow-md text-center">
          <h1 className="mt-0 mb-4 text-3xl text-foreground">Invalid Cancellation Link</h1>
          <p className="my-4 text-muted-foreground text-base leading-relaxed">
            This cancellation link is invalid. Please check the link and try again.
          </p>
          <button
            onClick={handleGoToLogin}
            className="bg-muted-foreground text-white border-none px-8 py-3.5 text-base font-medium rounded-lg cursor-pointer hover:bg-foreground transition-all duration-200 min-w-[150px]"
          >
            Go to Login
          </button>
        </div>
      </div>
    )
  }

  // Success state
  if (cancelSuccess) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-muted px-8 py-8">
        <div className="max-w-lg w-full bg-card rounded-xl px-12 py-12 shadow-md text-center border-[3px] border-success">
          <div className="w-20 h-20 mx-auto mb-6 bg-success/10 rounded-full flex items-center justify-center text-5xl text-success font-bold">
            ✓
          </div>
          <h1 className="mt-0 mb-4 text-3xl text-foreground">Account Deletion Canceled</h1>
          <p className="text-success font-medium my-6 text-lg">
            Your account deletion has been successfully canceled. Your account and all your data are
            safe.
          </p>
          <p className="my-4 text-muted-foreground text-base leading-relaxed">You can continue using BoostedBudget normally.</p>
          <button
            onClick={handleGoToProfile}
            className="bg-primary text-primary-foreground border-none px-8 py-3.5 text-base font-medium rounded-lg cursor-pointer hover:opacity-90 transition-all duration-200 min-w-[150px]"
          >
            Go to Profile
          </button>
        </div>
      </div>
    )
  }

  // Error state
  if (cancelError) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-muted px-8 py-8">
        <div className="max-w-lg w-full bg-card rounded-xl px-12 py-12 shadow-md text-center border-[3px] border-destructive">
          <div className="w-20 h-20 mx-auto mb-6 bg-destructive/10 rounded-full flex items-center justify-center text-5xl text-destructive font-bold">
            ✗
          </div>
          <h1 className="mt-0 mb-4 text-3xl text-foreground">Cancellation Failed</h1>
          <p className="text-destructive my-4 text-base leading-relaxed">
            We couldn't cancel your account deletion. This may be because:
          </p>
          <ul className="list-none p-0 my-6 text-left">
            <li className="relative py-2 pl-6 text-destructive/80 before:content-['•'] before:absolute before:left-2 before:text-destructive before:font-bold">
              The cancellation link has expired
            </li>
            <li className="relative py-2 pl-6 text-destructive/80 before:content-['•'] before:absolute before:left-2 before:text-destructive before:font-bold">
              The link has already been used
            </li>
            <li className="relative py-2 pl-6 text-destructive/80 before:content-['•'] before:absolute before:left-2 before:text-destructive before:font-bold">
              Your account deletion has already been processed
            </li>
          </ul>
          <div className="bg-destructive/5 border-l-4 border-destructive px-4 py-3 my-6 rounded text-destructive text-sm text-left">
            {cancelError.message}
          </div>
          <div className="flex gap-4 mt-8 justify-center flex-wrap">
            <button
              onClick={handleGoToLogin}
              className="bg-muted-foreground text-white border-none px-8 py-3.5 text-base font-medium rounded-lg cursor-pointer hover:bg-foreground transition-all duration-200 min-w-[150px]"
            >
              Go to Login
            </button>
            <button
              onClick={() => setAutoProcessing(true)}
              className="bg-warning text-white border-none px-8 py-3.5 text-base font-medium rounded-lg cursor-pointer hover:opacity-90 transition-all duration-200 min-w-[150px]"
            >
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
      <div className="min-h-screen flex items-center justify-center bg-muted px-8 py-8">
        <div className="max-w-lg w-full bg-card rounded-xl px-12 py-12 shadow-md text-center border-[3px] border-primary">
          <div className="w-16 h-16 mx-auto mb-6 border-[6px] border-border border-t-primary rounded-full animate-spin" />
          <h1 className="mt-0 mb-4 text-3xl text-foreground">Processing Cancellation...</h1>
          <p className="my-4 text-muted-foreground text-base leading-relaxed">Please wait while we cancel your account deletion.</p>
        </div>
      </div>
    )
  }

  // Should never reach here, but return loading state as fallback
  return (
    <div className="min-h-screen flex items-center justify-center bg-muted px-8 py-8">
      <div className="max-w-lg w-full bg-card rounded-xl px-12 py-12 shadow-md text-center border-[3px] border-primary">
        <div className="w-16 h-16 mx-auto mb-6 border-[6px] border-border border-t-primary rounded-full animate-spin" />
        <h1 className="mt-0 mb-4 text-3xl text-foreground">Processing...</h1>
        <p className="my-4 text-muted-foreground text-base leading-relaxed">Please wait...</p>
      </div>
    </div>
  )
}
