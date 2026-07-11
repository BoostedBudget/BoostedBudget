/**
 * InvitePanel Component
 *
 * Issue #1370: Household feature — InvitePanel (email-bound invite link
 * generation + revoke/regenerate)
 *
 * Renders the owner-facing invite UI: an email input that generates a
 * shareable invite link, the pending-invites list (Revoke / Regenerate), and
 * any friendly error surfaced by `useHouseholdInvites` (duplicate email,
 * seats full, etc).
 *
 * Scope note: this component renders the invite UI itself. Whether it's
 * shown at all (owner-only, seats-available gating) is the caller's
 * responsibility — this component assumes it has already been gated.
 */

import { useState, type FormEvent } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { useCopyToClipboard } from '@/shared/hooks'
import { useHouseholdInvites } from '../hooks/useHouseholdInvites'
import type { HouseholdInvite } from '../types'
import type { CreateInviteResponseBody } from '@boostedbudget/api-client/models'

/**
 * Formats an ISO date string as a short human-readable date (e.g. "Jan 8, 2025").
 *
 * Pinned to `timeZone: 'UTC'` deliberately: `expires_at` is a UTC-midnight
 * cutoff from the backend, and formatting it in the viewer's local timezone
 * would shift the displayed calendar day backward for anyone west of UTC
 * (i.e. all of the Americas) — an off-by-one that's easy to miss because a
 * UTC CI runner formats this same UTC-midnight instant identically whether
 * or not `timeZone: 'UTC'` is present, masking a regression there. See the
 * regression test in InvitePanel.test.tsx, which spies on
 * `toLocaleDateString` to assert the option is actually passed, independent
 * of the test runner's host timezone.
 */
function formatExpiresAt(dateStr: string): string {
  return new Date(dateStr).toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    timeZone: 'UTC',
  })
}

/**
 * Runs an async invite action, swallowing any error. Failures are already
 * surfaced to the user via `actionErrorMessage` from `useHouseholdInvites`,
 * so callers just need to reset their own loading state afterward.
 */
async function runGuarded(action: () => Promise<void>): Promise<void> {
  try {
    await action()
  } catch {
    // Failure is already surfaced via actionErrorMessage.
  }
}

interface GeneratedLinkCardProps {
  result: CreateInviteResponseBody
}

function GeneratedLinkCard({ result }: GeneratedLinkCardProps) {
  const { copy, copied } = useCopyToClipboard()

  return (
    <div className="space-y-2 rounded-md border border-border bg-muted/40 p-3">
      <p className="text-sm text-muted-foreground">
        Invite link for <span data-ph-mask>{result.invite.invitee_email}</span> — expires{' '}
        {formatExpiresAt(result.invite.expires_at)}
      </p>
      <div className="flex items-center gap-2">
        <span
          data-ph-mask
          className="flex-1 truncate rounded border border-input bg-background px-2 py-1 text-sm"
        >
          {result.invite_link}
        </span>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => copy(result.invite_link, 'Invite link copied!')}
        >
          {copied ? 'Copied!' : 'Copy'}
        </Button>
      </div>
    </div>
  )
}

interface PendingInviteRowProps {
  invite: HouseholdInvite
  disabled: boolean
  isRevoking: boolean
  isRegenerating: boolean
  onRevoke: (inviteId: string) => void
  onRegenerate: (invite: HouseholdInvite) => void
}

function PendingInviteRow({
  invite,
  disabled,
  isRevoking,
  isRegenerating,
  onRevoke,
  onRegenerate,
}: PendingInviteRowProps) {
  // Referenced (not duplicated) by the row's action buttons via
  // aria-describedby so screen-reader users get per-row context ("Revoke,
  // described by <email>") without ever putting the email into an aria-label
  // attribute — aria-label/title/placeholder/alt are recorded verbatim by
  // PostHog session replay regardless of visual masking, per
  // docs/security/posthog-pii-masking.md. aria-describedby only stores an id
  // reference; the PII stays solely in the already-`data-ph-mask`'d text node.
  const emailId = `invite-email-${invite.id}`

  return (
    <li className="flex flex-wrap items-center justify-between gap-2 py-3">
      <div>
        <p id={emailId} className="text-sm font-medium" data-ph-mask>
          {invite.invitee_email}
        </p>
        <p className="text-xs text-muted-foreground">Expires {formatExpiresAt(invite.expires_at)}</p>
      </div>
      <div className="flex items-center gap-2">
        <Badge variant="secondary">Pending</Badge>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={disabled}
          aria-describedby={emailId}
          onClick={() => onRegenerate(invite)}
        >
          {isRegenerating ? 'Regenerating…' : 'Regenerate'}
        </Button>
        <Button
          type="button"
          variant="destructive"
          size="sm"
          disabled={disabled}
          aria-describedby={emailId}
          onClick={() => onRevoke(invite.id)}
        >
          {isRevoking ? 'Revoking…' : 'Revoke'}
        </Button>
      </div>
    </li>
  )
}

export function InvitePanel() {
  const { invites, isLoading, actionErrorMessage, createInvite, revokeInvite, regenerateInvite } =
    useHouseholdInvites()

  const [email, setEmail] = useState('')
  const [isGenerating, setIsGenerating] = useState(false)
  const [revokingId, setRevokingId] = useState<string | null>(null)
  const [regeneratingId, setRegeneratingId] = useState<string | null>(null)
  const [generatedInvite, setGeneratedInvite] = useState<CreateInviteResponseBody | null>(null)

  const pendingInvites = invites.filter((invite) => invite.status === 'pending')
  const isRowActionInFlight = revokingId !== null || regeneratingId !== null
  // Generate and row actions (revoke/regenerate) all read and write the same
  // `generatedInvite` / `actionErrorMessage` state, so only one of them may
  // run at a time — otherwise their completions can race and leave stale or
  // mismatched state on screen (e.g. an old link shown next to a new error).
  const isAnyActionInFlight = isGenerating || isRowActionInFlight

  const handleGenerate = async (event: FormEvent) => {
    event.preventDefault()
    const trimmedEmail = email.trim()
    if (!trimmedEmail || isAnyActionInFlight) return

    setIsGenerating(true)
    // Clear any previously-displayed link up front: if this attempt fails,
    // the stale link from a prior success must not linger next to the new
    // error (it could be mistaken for the current invite's link).
    setGeneratedInvite(null)
    await runGuarded(async () => {
      const result = await createInvite(trimmedEmail)
      setGeneratedInvite(result)
      setEmail('')
    })
    setIsGenerating(false)
  }

  const handleRevoke = async (inviteId: string) => {
    if (isAnyActionInFlight) return

    setRevokingId(inviteId)
    await runGuarded(async () => {
      await revokeInvite(inviteId)
      setGeneratedInvite((current) => (current?.invite.id === inviteId ? null : current))
    })
    setRevokingId(null)
  }

  const handleRegenerate = async (invite: HouseholdInvite) => {
    if (isAnyActionInFlight) return

    setRegeneratingId(invite.id)
    // Same rationale as handleGenerate: clear the previous link eagerly so a
    // failed regenerate never leaves a stale, no-longer-valid link visible.
    setGeneratedInvite(null)
    await runGuarded(async () => {
      const result = await regenerateInvite(invite.id, invite.invitee_email)
      setGeneratedInvite(result)
    })
    setRegeneratingId(null)
  }

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader>
          <CardTitle>Invite a household member</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <form onSubmit={handleGenerate} className="flex flex-col gap-2 sm:flex-row sm:items-end">
            <div className="flex-1 space-y-2">
              <label htmlFor="invite-email" className="text-sm font-medium">
                Email address
              </label>
              <input
                id="invite-email"
                type="email"
                value={email}
                onChange={(event) => setEmail(event.target.value)}
                placeholder="name@example.com"
                disabled={isAnyActionInFlight}
                className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm transition-colors file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
              />
            </div>
            <Button type="submit" disabled={isAnyActionInFlight || !email.trim()}>
              {isGenerating ? 'Generating…' : 'Generate link'}
            </Button>
          </form>

          {actionErrorMessage && (
            <p role="alert" className="text-sm text-destructive">
              {actionErrorMessage}
            </p>
          )}

          {generatedInvite && <GeneratedLinkCard result={generatedInvite} />}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Pending invites</CardTitle>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <p className="text-sm text-muted-foreground">Loading invites…</p>
          ) : pendingInvites.length === 0 ? (
            <p className="text-sm text-muted-foreground">No pending invites.</p>
          ) : (
            <ul className="divide-y divide-border">
              {pendingInvites.map((invite) => (
                <PendingInviteRow
                  key={invite.id}
                  invite={invite}
                  disabled={isAnyActionInFlight}
                  isRevoking={revokingId === invite.id}
                  isRegenerating={regeneratingId === invite.id}
                  onRevoke={handleRevoke}
                  onRegenerate={handleRegenerate}
                />
              ))}
            </ul>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
