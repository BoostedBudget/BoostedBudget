import { useNavigate } from 'react-router-dom'
import { Button } from '@/components/ui/button'
import { usePaddleCheckout } from '../../billing/hooks/usePaddleCheckout'
import { classifyTierChange } from '../../subscription/lib/tierDirection'
import type { PricingState } from '../hooks/usePricingContext'
import type { SubscriptionTier } from '../../subscription/types'
import type { TierName } from '../../subscription/types'

interface TierActionButtonProps {
  tierKey: TierName
  billingInterval: 'month' | 'year'
  pricingState: PricingState
  tiers: Record<TierName, SubscriptionTier> | null
  onChangePlan: (tier: TierName, interval: 'month' | 'year') => void
}

export function TierActionButton({
  tierKey,
  billingInterval,
  pricingState,
  tiers,
  onChangePlan,
}: TierActionButtonProps) {
  const navigate = useNavigate()
  const { checkout, isLoading } = usePaddleCheckout()

  const handleCheckout = async () => {
    try {
      const response = await checkout({ tier: tierKey, billingInterval })
      if (window.Paddle) {
        window.Paddle.Checkout.open({ transactionId: response.transaction_id })
      }
    } catch {
      // Error handling done by usePaddleCheckout
    }
  }

  if (pricingState.status === 'loading') {
    return <Button disabled className="w-full">Loading...</Button>
  }

  if (pricingState.status === 'signed-out') {
    const handleSignedOutClick = () => {
      sessionStorage.setItem('checkout_intent', JSON.stringify({ tier: tierKey, billingInterval, storedAt: Date.now() }))
      navigate('/sign-up', { state: { checkoutIntent: { tier: tierKey, billingInterval } } })
    }
    return (
      <Button onClick={handleSignedOutClick} className="w-full">
        Subscribe
      </Button>
    )
  }

  if (pricingState.status === 'inactive' || pricingState.status === 'trial') {
    const label = pricingState.status === 'trial' ? 'Start plan' : 'Subscribe'
    return (
      <Button onClick={handleCheckout} disabled={isLoading} className="w-full">
        {isLoading ? 'Loading...' : label}
      </Button>
    )
  }

  if (pricingState.status === 'past_due') {
    return <Button disabled className="w-full">Update Payment</Button>
  }

  if (pricingState.status === 'active' || pricingState.status === 'trialing') {
    const direction = tiers
      ? classifyTierChange(pricingState.tier, pricingState.billingInterval, tierKey, billingInterval, tiers)
      : 'same'

    if (direction === 'same') {
      return <Button disabled className="w-full">Current Plan</Button>
    }

    const label =
      direction === 'upgrade' ? 'Upgrade' :
      direction === 'downgrade' ? 'Downgrade' :
      billingInterval === 'year' ? 'Switch to Yearly' : 'Switch to Monthly'

    return (
      <Button onClick={() => onChangePlan(tierKey, billingInterval)} className="w-full">
        {label}
      </Button>
    )
  }

  if (pricingState.status === 'cancelled') {
    const isSameTier = pricingState.tier === tierKey
    if (isSameTier) {
      return <Button disabled className="w-full">Current Plan</Button>
    }
    return (
      <Button onClick={() => onChangePlan(tierKey, billingInterval)} className="w-full">
        Change Plan
      </Button>
    )
  }

  return <Button disabled className="w-full">Subscribe</Button>
}
