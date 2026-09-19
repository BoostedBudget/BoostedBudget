import {
  beginRevenueCatCheckout,
  releaseRevenueCatCheckout,
  refreshRevenueCatSubscription,
} from '@boostedbudget/api-client/endpoints/subscription/subscription'
import { RevenueCatCheckoutBeginInputBodyTier } from '@boostedbudget/api-client/models'
import { RevenueCatCheckout } from './revenueCatCheckout'

// The pricing UI and the post-sign-in intent both hand us plain strings; the API accepts only a
// known tier. Parse at the boundary so an unknown value never reaches the request.
const CHECKOUT_TIERS: ReadonlySet<string> = new Set(Object.values(RevenueCatCheckoutBeginInputBodyTier))

function isCheckoutTier(tier: string): tier is RevenueCatCheckoutBeginInputBodyTier {
  return CHECKOUT_TIERS.has(tier)
}

export const revenueCatCheckout = new RevenueCatCheckout({
  begin: (tier, billingInterval) => {
    if (!isCheckoutTier(tier)) throw new Error('Checkout is unavailable')
    return beginRevenueCatCheckout({ tier, billing_interval: billingInterval })
  },
  release: id => releaseRevenueCatCheckout(id),
  refresh: () => refreshRevenueCatSubscription(),
  load: async (userId, isCurrent) => {
    const apiKey = window.ENV?.REVENUECAT_PUBLIC_API_KEY || import.meta.env.VITE_REVENUECAT_PUBLIC_API_KEY
    if (!apiKey) throw new Error('Checkout is not configured')
    const { Purchases } = await import('@revenuecat/purchases-js')
    if (!isCurrent()) throw new Error('Checkout identity changed')
    const purchases = Purchases.configure({ apiKey, appUserId: userId })
    return {
      close: () => purchases.close(),
      purchase: async (offeringId, packageId, htmlTarget) => {
        const offerings = await purchases.getOfferings()
        const offering = offerings.all[offeringId]
        const pkg = offering?.availablePackages.find(p => p.identifier === packageId)
        if (!pkg || !htmlTarget.isConnected) throw new Error('Checkout is unavailable')
        return purchases.purchase({ rcPackage: pkg, htmlTarget })
      },
    }
  },
})
