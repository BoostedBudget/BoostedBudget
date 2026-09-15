import { useClerk } from '@clerk/react-router'
import { Link } from 'react-router'
import { Button } from '@/components/ui/button'
import { useRevenueCatCheckout } from '../../billing/hooks/useRevenueCatCheckout'
import { BillingPortalButton } from '../../billing/components/BillingPortalButton'
import type { PricingState } from '../hooks/usePricingContext'
import type { TierName } from '../../subscription/types'

interface TierActionButtonProps {
  tierKey: TierName
  billingInterval: 'month' | 'year'
  pricingState: PricingState
  /** The provider's resolved management portal — the single self-service action (#1652). */
  billingPortalUrl?: string
  isLoadingPortal?: boolean
  portalError?: unknown
}

export function TierActionButton({
  tierKey,
  billingInterval,
  pricingState,
  billingPortalUrl,
  isLoadingPortal,
  portalError,
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

  // Any paid subscription (active, trialing, cancelled, past_due, paused) is managed through the
  // provider's own portal. #1652 removed the in-app Paddle update/cancel/preview mutations, so
  // there is exactly one self-service action here — no per-tier Upgrade/Downgrade/Resubscribe.
  return (
    <BillingPortalButton
      billingPortalUrl={billingPortalUrl}
      isLoadingPortal={!!isLoadingPortal}
      error={portalError}
    />
  )
}
