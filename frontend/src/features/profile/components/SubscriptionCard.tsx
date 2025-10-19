import { SUBSCRIPTION_TIERS } from '../types'
import type { User } from '../types'
import './SubscriptionCard.css'

interface SubscriptionCardProps {
  tier: User['subscription_tier']
  status: User['subscription_status']
}

export function SubscriptionCard({ tier, status }: SubscriptionCardProps) {
  const tierInfo = SUBSCRIPTION_TIERS[tier]
  const isPaidTier = tier !== 'free'

  // Generate CSS class name for tier
  const tierClassName = tier.replace('+', '-plus')

  return (
    <div className={`subscription-card tier-${tierClassName}`}>
      <div className="subscription-header">
        <h3 className="tier-name">{tierInfo.name}</h3>
        <p className="tier-price">{tierInfo.price}</p>
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
