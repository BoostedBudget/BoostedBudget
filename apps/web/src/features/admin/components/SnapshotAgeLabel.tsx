/**
 * SnapshotAgeLabel
 *
 * Discloses how stale a server "snapshot instant" is, for admin surfaces that anchor their
 * override-liveness arithmetic to `resolveServerSnapshotTime(asOf)` — a value read ONCE per
 * render and never refreshed on a timer. Without this label, an admin viewing
 * SubscriptionOverrideModal / UserDetailModal / UsersPage would have no way to tell a
 * freshly-loaded surface from one that's been sitting open for minutes off an increasingly
 * stale snapshot (Issue #1462 G3 / #1432 follow-up R3-10).
 *
 * Deliberately does NOT reuse the surface's own `now` (from `resolveServerSnapshotTime`) — that
 * value is trusted-but-possibly-stale by design (it's the server's snapshot instant, held fixed
 * so liveness checks can't self-contradict mid-render). This label always compares `asOf`
 * against the REAL current wall clock via `formatRelativeTime`, so its text keeps growing
 * ("2m ago" -> "5m ago") even while the rest of the surface stays pinned to the frozen snapshot.
 *
 * Guarded by `isParsableDate` (the same ISO-strict check `resolveServerSnapshotTime` uses) so a
 * malformed-but-truthy `asOf` degrades to the same em-dash placeholder as an absent one, in both
 * the visible label and the title attribute — never the literal string "Invalid Date" in either.
 *
 * Originally introduced inline in SubscriptionOverrideModal (Issue #1462 G3); extracted here so
 * UsersPage and UserDetailModal — which anchor the identical `resolveServerSnapshotTime`
 * liveness arithmetic to their own `as_of` fields but had no disclosure of their own — share one
 * implementation instead of copying the JSX a third time.
 */
import { Clock } from 'lucide-react'
import { cn } from '@/lib/utils'
import { formatRelativeTime, isParsableDate } from '@/shared/dates'

export interface SnapshotAgeLabelProps {
  /** The server's `as_of` snapshot instant this surface's override-liveness arithmetic is
   * anchored to. May be absent (older cached response, field not yet loaded) or malformed
   * (present but unparseable) — both degrade to a plain em-dash rather than crashing or
   * rendering "Invalid Date". */
  asOf: string | null | undefined
  className?: string
}

export function SnapshotAgeLabel({ asOf, className }: SnapshotAgeLabelProps) {
  const asOfIsParsable = !!asOf && isParsableDate(asOf)
  const ageLabel = asOfIsParsable ? formatRelativeTime(asOf as string) : null

  return (
    <span
      className={cn('inline-flex items-center gap-1 text-muted-foreground/70', className)}
      title={asOfIsParsable ? `Snapshot captured ${new Date(asOf as string).toLocaleString()}` : undefined}
    >
      <Clock size={12} />
      Snapshot: {ageLabel ?? '—'}
    </span>
  )
}
