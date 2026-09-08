/**
 * ReconnectPage
 *
 * Handles the "Reconnect Bank" flow from a login_required / pending_expiration /
 * permission_revoked notification (Issue #1609). The notification's action_url is
 * `/accounts/reconnect?item_id=<plaid item_id>` — deliberately item_id only, never a Plaid Link
 * token (see item_error_service.go and migration 000188 for why one used to be embedded there
 * and no longer is).
 *
 * This page mints its OWN update-mode Plaid Link token on demand (via
 * createReconnectLinkToken), opens Plaid Link in update mode, and on success calls the REPAIR
 * endpoint (repairLinkedItem) — never the normal exchange-token flow. Reusing PlaidLinkButton's
 * token/exchange wiring here would call ExchangePublicToken, which creates a BRAND NEW
 * linked_items row (AccountService.LinkAccounts) — the user would end up with the broken
 * connection PLUS a duplicate. Only the onExit error-mapping is intentionally shared with
 * PlaidLinkButton (see mapPlaidErrorToMessage) — Plaid Link errors mean the same thing regardless
 * of which flow opened Link.
 */

import { useEffect, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router'
import { useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import {
  usePlaidLink,
  type PlaidLinkError,
  type PlaidLinkOnExitMetadata,
} from 'react-plaid-link'
import {
  useCreateReconnectLinkToken,
  useRepairLinkedItem,
} from '@boostedbudget/api-client/endpoints/plaid/plaid'
import { getGetAccountsQueryKey } from '@boostedbudget/api-client/endpoints/accounts/accounts'
import { useTrackEvent } from '../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../shared/analytics'
import { mapPlaidErrorToMessage } from './plaidLinkErrorMessages'
import { mutationFailureMessage } from '@/features/admin/lib/mutationFailureMessage'

export function ReconnectPage() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const itemId = searchParams.get('item_id')
  const trackEvent = useTrackEvent()
  const queryClient = useQueryClient()

  const [linkToken, setLinkToken] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const createReconnectLinkTokenMutation = useCreateReconnectLinkToken()
  const repairLinkedItemMutation = useRepairLinkedItem()

  // Mint an update-mode link token for this specific item on mount.
  useEffect(() => {
    if (!itemId) {
      setError(
        'This reconnect link is missing its account reference. Please use the "Reconnect Bank" button from your notification again.'
      )
      return
    }

    let cancelled = false
    createReconnectLinkTokenMutation
      .mutateAsync({ data: { item_id: itemId } })
      .then(response => {
        if (!cancelled) setLinkToken(response.link_token)
      })
      .catch(err => {
        console.error('Failed to mint reconnect link token:', err)
        if (!cancelled) setError(mutationFailureMessage(err))
      })

    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [itemId])

  // Handle successful update-mode Plaid Link flow: repair, NOT exchange.
  const handleOnSuccess = async () => {
    if (!itemId) return
    try {
      await repairLinkedItemMutation.mutateAsync({ data: { item_id: itemId } })
      trackEvent(ANALYTICS_EVENTS.ACCOUNT_RECONNECT_SUCCEEDED)
      queryClient.invalidateQueries({ queryKey: getGetAccountsQueryKey() })
      toast.success('Bank connection reconnected.')
      navigate('/accounts')
    } catch (err) {
      console.error('Failed to clear error state after a successful reconnect:', err)
      trackEvent(ANALYTICS_EVENTS.ACCOUNT_RECONNECT_FAILED, { stage: 'repair' })
      setError(mutationFailureMessage(err))
    }
  }

  // Handle user exiting Plaid Link. Same shape as PlaidLinkButton's onExit (Issue #1609 Item 6):
  // err is only null/undefined on an intentional user exit.
  const handleOnExit = (err: PlaidLinkError | null, metadata: PlaidLinkOnExitMetadata) => {
    if (err) {
      console.error('Plaid Link exited with an error during reconnect:', {
        error_code: err.error_code,
        error_type: err.error_type,
        error_message: err.error_message,
        link_session_id: metadata.link_session_id,
        request_id: metadata.request_id,
      })
      trackEvent(ANALYTICS_EVENTS.ACCOUNT_RECONNECT_FAILED, {
        stage: 'link',
        error_code: err.error_code,
        error_type: err.error_type,
      })
      setError(mapPlaidErrorToMessage(err))
    }
    // No error state needed when err is null - user intentionally exited Link without error.
  }

  const { open, ready } = usePlaidLink({
    token: linkToken,
    onSuccess: handleOnSuccess,
    onExit: handleOnExit,
  })

  // Open Plaid Link automatically once ready — this page has no other purpose than reconnecting
  // the item named by item_id, so there is nothing else for the user to click first (same
  // auto-open pattern as PlaidOAuthCallback).
  useEffect(() => {
    if (ready && linkToken) {
      trackEvent(ANALYTICS_EVENTS.ACCOUNT_RECONNECT_LINK_STARTED)
      open()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ready, linkToken, open])

  return (
    <div className="flex min-h-[50vh] flex-col items-center justify-center gap-4 p-8 text-center">
      {error ? (
        <>
          <h1 className="text-xl font-semibold text-foreground">We couldn&apos;t reconnect your bank</h1>
          <p className="max-w-md text-muted-foreground">{error}</p>
          <button
            className="mt-2 px-6 py-3 bg-primary text-primary-foreground border-none rounded font-medium cursor-pointer hover:bg-primary/90 transition-colors min-h-[44px]"
            onClick={() => navigate('/accounts')}
          >
            Back to Accounts
          </button>
        </>
      ) : (
        <>
          <h1 className="text-xl font-semibold text-foreground">Reconnecting your bank&hellip;</h1>
          <p className="text-muted-foreground">Please wait while we prepare your bank connection.</p>
        </>
      )}
    </div>
  )
}
