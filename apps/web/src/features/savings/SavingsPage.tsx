import { useEffect } from 'react'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'

export function SavingsPage() {
  const trackEvent = useTrackEvent()
  useEffect(() => {
    // T3 will replace this string literal with ANALYTICS_EVENTS.SAVINGS_PAGE_VIEWED
    trackEvent('savings_page_viewed')
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
