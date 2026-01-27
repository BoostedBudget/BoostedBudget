import { useCreatePaddleCheckout } from '@boostedbudget/api-client/endpoints/subscription/subscription'
import type { CreateCheckoutInputBody } from '@boostedbudget/api-client'

interface CheckoutOptions {
  tier: string
  billingInterval: 'month' | 'year'
}

interface CheckoutError {
  status?: number
  message: string
}

/**
 * Custom hook for Paddle checkout operations with error normalization.
 * Wraps the generated useCreatePaddleCheckout hook with user-friendly error handling.
 *
 * @example
 * const { checkout, isLoading, error } = usePaddleCheckout()
 *
 * const handleUpgrade = async () => {
 *   const result = await checkout({ tier: 'premium', billingInterval: 'month' })
 *   if (result) {
 *     // Checkout URL created, Paddle overlay will open
 *   }
 * }
 */
export function usePaddleCheckout() {
  const mutation = useCreatePaddleCheckout()

  const checkout = async (options: CheckoutOptions) => {
    const response = await mutation.mutateAsync({
      data: {
        tier: options.tier,
        billing_interval: options.billingInterval,
      } as CreateCheckoutInputBody,
    })
    return response
  }

  const normalizeError = (error: any): CheckoutError | null => {
    if (!error) return null

    const status = error.status || error.response?.status

    switch (status) {
      case 401:
        return { status, message: 'Please sign in to subscribe' }
      case 403:
        return { status, message: 'You do not have permission to subscribe' }
      case 400:
        return { status, message: 'Invalid subscription tier selected' }
      case 500:
        return { status, message: 'Payment system error. Please try again later.' }
      case 503:
        return { status, message: 'Payment service temporarily unavailable' }
      default:
        if (error.message?.includes('network') || error.message?.includes('fetch')) {
          return { status: 0, message: 'Network error. Please check your connection.' }
        }
        return { status, message: error.message || 'Failed to start checkout. Please try again.' }
    }
  }

  return {
    checkout,
    isLoading: mutation.isPending,
    isSuccess: mutation.isSuccess,
    isError: mutation.isError,
    error: normalizeError(mutation.error),
    reset: mutation.reset,
  }
}
