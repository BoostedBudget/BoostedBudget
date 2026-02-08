import { useEffect } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { CheckCircle } from 'lucide-react'
import './CheckoutSuccessPage.css'

/**
 * CheckoutSuccessPage displays after successful Paddle checkout completion.
 * Shows confirmation message and provides link to subscription management.
 * Issue #772: GAP 4 - Checkout success page
 */
export function CheckoutSuccessPage() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const tierName = searchParams.get('tier') || 'Premium'

  useEffect(() => {
    // Scroll to top on mount
    window.scrollTo(0, 0)
  }, [])

  const handleViewSubscription = () => {
    navigate('/billing/manage')
  }

  return (
    <div className="checkout-success-page">
      <div className="checkout-success-container">
        <div className="checkout-success-icon">
          <CheckCircle size={64} aria-hidden="true" />
        </div>

        <h1 className="checkout-success-title">Subscription Activated!</h1>

        <p className="checkout-success-message">
          Welcome to {tierName}! Your subscription is now active and you have access to all features.
        </p>

        <div className="checkout-success-actions">
          <button
            className="btn-primary"
            onClick={handleViewSubscription}
          >
            View Subscription Details
          </button>
        </div>

        <div className="checkout-success-info">
          <h2>What's Next?</h2>
          <ul>
            <li>Connect your bank accounts to start tracking transactions</li>
            <li>Set up automation rules to categorize expenses</li>
            <li>Explore your dashboard and customize widgets</li>
          </ul>
        </div>
      </div>
    </div>
  )
}
