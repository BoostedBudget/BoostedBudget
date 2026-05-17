import { useEffect, useRef } from 'react'
import { useUser } from '@clerk/react-router'
import { useCreatePaddleCheckout } from '@boostedbudget/api-client/endpoints/subscription/subscription'
import type { CreateCheckoutInputBody } from '@boostedbudget/api-client/models'

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
  const { isLoaded, isSignedIn } = useUser()
  const mutation = useCreatePaddleCheckout()
  const firedRef = useRef(false)

  useEffect(() => {
    if (!isLoaded || !isSignedIn) return
    if (firedRef.current) return

    const intent = readAndClearIntent()
    if (!intent) return

    firedRef.current = true
    mutation.mutateAsync({
      data: {
        tier: intent.tier,
        billing_interval: intent.billingInterval,
      } as CreateCheckoutInputBody,
    }).then((response) => {
      if (window.Paddle) {
        window.Paddle.Checkout.open({ transactionId: response.transaction_id })
      }
    }).catch(() => {
      // Silently fail — user can re-try from pricing page
    })
  }, [isLoaded, isSignedIn, mutation])
}
