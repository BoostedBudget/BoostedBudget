import { useEffect } from 'react'
import { useProfile } from '@/features/profile/hooks/useProfile'
import { useGetBillingPortalUrl } from '@boostedbudget/api-client/endpoints/subscription/subscription'
import { useSessionDismiss } from '@/shared/hooks/useSessionDismiss'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'

interface BannerConfig {
  bg: string
  border: string
  text: string
  dismissible: boolean
}

const SUBSCRIPTION_BANNER_CONFIG: Record<string, BannerConfig> = {
  past_due: {
    bg: 'bg-warning/10',
    border: 'border-warning/30',
    text: 'text-warning-foreground',
    dismissible: true,
  },
  cancelled: {
    bg: 'bg-destructive/10',
    border: 'border-destructive/30',
    text: 'text-destructive-foreground',
    dismissible: false,
  },
  inactive: {
    bg: 'bg-blue-500/10',
    border: 'border-blue-500/30',
    text: 'text-foreground',
    dismissible: false,
  },
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

  const getMessage = () => {
    if (status === 'past_due') {
      return "Your payment didn't go through. Update your payment method to restore access."
    }
    if (status === 'cancelled') {
      const date = user.retention_expires_at
        ? ` Access ends ${user.retention_expires_at.slice(0, 10)}.`
        : ''
      return `Your subscription has been cancelled.${date}`
    }
    return 'Your subscription has expired. Renew your plan to restore access.'
  }

  const renderCta = () => {
    if (status === 'past_due') {
      const portalUrl = billingPortalQuery.data?.portal_url
      if (portalUrl) {
        return (
          <a
            href={portalUrl}
            target="_blank"
            rel="noreferrer"
            onClick={handleCtaClick}
            className="shrink-0 px-3 py-1.5 text-sm font-medium bg-warning text-warning-foreground hover:bg-warning/90 rounded-lg transition-colors"
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

    const label = status === 'cancelled' ? 'Resubscribe' : 'Re-subscribe'
    return (
      <a
        href="/pricing"
        onClick={handleCtaClick}
        className="shrink-0 px-3 py-1.5 text-sm font-medium bg-primary text-primary-foreground hover:bg-primary/90 rounded-lg transition-colors"
      >
        {label}
      </a>
    )
  }

  return (
    <div
      role="alert"
      className={`${config.bg} border-b ${config.border} px-4 py-3 flex items-center justify-between gap-4`}
    >
      <div className={`flex items-center gap-2 text-sm ${config.text}`}>
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
