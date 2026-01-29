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
  const { checkout, isLoading, error } = usePaddleCheckout()

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
    } catch (err: any) {
      toast.error(error?.message || 'Failed to start checkout. Please try again.')
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
