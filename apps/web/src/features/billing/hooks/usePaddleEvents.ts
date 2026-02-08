import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { toast } from 'sonner'

/**
 * Custom hook to listen for Paddle checkout events and handle navigation.
 * This hook should be mounted in components that initiate checkout flows.
 *
 * Events:
 * - paddle:checkout.completed: User completed payment, navigate to success page
 * - paddle:checkout.closed: User closed the checkout overlay without completing payment
 */
export function usePaddleEvents() {
  const navigate = useNavigate()

  useEffect(() => {
    const handleCheckoutCompleted = (event: Event) => {
      const paddleEvent = (event as CustomEvent).detail
      const transactionId = paddleEvent.data?.transaction_id
      console.log('Checkout completed:', { transactionId, event: paddleEvent })
      navigate('/billing/success')
    }

    const handleCheckoutClosed = (event: Event) => {
      const paddleEvent = (event as CustomEvent).detail
      // Only show toast if checkout was closed without completion
      if (paddleEvent.name === 'checkout.closed' && !paddleEvent.data?.status) {
        console.log('Checkout was closed without completion')
        toast.info('Checkout was cancelled')
      }
    }

    window.addEventListener('paddle:checkout.completed', handleCheckoutCompleted)
    window.addEventListener('paddle:checkout.closed', handleCheckoutClosed)

    return () => {
      window.removeEventListener('paddle:checkout.completed', handleCheckoutCompleted)
      window.removeEventListener('paddle:checkout.closed', handleCheckoutClosed)
    }
  }, [navigate])
}
