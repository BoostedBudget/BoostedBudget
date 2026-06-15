// Issue #1300: Insights tab — consolidates Reports + Net Worth under one nav item.
// Sub-tabs driven by ?tab= URL param so URLs are bookmarkable/shareable.
import { useEffect, useRef } from 'react'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { useTabState } from '@/shared/hooks/useTabState'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import { SpendingReportsPage } from '../reports'
import { NetWorthPage } from '../net-worth'
import { PortfolioPage } from '../investments/PortfolioPage'

const INSIGHTS_TABS = ['spending', 'net-worth', 'portfolio'] as const
type InsightsTab = (typeof INSIGHTS_TABS)[number]

const TAB_LABELS: Record<InsightsTab, string> = {
  'spending': 'Spending',
  'net-worth': 'Net Worth',
  'portfolio': 'Portfolio',
}

export function InsightsPage() {
  const trackEvent = useTrackEvent()
  const pageViewFired = useRef(false)
  const [activeTab, setActiveTab] = useTabState<InsightsTab>('tab', 'spending', INSIGHTS_TABS)

  useEffect(() => {
    if (!pageViewFired.current) {
      pageViewFired.current = true
      trackEvent(ANALYTICS_EVENTS.INSIGHTS_PAGE_VIEWED, { tab: activeTab })
    }
  }, [activeTab, trackEvent])

  const handleTabChange = (tab: string) => {
    if (!INSIGHTS_TABS.includes(tab as InsightsTab)) return
    setActiveTab(tab as InsightsTab)
    trackEvent(ANALYTICS_EVENTS.INSIGHTS_TAB_CHANGED, { tab })
  }

  return (
    <div className="container mx-auto px-4 py-6 space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">Insights</h1>
        <p className="text-muted-foreground mt-1">Understand your spending patterns, track your net worth, and monitor your investment portfolio.</p>
      </div>

      <Tabs value={activeTab} onValueChange={handleTabChange}>
        <TabsList>
          {INSIGHTS_TABS.map((tab) => (
            <TabsTrigger key={tab} value={tab}>
              {TAB_LABELS[tab]}
            </TabsTrigger>
          ))}
        </TabsList>

        <TabsContent value="spending">
          <div className="[&_h1]:hidden [&_.container]:px-0 [&_.py-6]:pt-0">
            <SpendingReportsPage />
          </div>
        </TabsContent>

        <TabsContent value="net-worth">
          <div className="[&_h1]:hidden [&_.container]:px-0 [&_.py-6]:pt-0">
            <NetWorthPage />
          </div>
        </TabsContent>

        <TabsContent value="portfolio">
          <div className="[&_h1]:hidden [&_.container]:px-0 [&_.py-6]:pt-0">
            <PortfolioPage />
          </div>
        </TabsContent>
      </Tabs>
    </div>
  )
}
