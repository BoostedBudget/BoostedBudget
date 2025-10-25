import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { usePlaidLink, PlaidLinkOnSuccessMetadata } from 'react-plaid-link'
import { fetchLinkToken, exchangePublicToken } from './api/plaidApi'
import type { ExchangePublicTokenResponse } from './types'
import './PlaidLinkButton.css'

interface PlaidLinkButtonProps {
  onSuccess?: (response: ExchangePublicTokenResponse) => void
  onExit?: () => void
  className?: string
  disabled?: boolean
}

/**
 * Maps error responses to user-friendly messages
 */
function mapErrorToMessage(error: unknown): string {
  // Type guard for error objects with response property
  const hasResponse = (err: unknown): err is { response: { status: number } } => {
    return typeof err === 'object' && err !== null && 'response' in err &&
      typeof (err as { response?: unknown }).response === 'object' &&
      (err as { response?: unknown }).response !== null &&
      'status' in ((err as { response: unknown }).response as object)
  }

  // Type guard for error objects with code property
  const hasCode = (err: unknown): err is { code: string } => {
    return typeof err === 'object' && err !== null && 'code' in err
  }

  if (hasResponse(error)) {
    if (error.response.status === 401) {
      return 'Please log in again to continue.'
    }
    if (error.response.status === 403) {
      return 'Account limit reached for your subscription tier. Please upgrade to add more accounts.'
    }
    if (error.response.status === 400) {
      return 'Invalid or expired connection. Please try again.'
    }
    if (error.response.status === 503) {
      return 'Bank connection service temporarily unavailable. Please try again in a few minutes.'
    }
    if (error.response.status === 500) {
      return 'An error occurred while connecting your bank. Please try again.'
    }
  }

  if (hasCode(error) && error.code === 'ERR_NETWORK') {
    return 'Network error. Please check your internet connection.'
  }

  return 'Failed to load bank connection. Please try again.'
}

export function PlaidLinkButton({ onSuccess, onExit, className, disabled = false }: PlaidLinkButtonProps) {
  const navigate = useNavigate()
  const [linkToken, setLinkToken] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [exchanging, setExchanging] = useState(false)

  // Fetch link token on mount (eager loading for better UX)
  useEffect(() => {
    async function loadLinkToken() {
      try {
        setLoading(true)
        setError(null)
        const response = await fetchLinkToken()
        setLinkToken(response.link_token)
      } catch (err) {
        console.error('Failed to fetch link token:', err)
        setError(mapErrorToMessage(err))
      } finally {
        setLoading(false)
      }
    }

    loadLinkToken()
  }, [])

  // Handle successful Plaid Link flow
  const handleOnSuccess = async (public_token: string, metadata: PlaidLinkOnSuccessMetadata) => {
    try {
      setExchanging(true)
      setError(null)

      const response = await exchangePublicToken({
        public_token,
        institution_id: metadata.institution?.institution_id || '',
        institution_name: metadata.institution?.name || '',
      })

      // Notify parent component if callback provided
      onSuccess?.(response)

      // Navigate to accounts page
      navigate('/accounts')
    } catch (err) {
      console.error('Failed to exchange public token:', err)
      setError(mapErrorToMessage(err))
    } finally {
      setExchanging(false)
    }
  }

  // Handle user exiting Plaid Link
  const handleOnExit = () => {
    // Call optional exit callback
    onExit?.()
    // No error state needed - user intentionally exited
  }

  // Initialize Plaid Link with token and callbacks
  const { open, ready } = usePlaidLink({
    token: linkToken,
    onSuccess: handleOnSuccess,
    onExit: handleOnExit,
  })

  // Loading state - fetching link token
  if (loading) {
    return (
      <div className={`plaid-link-button ${className || ''}`}>
        <p className="plaid-link-button__loading">Preparing bank connection...</p>
      </div>
    )
  }

  // Error state - failed to fetch link token
  if (error && !linkToken) {
    return (
      <div className={`plaid-link-button ${className || ''}`}>
        <p className="plaid-link-button__error">{error}</p>
        <button className="plaid-link-button__retry" onClick={() => window.location.reload()}>Retry</button>
      </div>
    )
  }

  // Exchanging state - connecting accounts
  if (exchanging) {
    return (
      <div className={`plaid-link-button ${className || ''}`}>
        <p className="plaid-link-button__connecting">Connecting your accounts...</p>
      </div>
    )
  }

  // Main state - ready to open Plaid Link
  return (
    <div className={`plaid-link-button ${className || ''}`}>
      {error && <p className="plaid-link-button__error">{error}</p>}
      <button
        onClick={() => open()}
        disabled={disabled || !ready || exchanging}
        className="plaid-link-button__submit"
      >
        Connect Bank Account
      </button>
    </div>
  )
}
