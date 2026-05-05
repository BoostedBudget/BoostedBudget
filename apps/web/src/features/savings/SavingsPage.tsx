import { useEffect } from 'react'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'

export function SavingsPage() {
  const trackEvent = useTrackEvent()
  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.SAVINGS_PAGE_VIEWED)
  }, [trackEvent])

  return (
    <main className="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
      <div className="mb-6">
        <h1 className="text-2xl font-bold tracking-tight">Savings</h1>
        <p className="text-sm text-muted-foreground">All your savings goals in one place.</p>
      </div>
    </main>
  )
}
