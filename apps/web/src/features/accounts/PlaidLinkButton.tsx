import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router'
import {
  usePlaidLink,
  PlaidLinkOnSuccessMetadata,
  PlaidLinkError,
  PlaidLinkOnExitMetadata,
} from 'react-plaid-link'
import {
  useCreateLinkToken,
  useExchangePublicToken,
} from '@boostedbudget/api-client/endpoints/plaid/plaid'
import type { ExchangePublicTokenResponse } from './types'
import { useTrackEvent } from '../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../shared/analytics'
import { mapErrorToMessage, mapPlaidErrorToMessage } from './plaidLinkErrorMessages'

interface PlaidLinkButtonProps {
  onSuccess?: (response: ExchangePublicTokenResponse) => void
  onExit?: () => void
  className?: string
  disabled?: boolean
}

export function PlaidLinkButton({ onSuccess, onExit, className, disabled = false }: PlaidLinkButtonProps) {
  const navigate = useNavigate()
  const [linkToken, setLinkToken] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const trackEvent = useTrackEvent()

  // Use generated mutation hooks
  const createLinkTokenMutation = useCreateLinkToken()
  const exchangePublicTokenMutation = useExchangePublicToken()

  // Derive exchanging state from mutation
  const exchanging = exchangePublicTokenMutation.isPending

  // Fetch link token on mount (eager loading for better UX)
  useEffect(() => {
    async function loadLinkToken() {
      try {
        setLoading(true)
        setError(null)
        const response = await createLinkTokenMutation.mutateAsync()
        setLinkToken(response.link_token)
      } catch (err) {
        console.error('Failed to fetch link token:', err)
        setError(mapErrorToMessage(err))
      } finally {
        setLoading(false)
      }
    }

    loadLinkToken()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Handle successful Plaid Link flow
  const handleOnSuccess = async (public_token: string | null, metadata: PlaidLinkOnSuccessMetadata) => {
    if (!public_token) {
      trackEvent(ANALYTICS_EVENTS.PLAID_LINK_FAILED)
      setError('Plaid Link completed without returning an authorization token. Please try again.')
      return
    }
    try {
      setError(null)

      const response = await exchangePublicTokenMutation.mutateAsync({
        data: {
          public_token,
          institution_id: metadata.institution?.institution_id || '',
          institution_name: metadata.institution?.name || '',
        },
      })

      trackEvent(ANALYTICS_EVENTS.ACCOUNT_LINK_COMPLETED, { provider: 'plaid' })

      // Notify parent component if callback provided
      onSuccess?.(response as ExchangePublicTokenResponse)

      // Navigate to accounts page
      navigate('/accounts')
    } catch (err) {
      console.error('Failed to exchange public token:', err)
      trackEvent(ANALYTICS_EVENTS.PLAID_LINK_FAILED)
      setError(mapErrorToMessage(err))
    }
  }

  // Handle user exiting Plaid Link.
  // Issue #1609 (Item 6): Plaid calls onExit(err, metadata) on BOTH an
  // intentional user exit AND an error exit (e.g. an unsupported
  // institution) — err is only null/undefined in the former case. The
  // previous implementation declared no parameters at all, silently
  // discarding error_code/error_message/error_type and the link session
  // and request ids, which made real failures indistinguishable from the
  // user simply closing Link.
  const handleOnExit = (err: PlaidLinkError | null, metadata: PlaidLinkOnExitMetadata) => {
    if (err) {
      // Diagnostic identifiers only (error code/type, session/request ids) —
      // never account numbers, balances, or tokens.
      console.error('Plaid Link exited with an error:', {
        error_code: err.error_code,
        error_type: err.error_type,
        error_message: err.error_message,
        link_session_id: metadata.link_session_id,
        request_id: metadata.request_id,
      })
      trackEvent(ANALYTICS_EVENTS.PLAID_LINK_FAILED, {
        error_code: err.error_code,
        error_type: err.error_type,
      })
      setError(mapPlaidErrorToMessage(err))
    }
    // Call optional exit callback in both cases - it is the component's public prop
    onExit?.()
    // No error state needed when err is null - user intentionally exited
  }

  // Initialize Plaid Link with token and callbacks
  const { open, ready } = usePlaidLink({
    token: linkToken,
    onSuccess: handleOnSuccess,
    onExit: handleOnExit,
  })

  const wrapperClass = `flex flex-col gap-4 p-4 ${className || ''}`

  // Loading state - fetching link token
  if (loading) {
    return (
      <div className={wrapperClass}>
        <p className="flex items-center justify-center py-8 text-muted-foreground">Preparing bank connection...</p>
      </div>
    )
  }

  // Error state - failed to fetch link token
  if (error && !linkToken) {
    return (
      <div className={wrapperClass}>
        <p className="px-3 py-2 mb-4 bg-destructive/10 border border-destructive/30 rounded text-destructive text-sm">{error}</p>
        <button
          className="mt-2 px-4 py-2 bg-primary text-primary-foreground border-none rounded cursor-pointer text-base hover:bg-primary/90 transition-colors"
          onClick={() => window.location.reload()}
        >
          Retry
        </button>
      </div>
    )
  }

  // Exchanging state - connecting accounts
  if (exchanging) {
    return (
      <div className={wrapperClass}>
        <p className="flex items-center justify-center py-8 text-muted-foreground">Connecting your accounts...</p>
      </div>
    )
  }

  // Main state - ready to open Plaid Link
  return (
    <div className={wrapperClass}>
      {error && <p className="px-3 py-2 mb-4 bg-destructive/10 border border-destructive/30 rounded text-destructive text-sm">{error}</p>}
      <button
        onClick={() => { trackEvent(ANALYTICS_EVENTS.PLAID_LINK_STARTED); open() }}
        disabled={disabled || !ready || exchanging}
        className="px-6 py-3 bg-primary text-primary-foreground border-none rounded font-medium cursor-pointer hover:bg-primary/90 transition-colors min-h-[44px] w-full sm:w-auto disabled:bg-muted disabled:text-muted-foreground disabled:cursor-not-allowed disabled:opacity-60"
      >
        Connect Bank Account
      </button>
    </div>
  )
}
