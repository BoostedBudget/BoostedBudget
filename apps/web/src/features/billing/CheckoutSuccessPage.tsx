import { useEffect } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { CheckCircle } from 'lucide-react'
import { useTrackEvent } from '../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../shared/analytics'

/**
 * CheckoutSuccessPage displays after successful Paddle checkout completion.
 * Shows confirmation message and provides link to subscription management.
 * Issue #772: GAP 4 - Checkout success page
 */
export function CheckoutSuccessPage() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const tierName = searchParams.get('tier') || 'Premium'
  const trackEvent = useTrackEvent()

  useEffect(() => {
    // Scroll to top on mount
    window.scrollTo(0, 0)
    trackEvent(ANALYTICS_EVENTS.CHECKOUT_COMPLETED)
  }, [trackEvent])

  const handleViewSubscription = () => {
    navigate('/billing/manage')
  }

  return (
    <div className="min-h-screen flex items-center justify-center px-8 py-8 bg-gradient-to-br from-primary/80 to-primary">
      <div className="bg-card rounded-xl shadow-2xl max-w-lg w-full p-12 text-center">
        <div className="text-success mb-6 flex justify-center">
          <CheckCircle size={64} aria-hidden="true" />
        </div>

        <h1 className="text-3xl font-bold text-foreground mb-4">Subscription Activated!</h1>

        <p className="text-lg text-muted-foreground mb-8 leading-relaxed">
          Welcome to {tierName}! Your subscription is now active and you have access to all features.
        </p>

        <div className="mb-8">
          <button
            className="bg-primary text-primary-foreground border-none px-8 py-3 text-base font-semibold rounded-lg cursor-pointer hover:opacity-90 transition-opacity"
            onClick={handleViewSubscription}
          >
            View Subscription Details
          </button>
        </div>

        <div className="bg-muted rounded-lg p-6 text-left">
          <h2 className="text-xl font-semibold text-foreground mb-4">What's Next?</h2>
          <ul className="list-none p-0 m-0">
            <li className="py-2 text-muted-foreground flex items-start before:content-['✓'] before:text-success before:font-bold before:mr-3 before:shrink-0">
              Connect your bank accounts to start tracking transactions
            </li>
            <li className="py-2 text-muted-foreground flex items-start before:content-['✓'] before:text-success before:font-bold before:mr-3 before:shrink-0">
              Set up automation rules to categorize expenses
            </li>
            <li className="py-2 text-muted-foreground flex items-start before:content-['✓'] before:text-success before:font-bold before:mr-3 before:shrink-0">
              Explore your dashboard and customize widgets
            </li>
          </ul>
        </div>
      </div>
    </div>
  )
}
