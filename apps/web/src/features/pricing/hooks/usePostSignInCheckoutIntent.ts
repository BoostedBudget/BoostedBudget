import { useEffect, useRef } from 'react'
import { useUser } from '@clerk/react-router'
import { revenueCatCheckout } from '../../billing/revenueCatRuntime'

const INTENT_KEY = 'checkout_intent'
const INTENT_TTL_MS = 10 * 60 * 1000

interface CheckoutIntent {
  tier: string
  billingInterval: 'month' | 'year'
  storedAt?: number
}

function readAndClearIntent(): CheckoutIntent | null {
  try {
    const raw = sessionStorage.getItem(INTENT_KEY)
    if (!raw) return null
    sessionStorage.removeItem(INTENT_KEY)
    const intent: CheckoutIntent = JSON.parse(raw)
    if (intent.storedAt && Date.now() - intent.storedAt > INTENT_TTL_MS) return null
    if (!intent.tier || !intent.billingInterval) return null
    return intent
  } catch {
    sessionStorage.removeItem(INTENT_KEY)
    return null
  }
}

export function usePostSignInCheckoutIntent() {
  const { isLoaded, isSignedIn, user } = useUser()
  const firedRef = useRef(false)

  useEffect(() => {
    if (!isLoaded || !isSignedIn || !user?.id) return
    if (firedRef.current) return

    const intent = readAndClearIntent()
    if (!intent) return

    firedRef.current = true
    revenueCatCheckout.setIdentity(user.id)
    void revenueCatCheckout.start()
  }, [isLoaded, isSignedIn, user?.id])
}
