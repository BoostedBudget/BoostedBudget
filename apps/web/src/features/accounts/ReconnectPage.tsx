/**
 * ReconnectPage
 *
 * Handles the "Reconnect Bank" flow from a login_required / error / pending_expiration
 * notification (Issue #1609). The notification's action_url is
 * `/accounts/reconnect?item_id=<plaid item_id>` — deliberately item_id only, never a Plaid Link
 * token (see item_error_service.go and migration 000188 for why one used to be embedded there
 * and no longer is).
 *
 * Issue #1609 round 2, BLOCKING 1: a `permission_revoked` notification does NOT route here.
 * Plaid says a revoked Item cannot be restored via update mode — its notification's action_url
 * points at `/accounts` instead, the normal (non-update-mode) Link flow that creates a fresh
 * Item. See ErrLinkedItemNotRepairable and HandlePermissionRevoked (item_error_service.go).
 *
 * This page mints its OWN update-mode Plaid Link token on demand (via
 * createReconnectLinkToken), opens Plaid Link in update mode, and on success calls the REPAIR
 * endpoint (repairLinkedItem) — never the normal exchange-token flow.
 *
 * Issue #1620: that mint also returns a single-use `repair_nonce`, which the repair call must
 * present. It is held in component state for the life of the Link session and nowhere else.
 * ⚠ It is a CREDENTIAL with exactly the handling rules of the link token beside it: never put it
 * in the URL (the item_id in the query string is not a secret; this is), never log it, and never
 * send it as an analytics property. This branch removed a Plaid link token from a notification's
 * `action_url` for precisely that reason — see migration 000188. Reusing PlaidLinkButton's
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
  const [repairNonce, setRepairNonce] = useState<string | null>(null)
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
        if (cancelled) return
        setRepairNonce(response.repair_nonce)
        setLinkToken(response.link_token)
      })
      .catch(err => {
        console.error('Failed to mint reconnect link token:', err)
        trackEvent(ANALYTICS_EVENTS.ACCOUNT_RECONNECT_FAILED, { stage: 'mint' })
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
    if (!repairNonce) {
      // Unreachable in practice — Link cannot open without a token, and the token and the nonce
      // arrive in the same response — but a repair call with no nonce is refused by the server
      // anyway (403), so failing here gives the user the actionable message instead of a raw
      // status code.
      trackEvent(ANALYTICS_EVENTS.ACCOUNT_RECONNECT_FAILED, { stage: 'repair' })
      setError(
        'This reconnect session has expired. Please use the "Reconnect Bank" button from your notification again.'
      )
      return
    }
    try {
      await repairLinkedItemMutation.mutateAsync({
        data: { item_id: itemId, repair_nonce: repairNonce },
      })
      // Issue #1620: the nonce is spent the moment the server burns it. Drop it so a second
      // onSuccess from the same Link session cannot resend a value that is now guaranteed to be
      // refused, and so it stops living in component state any longer than it must.
      setRepairNonce(null)
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
