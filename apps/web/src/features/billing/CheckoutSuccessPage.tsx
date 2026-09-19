import { useEffect, useState } from 'react'
import { Link } from 'react-router'
import { useAuth } from '@clerk/react-router'
import { refreshRevenueCatSubscription } from '@boostedbudget/api-client/endpoints/subscription/subscription'
import { Button } from '@/components/ui/button'
import { useQueryClient } from '@tanstack/react-query'

/** A return URL is a request to verify a purchase, never proof of payment. */
export function CheckoutSuccessPage() {
  const { userId } = useAuth()
  const queryClient = useQueryClient()
  const [attempt, setAttempt] = useState(0)
  const [result, setResult] = useState<{ userId: string; attempt: number; confirmed: boolean }>()
  useEffect(() => {
    if (!userId) return
    let current = true
    void refreshRevenueCatSubscription().then(summary => {
      if (!current) return
      const confirmed = summary.paid?.status === 'active'
      if (confirmed) void queryClient.invalidateQueries()
      setResult({ userId, attempt, confirmed })
    }).catch(() => {
      if (current) setResult({ userId, attempt, confirmed: false })
    })
    return () => { current = false }
  }, [userId, attempt, queryClient])

  const observed = result?.userId === userId && result?.attempt === attempt ? result : undefined
  return (
    <div className="max-w-lg mx-auto p-8 space-y-6" role="status">
      <h1 className="text-3xl font-semibold">{observed?.confirmed ? 'Subscription confirmed' : 'Confirming your subscription'}</h1>
      <p>{!userId ? 'Sign in to check your subscription.' : observed?.confirmed
        ? 'Your paid subscription has been verified. View your subscription for access and billing details.'
        : observed ? 'Your purchase is awaiting confirmation. Check again before trying another purchase.'
        : 'Checking your subscription with the billing service…'}</p>
      {userId && observed && !observed.confirmed && (
        <Button onClick={() => setAttempt(value => value + 1)}>Check again</Button>
      )}
      <Link className="block underline" to="/billing/manage">View Subscription Details</Link>
    </div>
  )
}
