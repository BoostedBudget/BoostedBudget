// Issue #1300: Planning tab — consolidates Budgets + Savings under one nav item.
// Sub-tabs driven by ?tab= URL param so URLs are bookmarkable/shareable.
import { useEffect, useRef } from 'react'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { useTabState } from '@/shared/hooks/useTabState'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import { BudgetsPage } from '../budgets'
import { SavingsPage } from '../savings'

const PLANNING_TABS = ['budgets', 'savings'] as const
type PlanningTab = (typeof PLANNING_TABS)[number]

const TAB_LABELS: Record<PlanningTab, string> = {
  budgets: 'Budgets',
  savings: 'Savings',
}

export function PlanningPage() {
  const trackEvent = useTrackEvent()
  const pageViewFired = useRef(false)
  const [activeTab, setActiveTab] = useTabState<PlanningTab>('tab', 'budgets', PLANNING_TABS)

  useEffect(() => {
    if (!pageViewFired.current) {
      pageViewFired.current = true
      trackEvent(ANALYTICS_EVENTS.PLANNING_PAGE_VIEWED, { tab: activeTab })
    }
  }, [activeTab, trackEvent])

  const handleTabChange = (tab: string) => {
    if (!PLANNING_TABS.includes(tab as PlanningTab)) return
    setActiveTab(tab as PlanningTab)
    trackEvent(ANALYTICS_EVENTS.PLANNING_TAB_CHANGED, { tab })
  }

  return (
    <div className="container mx-auto px-4 py-6 space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">Planning</h1>
        <p className="text-muted-foreground mt-1">Manage your budgets and savings goals in one place.</p>
      </div>

      <Tabs value={activeTab} onValueChange={handleTabChange}>
        <TabsList>
          {PLANNING_TABS.map((tab) => (
            <TabsTrigger key={tab} value={tab}>
              {TAB_LABELS[tab]}
            </TabsTrigger>
          ))}
        </TabsList>

        <TabsContent value="budgets">
          <div className="[&_h1]:hidden [&_.container]:px-0 [&_.py-6]:pt-0">
            <BudgetsPage />
          </div>
        </TabsContent>

        <TabsContent value="savings">
          <div className="[&_h1]:hidden [&_.container]:px-0 [&_.py-6]:pt-0">
            <SavingsPage />
          </div>
        </TabsContent>
      </Tabs>
    </div>
  )
}
