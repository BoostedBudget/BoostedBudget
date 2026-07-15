/**
 * HouseholdOverCapacityBanner
 *
 * Issue #1371 Task 10: Household offboarding & lifecycle.
 *
 * Owner-only banner shown when a household is over its plan's seat limit
 * (`useHousehold().overCapacitySince` truthy). Never rendered for non-owner
 * members — over-capacity is an owner/billing concept. Mirrors
 * `SubscriptionStatusBanner`'s structure: semantic `warning` design tokens,
 * per-session dismissal via `useSessionDismiss`, and view/dismiss/CTA-click
 * analytics via `useTrackEvent`.
 */
import { useEffect } from 'react'
import { useProfile } from '@/features/profile/hooks/useProfile'
import { useHousehold } from '../hooks/useHousehold'
import { useSessionDismiss } from '@/shared/hooks/useSessionDismiss'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'

// Distinct dismiss key from SubscriptionStatusBanner's ('status' values like
// 'past_due') so the two banners are dismissed independently in the same
// session.
const DISMISS_KEY = 'household_over_capacity'

export function HouseholdOverCapacityBanner() {
  const { user } = useProfile()
  const { isOwner, overCapacitySince, seatsUsed, seatsTotal } = useHousehold()
  const track = useTrackEvent()

  const isApplicable = isOwner && !!overCapacitySince

  const { isDismissed, dismiss } = useSessionDismiss(user?.id ?? '', DISMISS_KEY)

  const overage = Math.max(seatsUsed - seatsTotal, 0)

  useEffect(() => {
    if (isApplicable) {
      track(ANALYTICS_EVENTS.HOUSEHOLD_OVER_CAPACITY_BANNER_VIEWED, { overage })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isApplicable])

  if (!user || !isApplicable || isDismissed) return null

  const handleCtaClick = () => {
    track(ANALYTICS_EVENTS.HOUSEHOLD_OVER_CAPACITY_BANNER_CTA_CLICKED, { overage })
  }

  const handleDismiss = () => {
    track(ANALYTICS_EVENTS.HOUSEHOLD_OVER_CAPACITY_BANNER_DISMISSED, { overage })
    dismiss()
  }

  return (
    <div
      role="alert"
      className="border-b px-4 py-3 flex items-center justify-between gap-4 bg-warning/10 border-warning/30 text-warning-foreground"
    >
      <div className="flex items-center gap-2 text-sm">
        <span>
          {`Your household is over your plan's seat limit. Remove ${overage} members or upgrade to avoid interruption.`}
        </span>
      </div>
      <div className="flex items-center gap-2 shrink-0">
        <a
          href="/profile?tab=household"
          onClick={handleCtaClick}
          className="shrink-0 px-3 py-1.5 text-sm font-medium bg-warning text-warning-foreground hover:bg-warning/90 rounded-lg transition-colors"
        >
          Manage household
        </a>
        <button
          onClick={handleDismiss}
          aria-label="Dismiss banner"
          className="p-1 rounded hover:bg-black/10 transition-colors text-sm"
        >
          ×
        </button>
      </div>
    </div>
  )
}
