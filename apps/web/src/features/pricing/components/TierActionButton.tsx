import { useClerk } from '@clerk/react-router'
import { Link } from 'react-router'
import { Button } from '@/components/ui/button'
import { useRevenueCatCheckout } from '../../billing/hooks/useRevenueCatCheckout'
import { classifyTierChange } from '../../subscription/lib/tierDirection'
import type { PricingState } from '../hooks/usePricingContext'
import type { SubscriptionTier, TierName } from '../../subscription/types'

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
  const { openSignUp } = useClerk()
  const { checkout, isLoading } = useRevenueCatCheckout()

  const handleCheckout = async () => {
    await checkout()
  }

  if (pricingState.status === 'loading') {
    return <Button disabled className="w-full">Loading...</Button>
  }

  if (pricingState.status === 'unresolved') {
    return <Button asChild variant="outline" className="w-full"><Link to="/billing/manage">Check subscription status</Link></Button>
  }

  if (pricingState.status === 'signed-out') {
    const handleSignedOutClick = () => {
      sessionStorage.setItem('checkout_intent', JSON.stringify({ tier: tierKey, billingInterval, storedAt: Date.now() }))
      openSignUp()
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

  // Complimentary access is independent of a paid plan, including the same tier.
  if (pricingState.status === 'comped') {
    return (
      <Button onClick={handleCheckout} disabled={isLoading} className="w-full">
        {isLoading ? 'Loading...' : 'Subscribe'}
      </Button>
    )
  }

  if (pricingState.status === 'past_due') {
    return <Button disabled className="w-full">Update Payment</Button>
  }

  if (pricingState.status === 'paused') {
    return <Button disabled className="w-full">Manage Billing</Button>
  }

  if (
    pricingState.status === 'active' ||
    pricingState.status === 'trialing' ||
    pricingState.status === 'cancelled'
  ) {
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

  return <Button disabled className="w-full">Subscribe</Button>
}
