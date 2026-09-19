import { Link } from 'react-router'
import { Button } from '@/components/ui/button'
import { useRevenueCatCheckout } from '../hooks/useRevenueCatCheckout'

export function RevenueCatCheckoutStatus() {
  const { state, retry } = useRevenueCatCheckout()
  if (state === 'idle' || state === 'open' || state === 'loading') return null
  return (
    <div role="status" className="rounded-lg border p-4 mb-6 space-y-2">
      {state === 'confirmed' ? <>
        <p>Your paid subscription is confirmed.</p>
        <Link to="/billing/manage">View Subscription Details</Link>
      </> : state === 'cancelled' ? <p>Checkout was cancelled.</p> : <>
        <p>{state === 'pending'
          ? 'Your purchase is awaiting confirmation. Check its status before trying another purchase.'
          : 'Checkout is unavailable. Check your subscription status, or reload after switching accounts.'}</p>
        <Button variant="outline" onClick={() => void retry()}>Check subscription status</Button>
      </>}
    </div>
  )
}
