/**
 * ScopeSegmentedControl
 * Issue #1370: Household feature — Personal/Household dashboard scope toggle
 *
 * A ToggleGroup (Personal | Household) that drives DashboardContext's `scope`
 * state, which widget-level hooks (useDashboardData, useNetWorthData,
 * useCashFlowData) fold into the `scope` query param sent to the balance
 * summary/history, net worth summary/history, and cash flow summary/history
 * endpoints.
 *
 * Hidden entirely (not merely disabled) when the caller has no active
 * household membership — there's nothing to toggle to, and a visible-but-inert
 * control would be confusing. See `useHousehold`'s `isInHousehold` for the
 * membership signal.
 *
 * Reset-on-leave-household: this component also owns the defensive reset of
 * `scope` back to 'personal' when `isInHousehold` transitions to false (e.g.
 * the user leaves/is removed from their household, or the household is
 * disbanded) while `scope` is still 'household'. It's implemented here rather
 * than in DashboardContext itself so that DashboardContext (and the many
 * tests that mount DashboardProvider) don't need to depend on `useHousehold`
 * (and its `useQueryClient` requirement). This component is always mounted
 * whenever the dashboard filter row is visible — even while hidden (returning
 * null), its hooks (including this effect) still run — so the reset fires
 * promptly regardless of whether the toggle itself is currently displayed.
 *
 * Share-coverage caption (Issue #1706): when `scope === 'household'` the
 * household total correctly sums only accounts the caller has shared into the
 * household, but the UI never said so. This caption states the CALLER'S OWN
 * coverage ("N of your M accounts are shared") and links to the household
 * sharing controls. It deliberately reports nothing about any OTHER member:
 * the owner's constraint is that we must not expose which accounts are NOT
 * shared with the other members. Both counts come from caller-scoped sources
 * only — `useActiveAccounts` (the same account list the dashboard renders, so
 * M can never disagree with it) and `useHouseholdVisibility` (which wraps the
 * caller's own `GET /households/me/account-shares`). `useHouseholdVisibility`
 * does not filter `revoked_at`, so a revoked share is excluded here.
 * Enforced by ScopeSegmentedControl.privacy.test.tsx.
 */

import { useEffect } from 'react'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { useHousehold } from '@/features/household/hooks/useHousehold'
import { useHouseholdVisibility } from '@/features/household/hooks/useHouseholdVisibility'
import { useActiveAccounts } from '@/features/accounts/hooks'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import { useDashboardContext } from '../context/DashboardContext'
import type { DashboardScope } from '../types'

/**
 * Data-free fallback shown while the caller's share list is loading or has
 * errored. The endpoint has a measured transient failure, and the explanation
 * must not vanish exactly when it is most needed — so this states the
 * inclusion rule without any count rather than rendering nothing.
 */
const SHARE_COVERAGE_FALLBACK =
  'Household totals include only accounts shared with the household.'

const MANAGE_SHARING_HREF = '/profile?tab=household'

/**
 * Resolves a raw ToggleGroup onValueChange value into a valid DashboardScope,
 * or null if invalid. Radix's ToggleGroup (type="single") fires onValueChange
 * with an empty string when the user clicks the already-selected item
 * (deselecting it) — that's not a valid scope, so it must be filtered out
 * rather than passed through to setScope.
 */
// eslint-disable-next-line react-refresh/only-export-components
export function resolveScopeSelection(value: string): DashboardScope | null {
  return value === 'personal' || value === 'household' ? value : null
}

export function ScopeSegmentedControl() {
  const { scope, setScope } = useDashboardContext()
  const { isInHousehold } = useHousehold()
  const { accounts } = useActiveAccounts()
  const { shares, isLoading: sharesLoading, error: sharesError } = useHouseholdVisibility()
  const trackEvent = useTrackEvent()

  // Defensive reset — see file header. Hooks must run unconditionally, so this
  // effect is declared before the `isInHousehold` early return below.
  useEffect(() => {
    if (!isInHousehold && scope !== 'personal') {
      setScope('personal')
    }
  }, [isInHousehold, scope, setScope])

  if (!isInHousehold) return null

  const ownAccounts = accounts ?? []
  const sharedUserAccountIds = new Set(
    shares
      .filter((share) => share.visibility === 'shared' && share.revoked_at == null)
      .map((share) => share.user_account_id)
  )
  const sharedCount = ownAccounts.filter(
    (account) =>
      account.user_account_id != null && sharedUserAccountIds.has(account.user_account_id)
  ).length
  const sharesUnavailable = sharesLoading || sharesError != null

  const handleValueChange = (value: string) => {
    const resolved = resolveScopeSelection(value)
    if (!resolved) return
    setScope(resolved)
    trackEvent(ANALYTICS_EVENTS.DASHBOARD_SCOPE_CHANGED, { scope: resolved })
  }

  return (
    <div data-testid="scope-segmented-control">
      <ToggleGroup type="single" value={scope} onValueChange={handleValueChange}>
        <ToggleGroupItem value="personal" aria-label="Personal scope">
          Personal
        </ToggleGroupItem>
        <ToggleGroupItem value="household" aria-label="Household scope">
          Household
        </ToggleGroupItem>
      </ToggleGroup>
      {scope === 'household' && (
        <p
          data-testid="household-share-coverage"
          className="mt-2 text-xs text-muted-foreground"
        >
          {sharesUnavailable ? (
            SHARE_COVERAGE_FALLBACK
          ) : (
            <>
              <span>
                {sharedCount} of your {ownAccounts.length} accounts are shared
              </span>{' '}
              <a
                href={MANAGE_SHARING_HREF}
                className="font-medium text-primary underline underline-offset-2 hover:no-underline"
              >
                Manage sharing
              </a>
            </>
          )}
        </p>
      )}
    </div>
  )
}
