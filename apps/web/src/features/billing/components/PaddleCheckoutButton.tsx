import { Button } from '@/shared/components/ui/button'
import { useCreatePaddleCheckout } from '@boostedbudget/api-client/endpoints/subscription/subscription'
import { toast } from 'sonner'
import { useState } from 'react'

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
  const [isProcessing, setIsProcessing] = useState(false)
  const createCheckout = useCreatePaddleCheckout()

  const handleCheckout = async () => {
    if (!window.Paddle) {
      toast.error('Payment system not loaded. Please refresh the page.')
      return
    }

    setIsProcessing(true)

    try {
      // Create checkout session
      const response = await createCheckout.mutateAsync({
        data: {
          tier,
          billing_interval: billingInterval,
        },
      })

      // Open Paddle checkout overlay
      window.Paddle.Checkout.open({
        settings: {
          checkoutURL: response.checkout_url,
        },
      })

      // Success handled via Paddle event callback
      onSuccess?.()
    } catch (error: any) {
      // Map error codes to user-friendly messages
      if (error.status === 403) {
        toast.error('Please sign in to subscribe')
      } else if (error.status === 400) {
        toast.error('Invalid subscription tier selected')
      } else if (error.status === 500) {
        toast.error('Payment system error. Please try again later.')
      } else {
        toast.error('Failed to start checkout. Please try again.')
      }
      console.error('Checkout error:', error)
    } finally {
      setIsProcessing(false)
    }
  }

  return (
    <Button
      onClick={handleCheckout}
      disabled={disabled || isProcessing || createCheckout.isPending}
      className="w-full"
    >
      {isProcessing || createCheckout.isPending ? 'Loading...' : 'Subscribe'}
    </Button>
  )
}
