import { useEffect } from 'react'
import { useProfile } from '@/features/profile/hooks/useProfile'
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

export function SubscriptionStatusBanner() {
  const { user } = useProfile()
  const track = useTrackEvent()

  const status = user?.subscription_status ?? ''
  const config = SUBSCRIPTION_BANNER_CONFIG[status]

  const billingPortalQuery = useGetBillingPortalUrl({
    query: { enabled: status === 'past_due' },
  })

  const { isDismissed, dismiss } = useSessionDismiss(user?.id ?? '', status)

  useEffect(() => {
    if (config) {
      track(ANALYTICS_EVENTS.SUBSCRIPTION_BANNER_VIEWED, { status })
    }
  }, [status]) // eslint-disable-line react-hooks/exhaustive-deps

  if (!user || !config || isDismissed) return null

  const handleCtaClick = () => {
    track(ANALYTICS_EVENTS.SUBSCRIPTION_BANNER_CTA_CLICKED, { status })
  }

  const handleDismiss = () => {
    track(ANALYTICS_EVENTS.SUBSCRIPTION_BANNER_DISMISSED, { status })
    dismiss()
  }

  const isPastDue = status === 'past_due'
  const isCancelled = status === 'cancelled'

  const getMessage = () => {
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
        className="shrink-0 px-3 py-1.5 text-sm font-medium bg-primary text-primary-foreground hover:bg-primary/90 rounded-lg transition-colors"
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
