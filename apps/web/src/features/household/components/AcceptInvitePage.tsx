/**
 * AcceptInvitePage Component
 *
 * Issue #1370: Household feature — standalone invite-accept page (Task 6)
 *
 * Public, unauthenticated-accessible page a household invitee lands on when
 * they click their invite link. Routed (see App.tsx) at BOTH:
 *   - `/households/invites/:token` — the REAL link the backend generates
 *     (`household_service.go`'s `invite_link = frontendURL + "/households/invites/" + token`)
 *   - `/household/invite/:token` — an alias matching the original issue text
 *
 * Neither route is wrapped in `<ProtectedRoute>` — the invite token itself is
 * the credential (see `GetInvitePreviewResponseBody`'s "Unauthenticated — the
 * token is the credential" doc comment in api/openapi.yaml), and a
 * not-yet-signed-in invitee must be able to see who invited them before
 * being asked to sign in.
 *
 * State machine (checked in this order):
 *   1. loading      — preview still fetching, or Clerk hasn't loaded yet
 *   2. not_found     — invalid/expired/unknown token (404)
 *   3. seats_full    — household is full (409), from either the initial
 *                      preview or a later Join attempt
 *   4. unknown (preview) — any other error (network, 5xx, …) from the
 *                      INITIAL preview fetch, where `preview` is still null.
 *                      Must be handled explicitly and BEFORE the signed-out
 *                      and default branches below, or it falls through and
 *                      renders a broken "Join undefined" form with live
 *                      Join/Decline buttons. A later Join/Decline mutation
 *                      failing with 'unknown' is handled separately, inline,
 *                      inside the default branch (9) — `preview` is already
 *                      populated by then, so the form stays usable.
 *   5. signed out    — show preview + "Sign in to continue"
 *   6. email_mismatch — signed in with the wrong account (403)
 *   7. declined      — local state after a successful decline
 *   8. accepted      — local state after a successful accept; redirects to
 *                      /dashboard after a brief confirmation
 *   9. default       — signed in, no error: ConsentPreview + Join/Decline
 */

import { useEffect, useState } from 'react'
import { useParams, useNavigate } from 'react-router'
import { useAuth, useClerk } from '@clerk/react-router'
import { Button } from '@/components/ui/button'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import { useAcceptInvite } from '../hooks/useAcceptInvite'
import { ConsentPreview } from './ConsentPreview'

/** How long the post-accept confirmation stays on screen before redirecting to the dashboard. */
const ACCEPT_SUCCESS_REDIRECT_DELAY_MS = 1500

function formatExpiresAt(dateStr: string): string {
  return new Date(dateStr).toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    timeZone: 'UTC',
  })
}

function CenteredCard({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex min-h-screen items-center justify-center bg-muted px-4 py-8">
      <Card className="w-full max-w-lg text-center">{children}</Card>
    </div>
  )
}

export function AcceptInvitePage() {
  const { token } = useParams<{ token: string }>()
  const navigate = useNavigate()
  const { isSignedIn, isLoaded: isAuthLoaded } = useAuth()
  const { openSignIn } = useClerk()
  const { preview, isPreviewLoading, errorKind, accept, decline, refetchPreview } = useAcceptInvite(token ?? '')
  const trackEvent = useTrackEvent()

  const [hiddenAccountIds, setHiddenAccountIds] = useState<string[]>([])
  const [isJoining, setIsJoining] = useState(false)
  const [isDeclining, setIsDeclining] = useState(false)
  const [accepted, setAccepted] = useState(false)
  const [declined, setDeclined] = useState(false)

  useEffect(() => {
    if (!accepted) return
    const timer = setTimeout(() => navigate('/dashboard'), ACCEPT_SUCCESS_REDIRECT_DELAY_MS)
    return () => clearTimeout(timer)
  }, [accepted, navigate])

  const handleJoin = async () => {
    setIsJoining(true)
    try {
      await accept(hiddenAccountIds)
      setAccepted(true)
      // Fire only once the join has actually persisted, never on failure.
      // hidden_account_count is an aggregate count, not PII — see
      // docs/security/posthog-pii-masking.md.
      trackEvent(ANALYTICS_EVENTS.HOUSEHOLD_INVITE_ACCEPTED, {
        hidden_account_count: hiddenAccountIds.length,
      })
    } catch {
      // errorKind (email_mismatch / seats_full / unknown) is already surfaced by the hook.
    } finally {
      setIsJoining(false)
    }
  }

  const handleDecline = async () => {
    setIsDeclining(true)
    try {
      await decline()
      setDeclined(true)
      trackEvent(ANALYTICS_EVENTS.HOUSEHOLD_INVITE_DECLINED)
    } catch {
      // errorKind is already surfaced by the hook.
    } finally {
      setIsDeclining(false)
    }
  }

  // 1. Loading — preview still fetching, or Clerk hasn't resolved auth state yet.
  if (!token || isPreviewLoading || !isAuthLoaded) {
    return (
      <CenteredCard>
        <CardHeader>
          <CardTitle>Loading invite…</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground">Please wait while we look up this invite.</p>
        </CardContent>
      </CenteredCard>
    )
  }

  // 2. Invalid / expired / unknown token.
  if (errorKind === 'not_found') {
    return (
      <CenteredCard>
        <CardHeader>
          <CardTitle>Invite not found</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground">
            This invite is invalid or has expired. Ask whoever invited you to send a new one.
          </p>
        </CardContent>
      </CenteredCard>
    )
  }

  // 3. Seats full — can happen on the initial preview or after clicking Join.
  if (errorKind === 'seats_full') {
    return (
      <CenteredCard>
        <CardHeader>
          <CardTitle>Household is full</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground">
            This household is now full. Ask the household owner to free up a seat or upgrade their plan.
          </p>
        </CardContent>
      </CenteredCard>
    )
  }

  // 4. Preview-level "unknown" error (network, 5xx, …) — `preview` is null
  //    in this case, so this MUST be checked before the signed-out/default
  //    branches below: otherwise it falls through and renders a broken
  //    "Join undefined" form with live Join/Decline buttons. A later
  //    Join/Decline *mutation* failing with 'unknown' is handled separately,
  //    inline, in the default branch (state 9) below — `preview` is already
  //    populated there, so the form stays usable and retryable.
  if (errorKind === 'unknown' && !preview) {
    return (
      <CenteredCard>
        <CardHeader>
          <CardTitle>Something went wrong</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-sm text-muted-foreground">
            We couldn&apos;t load this invite right now. Please try again.
          </p>
          <Button variant="outline" onClick={() => { void refetchPreview() }}>
            Try again
          </Button>
        </CardContent>
      </CenteredCard>
    )
  }

  // 5. Signed out — show who invited them, then prompt sign-in.
  if (!isSignedIn) {
    return (
      <CenteredCard>
        <CardHeader>
          <CardTitle>Join <span data-ph-mask>{preview?.household_name}</span></CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {preview && (
            <p className="text-sm text-muted-foreground">
              This invite expires {formatExpiresAt(preview.expires_at)}.
            </p>
          )}
          <Button onClick={() => openSignIn({ forceRedirectUrl: window.location.href })}>
            Sign in to continue
          </Button>
        </CardContent>
      </CenteredCard>
    )
  }

  // 6. Signed in, but the wrong account.
  if (errorKind === 'email_mismatch') {
    return (
      <CenteredCard>
        <CardHeader>
          <CardTitle>Wrong account</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground">
            This invite was sent to <span data-ph-mask>{preview?.invitee_email}</span> — sign in with
            that account instead.
          </p>
        </CardContent>
      </CenteredCard>
    )
  }

  // 7. Declined.
  if (declined) {
    return (
      <CenteredCard>
        <CardHeader>
          <CardTitle>Invite declined</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground">You&apos;ve declined this household invite.</p>
        </CardContent>
      </CenteredCard>
    )
  }

  // 8. Accepted — brief confirmation, then redirect (see the effect above).
  if (accepted) {
    return (
      <CenteredCard>
        <CardHeader>
          <CardTitle>You&apos;re in!</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground">
            You&apos;ve joined <span data-ph-mask>{preview?.household_name}</span>. Taking you to your
            dashboard…
          </p>
        </CardContent>
      </CenteredCard>
    )
  }

  // 9. Default — signed in, no error, not yet acted on: consent + Join/Decline.
  return (
    <div className="flex min-h-screen items-center justify-center bg-muted px-4 py-8">
      <Card className="w-full max-w-2xl">
        <CardHeader>
          <CardTitle>Join <span data-ph-mask>{preview?.household_name}</span></CardTitle>
        </CardHeader>
        <CardContent className="space-y-6">
          {preview && (
            <p className="text-sm text-muted-foreground">
              This invite expires {formatExpiresAt(preview.expires_at)}.
            </p>
          )}

          <ConsentPreview onHiddenAccountIdsChange={setHiddenAccountIds} />

          {errorKind === 'unknown' && (
            <p role="alert" className="text-sm text-destructive">
              Something went wrong. Please try again.
            </p>
          )}

          <div className="flex flex-wrap gap-3">
            <Button onClick={handleJoin} disabled={isJoining || isDeclining}>
              {isJoining ? 'Joining…' : 'Join household'}
            </Button>
            <Button variant="outline" onClick={handleDecline} disabled={isJoining || isDeclining}>
              {isDeclining ? 'Declining…' : 'Decline'}
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
