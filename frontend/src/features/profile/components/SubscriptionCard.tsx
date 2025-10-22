import { SUBSCRIPTION_TIERS } from '../types'
import type { User } from '../types'
import './SubscriptionCard.css'

interface SubscriptionCardProps {
  tier: User['subscription_tier']
  status: User['subscription_status']
}

export function SubscriptionCard({ tier, status }: SubscriptionCardProps) {
  // Handle undefined/null tier or status gracefully
  if (!tier || !status) {
    return (
      <div className="subscription-card loading">
        <p>Loading subscription details...</p>
      </div>
    )
  }

  const tierInfo = SUBSCRIPTION_TIERS[tier]

  // Handle invalid tier (shouldn't happen, but safety check)
  if (!tierInfo) {
    return (
      <div className="subscription-card error">
        <p>Invalid subscription tier</p>
      </div>
    )
  }

  const isPaidTier = tier !== 'free'

  // Generate CSS class name for tier (tier names now use underscores: premium_plus)
  const tierClassName = tier

  return (
    <div className={`subscription-card tier-${tierClassName}`}>
      <div className="subscription-header">
        <h3 className="tier-name">{tierInfo.name}</h3>
        <p className="tier-price">{tierInfo.priceMonthly}</p>
        <span className={`status-badge status-${status}`}>{status}</span>
      </div>

      <div className="subscription-features">
        <h4>Features:</h4>
        <ul>
          {tierInfo.features.map((feature, index) => (
            <li key={index}>{feature}</li>
          ))}
        </ul>
      </div>

      <div className="subscription-actions">
        {isPaidTier ? (
          <button className="btn-manage">Manage Subscription</button>
        ) : (
          <button className="btn-upgrade">Upgrade to Premium</button>
        )}
      </div>
    </div>
  )
}
