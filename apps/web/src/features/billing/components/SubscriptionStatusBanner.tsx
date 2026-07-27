import { useEffect } from 'react'
import { useProfile } from '@/features/profile/hooks/useProfile'
import { presentationFromUser } from '@/features/subscription/lib/subscriptionPresentation'
import { useGetBillingPortalUrl } from '@boostedbudget/api-client/endpoints/subscription/subscription'
import { useSessionDismiss } from '@/shared/hooks/useSessionDismiss'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import { bannerVariants, bannerCtaVariants } from '@/shared/components/bannerVariants'

type BannerVariant = 'warning' | 'destructive' | 'info'

interface BannerConfig {
  variant: BannerVariant
  dismissible: boolean
}

// Single source of truth: status → behavior. Styling is delegated to bannerVariants.
const SUBSCRIPTION_BANNER_CONFIG: Record<string, BannerConfig> = {
  past_due: { variant: 'warning', dismissible: true },
  cancelled: { variant: 'destructive', dismissible: false },
  inactive: { variant: 'info', dismissible: false },
}

/**
 * Issue #1413: a complimentary access grant that's ending within this many days surfaces a calm
 * heads-up banner. A permanent comp (or one ending further out) stays silent — a persistent banner
 * announcing a good state is a nag, not a service (calm confidence).
 */
const COMP_ENDING_SOON_THRESHOLD_DAYS = 14

function isCompEndingSoon(compEndsAt: string | undefined): boolean {
  if (!compEndsAt) return false
  const daysRemaining = (new Date(compEndsAt).getTime() - Date.now()) / (1000 * 60 * 60 * 24)
  return daysRemaining <= COMP_ENDING_SOON_THRESHOLD_DAYS
}

export function SubscriptionStatusBanner() {
  const { user } = useProfile()
  const track = useTrackEvent()

  const rawStatus = user?.subscription_status ?? ''
  const presentation = user ? presentationFromUser(user) : null
  const isComped = presentation?.isComped ?? false
  const compEndsAt = presentation?.compEndsAt
  const compEndingSoon = isComped && isCompEndingSoon(compEndsAt)

  // Comped users get their own single-purpose banner slot ('comped'); everything else keys off
  // the raw subscription_status exactly as before.
  const trackedStatus = isComped ? 'comped' : rawStatus

  const config: BannerConfig | null = isComped
    ? (compEndingSoon ? { variant: 'info', dismissible: true } : null)
    : (SUBSCRIPTION_BANNER_CONFIG[rawStatus] ?? null)

  const billingPortalQuery = useGetBillingPortalUrl({
    query: { enabled: !isComped && rawStatus === 'past_due' },
  })

  const { isDismissed, dismiss } = useSessionDismiss(user?.id ?? '', trackedStatus)

  useEffect(() => {
    if (config) {
      track(ANALYTICS_EVENTS.SUBSCRIPTION_BANNER_VIEWED, { status: trackedStatus })
    }
  }, [trackedStatus]) // eslint-disable-line react-hooks/exhaustive-deps

  if (!user || !config || isDismissed) return null

  const handleCtaClick = () => {
    track(ANALYTICS_EVENTS.SUBSCRIPTION_BANNER_CTA_CLICKED, { status: trackedStatus })
  }

  const handleDismiss = () => {
    track(ANALYTICS_EVENTS.SUBSCRIPTION_BANNER_DISMISSED, { status: trackedStatus })
    dismiss()
  }

  const isPastDue = rawStatus === 'past_due'
  const isCancelled = rawStatus === 'cancelled'

  const getMessage = () => {
    // Only reachable when config is non-null, i.e. isComped implies "ending soon" here.
    if (isComped) {
      const date = compEndsAt?.slice(0, 10)
      return date
        ? `Complimentary access ends ${date}.`
        : 'Complimentary access is ending soon.'
    }
    if (isPastDue) {
      return "Your payment didn't go through. Update your payment method to restore access."
    }
    if (isCancelled) {
      const date = user.retention_expires_at
        ? ` Access ends ${user.retention_expires_at.slice(0, 10)}.`
        : ''
      return `Your subscription has been cancelled.${date}`
    }
    return 'Your subscription has expired. Renew your plan to restore access.'
  }

  const renderCta = () => {
    if (isComped) {
      return (
        <a
          href="/pricing"
          onClick={handleCtaClick}
          className={bannerCtaVariants({ variant: 'primary' })}
        >
          View plans
        </a>
      )
    }
    if (isPastDue) {
      const portalUrl = billingPortalQuery.data?.portal_url
      if (portalUrl) {
        return (
          <a
            href={portalUrl}
            target="_blank"
            rel="noreferrer"
            onClick={handleCtaClick}
            className={bannerCtaVariants({ variant: 'warning' })}
          >
            Update payment method
          </a>
        )
      }
      return (
        <button
          disabled
          className="shrink-0 px-3 py-1.5 text-sm font-medium bg-warning/50 text-warning-foreground rounded-lg opacity-50 cursor-not-allowed"
        >
          Update payment method
        </button>
      )
    }

    return (
      <a
        href="/pricing"
        onClick={handleCtaClick}
        className={bannerCtaVariants({ variant: 'primary' })}
      >
        Resubscribe
      </a>
    )
  }

  return (
    <div
      role="alert"
      className={bannerVariants({ variant: config.variant })}
    >
      <div className="flex items-center gap-2 text-sm">
        <span>{getMessage()}</span>
      </div>
      <div className="flex items-center gap-2 shrink-0">
        {renderCta()}
        {config.dismissible && (
          <button
            onClick={handleDismiss}
            aria-label="Dismiss banner"
            className="p-1 rounded hover:bg-black/10 transition-colors text-sm"
          >
            ×
          </button>
        )}
      </div>
    </div>
  )
}
