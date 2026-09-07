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
import { useQueryClient } from '@tanstack/react-query'
import { getGetAccountsQueryKey } from '@boostedbudget/api-client/endpoints/accounts/accounts'
import type { ExchangePublicTokenResponse } from './types'
import { useTrackEvent } from '../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../shared/analytics'

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
      return 'Unable to link this account. Please contact support.'
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

/**
 * Maps a Plaid Link error (from `onExit`) to a user-readable message.
 *
 * Issue #1609 (Item 6): a 2026-09-07 production incident (Citibank Online /
 * ins_5, error_code INSTITUTION_REGISTRATION_REQUIRED) showed only a generic
 * "Something went wrong" message in the UI — the real cause was only visible
 * by cross-referencing the Plaid Dashboard's Link Sessions view. The
 * error_code is always included below so the message stays diagnosable even
 * for codes without a specific mapping.
 */
function mapPlaidErrorToMessage(err: PlaidLinkError): string {
  switch (err.error_code) {
    case 'INSTITUTION_REGISTRATION_REQUIRED':
      return 'This bank is not yet fully set up for secure connections (INSTITUTION_REGISTRATION_REQUIRED). Please try again later or contact support.'
    case 'INSTITUTION_NOT_RESPONDING':
    case 'INSTITUTION_DOWN':
      return `Your bank is temporarily unavailable (${err.error_code}). Please try again in a few minutes.`
    case 'INSTITUTION_NOT_AVAILABLE':
      return `This bank isn't available right now (${err.error_code}). Please try again later.`
    default:
      return `Unable to connect your bank account (${err.error_code}). Please try again or contact support.`
  }
}

export function PlaidLinkButton({ onSuccess, onExit, className, disabled = false }: PlaidLinkButtonProps) {
  const navigate = useNavigate()
  const [linkToken, setLinkToken] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const trackEvent = useTrackEvent()
  const queryClient = useQueryClient()

  // Use generated mutation hooks
  const createLinkTokenMutation = useCreateLinkToken()
  const exchangePublicTokenMutation = useExchangePublicToken()

  // Read accounts from cache to check if this is the first account (no extra network request)
  const cachedAccountsData = queryClient.getQueryData<{ accounts: unknown[] }>(
    getGetAccountsQueryKey({ include_hidden: 'true' })
  )
  const accounts = cachedAccountsData?.accounts ?? []

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
  const handleOnSuccess = async (public_token: string, metadata: PlaidLinkOnSuccessMetadata) => {
    try {
      setError(null)

      const response = await exchangePublicTokenMutation.mutateAsync({
        data: {
          public_token,
          institution_id: metadata.institution?.institution_id || '',
          institution_name: metadata.institution?.name || '',
        },
      })

      trackEvent(ANALYTICS_EVENTS.PLAID_LINK_SUCCEEDED)

      // Track first account linked event
      if (accounts.length === 0) {
        trackEvent(ANALYTICS_EVENTS.FIRST_ACCOUNT_LINKED)
      }

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
