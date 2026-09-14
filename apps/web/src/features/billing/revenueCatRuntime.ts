import {
  beginRevenueCatCheckout,
  releaseRevenueCatCheckout,
  refreshRevenueCatSubscription,
} from '@boostedbudget/api-client/endpoints/subscription/subscription'
import { RevenueCatCheckout } from './revenueCatCheckout'

export const revenueCatCheckout = new RevenueCatCheckout({
  begin: () => beginRevenueCatCheckout(),
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
      present: async (offeringId, htmlTarget, purchaseHtmlTarget) => {
        const offerings = await purchases.getOfferings()
        const offering = offerings.all[offeringId]
        if (!offering || !htmlTarget.isConnected || !purchaseHtmlTarget.isConnected) {
          throw new Error('Checkout is unavailable')
        }
        return purchases.presentPaywall({ offering, htmlTarget, purchaseHtmlTarget })
      },
    }
  },
})
