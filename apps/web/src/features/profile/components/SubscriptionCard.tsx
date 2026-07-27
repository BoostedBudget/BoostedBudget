import { Link } from 'react-router-dom'
import { Award, Check, Gift } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import { useSubscriptionTiers } from '../../subscription/hooks/useSubscriptionTiers'
import type { SubscriptionTier } from '../../subscription/types'
import type { SubscriptionPresentation } from '../../subscription/lib/subscriptionPresentation'

interface SubscriptionCardProps {
  tier: string // User's subscription_tier from API
  /**
   * Issue #1413: the single shared status → UI derivation (badge variant/label, comp state),
   * rather than a raw `status: string` this component used to re-derive its own badge mapping
   * from.
   */
  presentation: SubscriptionPresentation
  /**
   * Household seat usage (used/total). Caller (ProfilePage) computes this via
   * `useHousehold()` and passes it down — this component stays presentational
   * and never calls `useHousehold()` itself. Omit (or pass `total <= 1`) to
   * render nothing extra; this is distinct from the tier's static seat-COUNT
   * feature-list line ("For N household members") built in `buildFeatureList`.
   */
  seatUsage?: { used: number; total: number }
}


/**
 * Format price for display
 */
function formatPrice(price: number | undefined): string {
  return !price ? '$0' : `$${price.toFixed(2)}`
}

/**
 * Build human-readable feature list from tier config
 */
function buildFeatureList(tierData: SubscriptionTier): string[] {
  const { features: featureFlags, pricing } = tierData

  const usersLabel = pricing.users_included === 1
    ? 'For 1 person'
    : `For ${pricing.users_included} household members`

  const features = [
    usersLabel,
    'Unlimited bank accounts',
    'Unlimited automation rules',
  ]

  if (featureFlags.priority_support) features.push('Priority support')
  if (featureFlags.advanced_reports) features.push('Advanced reports')
  if (featureFlags.api_access) features.push('API access')

  return features
}

export function SubscriptionCard({ tier, presentation, seatUsage }: SubscriptionCardProps) {
  const { tiers, loading, error } = useSubscriptionTiers()

  // Handle loading state
  if (loading || !tier || !presentation) {
    return (
      <Card className="border-2 my-4 flex justify-center items-center min-h-[200px] text-muted-foreground">
        <p>Loading subscription details...</p>
      </Card>
    )
  }

  // Handle error state
  if (error) {
    return (
      <Card className="border-2 my-4 flex justify-center items-center min-h-[200px] text-destructive">
        <p>Error loading subscription: {error.message}</p>
      </Card>
    )
  }

  // Get tier info from API response
  const tierInfo = tiers?.tiers[tier]

  // Handle invalid tier (shouldn't happen, but safety check)
  if (!tierInfo) {
    return (
      <Card className="border-2 my-4 flex justify-center items-center min-h-[200px] text-destructive">
        <p>Invalid subscription tier</p>
      </Card>
    )
  }

  const displayName = tierInfo.display_name
  const displayPrice = formatPrice(tierInfo.pricing.price_monthly)
  const features = buildFeatureList(tierInfo)

  const isPremiumTier = ['premium', 'household_2', 'household_3', 'household_4', 'household_5'].includes(tier)

  // Issue #1413: a comped user is not being charged — no monthly price, and an explicit note
  // about the comp end date (or that access is ongoing) instead.
  const compEndDateLabel = presentation.isComped && presentation.compEndsAt
    ? presentation.compEndsAt.slice(0, 10)
    : undefined

  return (
    <Card className={`subscription-card tier-${tier} border-2 my-4 ${isPremiumTier ? 'border-primary bg-gradient-to-br from-card to-primary/5' : ''}`}>
      <div className="flex justify-between items-start mb-6 flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <Award className="w-8 h-8 text-primary" aria-hidden="true" />
          <h3 className="text-2xl m-0 font-bold text-foreground">{displayName}</h3>
        </div>
        {!presentation.isComped && (
          <div className="flex items-baseline gap-1">
            <span className="text-2xl font-bold text-foreground">{displayPrice}</span>
            {tierInfo.pricing.price_monthly > 0 && (
              <span className="text-sm text-muted-foreground font-normal">/month</span>
            )}
          </div>
        )}
        <Badge variant={presentation.badgeVariant} className="gap-1 tracking-wide">
          {presentation.isComped && <Gift size={14} aria-hidden="true" />}
          {presentation.badgeLabel}
        </Badge>
        {presentation.isComped && (
          <p className="w-full text-sm text-muted-foreground">
            {compEndDateLabel
              ? `Complimentary access ends ${compEndDateLabel}`
              : 'Complimentary Premium, provided by BoostedBudget'}
          </p>
        )}
      </div>

      <div className="mb-6">
        <ul className="list-none p-0 m-0 grid gap-2">
          {features.map((feature, index) => (
            <li key={index} className="flex items-center gap-2 py-2 text-foreground text-[0.9375rem]">
              <Check className="w-4 h-4 text-success shrink-0" aria-hidden="true" />
              <span>{feature}</span>
            </li>
          ))}
        </ul>
        {seatUsage && seatUsage.total > 1 && (
          <p className="text-sm text-muted-foreground mt-2">
            {seatUsage.used} of {seatUsage.total} seats used
          </p>
        )}
      </div>

      <div className="flex flex-col gap-3">
        <Link
          to="/billing/manage"
          className="w-full py-3.5 border border-border rounded-lg text-base font-semibold cursor-pointer text-center bg-muted text-foreground hover:bg-accent hover:border-muted-foreground transition-all duration-200 no-underline block"
        >
          Manage Subscription
        </Link>
        <Link
          to="/pricing"
          className="block text-center text-primary text-sm font-medium no-underline px-2 py-2 rounded hover:bg-primary/10 transition-all duration-200"
        >
          Compare Plans →
        </Link>
      </div>
    </Card>
  )
}
