import { Award, Check } from 'lucide-react'

interface CurrentSubscriptionDisplayProps {
  tier: string
  tierName: string
  status: string
  billingInterval: string
  amount: string
  currency: string
  features: string[]
}

export function CurrentSubscriptionDisplay({
  tierName,
  status,
  billingInterval,
  amount,
  currency,
  features,
}: CurrentSubscriptionDisplayProps) {
  const billingPeriod = billingInterval === 'year' ? 'year' : 'month'
  const statusClass = `status-badge status-${status.toLowerCase()}`
  const statusLabel = status.charAt(0).toUpperCase() + status.slice(1)

  return (
    <section className="current-subscription-card" aria-label="Current subscription details">
      <div className="card-header">
        <div className="tier-header">
          <Award className="tier-icon" aria-hidden="true" size={24} />
          <h2 className="tier-title">Current Subscription</h2>
        </div>
      </div>

      <div className="card-body">
        <div className="tier-info">
          <div className="tier-name-row">
            <h3 className="tier-name">{tierName}</h3>
            <span className="tier-badge" aria-label="Current plan badge">
              Current Plan
            </span>
          </div>
          <span className={statusClass} aria-label={`Subscription status: ${statusLabel}`}>
            {statusLabel}
          </span>
        </div>

        <div className="pricing" aria-label="Pricing information">
          <span className="price" aria-label={`Price: ${currency} ${amount} per ${billingPeriod}`}>
            {currency} {amount}
          </span>
          <span className="billing-period">per {billingPeriod}</span>
        </div>

        <div className="features">
          <h4 className="features-title">Features:</h4>
          <ul className="features-list" aria-label="Subscription features">
            {features.map((feature, index) => (
              <li key={index} className="feature-item">
                <Check className="check-icon" aria-hidden="true" size={16} />
                <span>{feature}</span>
              </li>
            ))}
          </ul>
        </div>
      </div>
    </section>
  )
}
