import { Link } from 'react-router-dom'
import { Award, Check } from 'lucide-react'
import { useSubscriptionTiers } from '../../subscription/hooks/useSubscriptionTiers'
import type { SubscriptionTier } from '../../subscription/types'

interface SubscriptionCardProps {
  tier: string // User's subscription_tier from API
  status: string // User's subscription_status from API
  trialDaysRemaining?: number
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
    featureFlags.ads_enabled ? 'Ad-supported' : 'No ads',
  ]

  if (featureFlags.ai_insights) features.push('AI-powered insights')
  if (featureFlags.priority_support) features.push('Priority support')
  if (featureFlags.advanced_reports) features.push('Advanced reports')
  if (featureFlags.api_access) features.push('API access')

  return features
}

export function SubscriptionCard({ tier, status, trialDaysRemaining }: SubscriptionCardProps) {
  const { tiers, loading, error } = useSubscriptionTiers()

  // Handle loading state
  if (loading || !tier || !status) {
    return (
      <div className="border-2 border-border rounded-xl p-6 my-4 bg-card flex justify-center items-center min-h-[200px] text-muted-foreground">
        <p>Loading subscription details...</p>
      </div>
    )
  }

  // Handle error state
  if (error) {
    return (
      <div className="border-2 border-border rounded-xl p-6 my-4 bg-card flex justify-center items-center min-h-[200px] text-destructive">
        <p>Error loading subscription: {error.message}</p>
      </div>
    )
  }

  // Get tier info from API response
  const tierInfo = tiers?.tiers[tier]

  // Handle invalid tier (shouldn't happen, but safety check)
  if (!tierInfo) {
    return (
      <div className="border-2 border-border rounded-xl p-6 my-4 bg-card flex justify-center items-center min-h-[200px] text-destructive">
        <p>Invalid subscription tier</p>
      </div>
    )
  }

  const displayName = tierInfo.display_name
  const displayPrice = formatPrice(tierInfo.pricing.price_monthly)
  const features = buildFeatureList(tierInfo)

  const isPremiumTier = ['premium', 'household_2', 'household_3', 'household_4', 'household_5'].includes(tier)

  return (
    <div className={`subscription-card tier-${tier} border-2 rounded-xl p-6 my-4 ${isPremiumTier ? 'border-primary bg-gradient-to-br from-card to-primary/5' : 'border-border bg-card'}`}>
      <div className="flex justify-between items-start mb-6 flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <Award className="w-8 h-8 text-primary" aria-hidden="true" />
          <h3 className="text-2xl m-0 font-bold text-foreground">{displayName}</h3>
        </div>
        <div className="flex items-baseline gap-1">
          <span className="text-2xl font-bold text-foreground">{displayPrice}</span>
          {tierInfo.pricing.price_monthly > 0 && (
            <span className="text-sm text-muted-foreground font-normal">/month</span>
          )}
        </div>
        <span className={`px-3.5 py-1.5 rounded-full text-xs font-semibold capitalize tracking-wide ${
          status === 'active' ? 'bg-success/10 text-success' :
          status === 'inactive' ? 'bg-destructive/10 text-destructive' :
          status === 'cancelled' ? 'bg-muted text-muted-foreground' :
          status === 'trial' ? 'bg-info/10 text-info' :
          'bg-muted text-muted-foreground'
        }`}>
          {status === 'trial' && trialDaysRemaining !== undefined
            ? `Trial — ${trialDaysRemaining} day${trialDaysRemaining === 1 ? '' : 's'} remaining`
            : status}
        </span>
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
    </div>
  )
}
