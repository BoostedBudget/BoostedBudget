import { Button } from '@/components/ui/button'
import { usePaddleCheckout } from '@/features/billing/hooks/usePaddleCheckout'
import { toast } from 'sonner'

interface PaddleCheckoutButtonProps {
  tier: string
  billingInterval: 'month' | 'year'
  disabled?: boolean
  onSuccess?: () => void
}

export function PaddleCheckoutButton({
  tier,
  billingInterval,
  disabled,
  onSuccess,
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
        settings: {
          checkoutURL: response.checkout_url,
        },
      })

      onSuccess?.()
    } catch (err: unknown) {
      const status = (err as { status?: number; response?: { status?: number } }).status ||
                     (err as { status?: number; response?: { status?: number } }).response?.status

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
