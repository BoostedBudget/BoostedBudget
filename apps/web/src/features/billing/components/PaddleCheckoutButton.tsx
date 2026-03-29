import { Button } from '@/components/ui/button'
import { usePaddleCheckout } from '@/features/billing/hooks/usePaddleCheckout'
import { toast } from 'sonner'

interface PaddleCheckoutButtonProps {
  tier: string
  billingInterval: 'month' | 'year'
  disabled?: boolean
}

export function PaddleCheckoutButton({
  tier,
  billingInterval,
  disabled,
}: PaddleCheckoutButtonProps) {
  const { checkout, isLoading } = usePaddleCheckout()

  const handleCheckout = async () => {
    if (!window.Paddle) {
      toast.error('Payment system not loaded. Please refresh the page.')
      return
    }

    try {
      const response = await checkout({ tier, billingInterval })

      window.Paddle.Checkout.open({
        transactionId: response.transaction_id,
      })

      // Success navigation happens via Paddle event callback (see App.tsx)
      // DO NOT navigate here - the overlay needs to stay open for payment completion
    } catch (err: unknown) {
      // Extract status from error object
      const getErrorStatus = (error: unknown): number | undefined => {
        if (typeof error !== 'object' || !error) return undefined
        const obj = error as Record<string, unknown>
        return (obj.status as number) || (obj.response as Record<string, unknown>)?.status as number
      }

      const status = getErrorStatus(err)
      const errorMessages: Record<number, string> = {
        403: 'Please sign in to subscribe',
        400: 'Invalid subscription tier selected',
        500: 'Payment system error. Please try again later.',
      }

      const errorMessage = errorMessages[status || 0] || 'Failed to start checkout. Please try again.'
      toast.error(errorMessage)
      console.error('Checkout error:', err)
    }
  }

  return (
    <Button
      onClick={handleCheckout}
      disabled={disabled || isLoading}
      className="w-full"
    >
      {isLoading ? 'Loading...' : 'Subscribe'}
    </Button>
  )
}
