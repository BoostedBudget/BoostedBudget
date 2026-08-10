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
 * ("2m ago" -> "5m ago") even while the rest of the surface stays pinned to the frozen snapshot —
 * driven by a ~30s re-render interval below (this component has no other source of rerenders,
 * since its own props rarely change once a modal/page is open).
 *
 * Guarded by `parseServerDateTime` (the same Go time.Time wire-format check
 * `resolveServerSnapshotTime` uses, via `isParsableDate`) so a malformed-but-truthy `asOf`
 * degrades to the same em-dash placeholder as an absent one, in both the visible label and the
 * title attribute — never the literal string "Invalid Date" in either. `asOf` is parsed exactly
 * ONCE per render, here, and that single `Date` flows to both the label (via `formatRelativeTime`)
 * and the `title` attribute — rather than each re-parsing the source string with a different
 * parser and risking disagreement on a malformed instant (Issue #1432 follow-up S4).
 *
 * Originally introduced inline in SubscriptionOverrideModal (Issue #1462 G3); extracted here so
 * UsersPage and UserDetailModal — which anchor the identical `resolveServerSnapshotTime`
 * liveness arithmetic to their own `as_of` fields but had no disclosure of their own — share one
 * implementation instead of copying the JSX a third time.
 */
import { useEffect, useState } from 'react'
import { Clock } from 'lucide-react'
import { cn } from '@/lib/utils'
import { formatRelativeTime, parseServerDateTime } from '@/shared/dates'

/** How often this label re-renders to keep its relative-age text current. Doesn't need to be
 * precise — the label buckets to whole minutes/hours/days — just frequent enough that "2m ago"
 * visibly becomes "3m ago" without the admin needing to trigger an unrelated rerender. */
const TICK_INTERVAL_MS = 30_000

export interface SnapshotAgeLabelProps {
  /** The server's `as_of` snapshot instant this surface's override-liveness arithmetic is
   * anchored to. May be absent (older cached response, field not yet loaded) or malformed
   * (present but unparseable) — both degrade to a plain em-dash rather than crashing or
   * rendering "Invalid Date". */
  asOf: string | null | undefined
  className?: string
}

export function SnapshotAgeLabel({ asOf, className }: SnapshotAgeLabelProps) {
  // Forces a rerender roughly every 30s so `ageLabel` (computed fresh below from the REAL wall
  // clock on every render) keeps advancing. The interval's own callback carries no state — it
  // exists purely to make React call this component again.
  const [, forceTick] = useState(0)
  useEffect(() => {
    const id = setInterval(() => forceTick((t) => t + 1), TICK_INTERVAL_MS)
    return () => clearInterval(id)
  }, [])

  const parsedAsOf = parseServerDateTime(asOf)
  const ageLabel = parsedAsOf ? formatRelativeTime(parsedAsOf) : null

  return (
    <span
      className={cn('inline-flex items-center gap-1 text-muted-foreground/70', className)}
      title={parsedAsOf ? `Snapshot captured ${parsedAsOf.toLocaleString()}` : undefined}
    >
      <Clock size={12} />
      Snapshot: {ageLabel ?? '—'}
    </span>
  )
}
