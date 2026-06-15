// Issue #1267: Boosts page — unified control center for transfer suggestions + rules management
// Issue #1300: "Create Boost" button always visible above tabs for discoverability
import { useState, useEffect, useRef, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { Plus } from 'lucide-react'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { Button } from '@/components/ui/button'
import { ReadOnlyGuard } from '@/auth/ReadOnlyGuard'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import {
  BOOSTS_TABS,
  BOOSTS_TAB_STORAGE_KEY,
  TAB_LABELS,
  HISTORY_PAGE_SIZE,
  isBoostsTab,
} from '../constants'
import type { BoostsTab } from '../constants'
import { useBoostsSummary } from '../hooks/useBoostsSummary'
import { useBoostSuggestions } from '../hooks/useBoostSuggestions'
import { useBoostSuggestionActions } from '../hooks/useBoostSuggestionActions'
import { SavingsSummaryHeader } from './SavingsSummaryHeader'
import { ActiveRecommendationsTab } from './ActiveRecommendationsTab'
import { AwaitingMatchTab } from './AwaitingMatchTab'
import { HistoryTab } from './HistoryTab'
import { BoostsTab as BoostsTabPanel } from './BoostsTab'

function getInitialTab(): BoostsTab {
  try {
    const urlTab = new URLSearchParams(window.location.search).get('tab')
    if (isBoostsTab(urlTab)) return urlTab
    const stored = localStorage.getItem(BOOSTS_TAB_STORAGE_KEY)
    if (isBoostsTab(stored)) return stored
  } catch {
    // Ignore storage/URL errors
  }
  return 'active'
}

export function BoostsPage() {
  const trackEvent = useTrackEvent()
  const navigate = useNavigate()
  const pageViewFired = useRef(false)
  const [activeTab, setActiveTab] = useState<BoostsTab>(getInitialTab)
  const [historyOffset, setHistoryOffset] = useState(0)

  const handleCreateBoost = useCallback(() => {
    trackEvent(ANALYTICS_EVENTS.BOOSTS_CREATE_CLICKED, { source: 'page_header' })
    navigate('/boosts/new')
  }, [navigate, trackEvent])

  useEffect(() => {
    if (!pageViewFired.current) {
      pageViewFired.current = true
      trackEvent(ANALYTICS_EVENTS.BOOSTS_PAGE_VIEWED, { tab: activeTab })
    }
  }, [activeTab, trackEvent])

  const handleTabChange = (tab: string) => {
    if (!isBoostsTab(tab)) return
    setActiveTab(tab)
    trackEvent(ANALYTICS_EVENTS.BOOSTS_TAB_CHANGED, { tab })
    try {
      localStorage.setItem(BOOSTS_TAB_STORAGE_KEY, tab)
      const url = new URL(window.location.href)
      url.searchParams.set('tab', tab)
      window.history.replaceState(null, '', url.toString())
    } catch {
      // Ignore storage/URL errors
    }
  }

  const summary = useBoostsSummary()
  const { accept, reject, confirmMatch, cancelAccepted, isPending } = useBoostSuggestionActions()

  const { items: activeItems, isLoading: activeLoading, error: activeError, refetch: refetchActive } = useBoostSuggestions({
    status: 'pending',
    limit: 20,
    offset: 0,
  })

  const { items: awaitingItems, isLoading: awaitingLoading, error: awaitingError, refetch: refetchAwaiting } = useBoostSuggestions({
    status: 'accepted',
    limit: 20,
    offset: 0,
  })

  const { items: historyItems, total: historyTotal, isLoading: historyLoading, error: historyError, refetch: refetchHistory } = useBoostSuggestions({
    status: 'completed,rejected,expired',
    limit: HISTORY_PAGE_SIZE,
    offset: historyOffset,
  })

  return (
    <div className="flex flex-col gap-6 p-6">
      <div className="flex items-center justify-between gap-4">
        <h1 className="text-2xl font-semibold text-foreground">Boosts</h1>
        {/* Issue #1300: always-visible Create Boost button for discoverability */}
        <ReadOnlyGuard>
          <Button onClick={handleCreateBoost} aria-label="Create new boost">
            <Plus aria-hidden="true" />
            Create Boost
          </Button>
        </ReadOnlyGuard>
      </div>

      <SavingsSummaryHeader
        totalMoved={summary.totalMoved}
        momChange={summary.momChange}
        completedCount={summary.completedCount}
        activeBoostsCount={summary.activeBoostsCount}
        isLoading={summary.isLoading}
        isError={summary.error !== null}
        onRetry={summary.refetch}
      />

      <Tabs value={activeTab} onValueChange={handleTabChange}>
        <TabsList>
          {BOOSTS_TABS.map((tab) => (
            <TabsTrigger key={tab} value={tab}>
              {TAB_LABELS[tab]}
            </TabsTrigger>
          ))}
        </TabsList>

        <TabsContent value="active">
          <ActiveRecommendationsTab
            items={activeItems}
            isLoading={activeLoading}
            onAccept={accept}
            onReject={reject}
            isPending={isPending}
            error={activeError}
            onRetry={refetchActive}
          />
        </TabsContent>

        <TabsContent value="awaiting">
          <AwaitingMatchTab
            items={awaitingItems}
            isLoading={awaitingLoading}
            onConfirmMatch={confirmMatch}
            onCancelAccepted={cancelAccepted}
            isPending={isPending}
            error={awaitingError}
            onRetry={refetchAwaiting}
          />
        </TabsContent>

        <TabsContent value="history">
          <HistoryTab
            items={historyItems}
            total={historyTotal}
            limit={HISTORY_PAGE_SIZE}
            offset={historyOffset}
            isLoading={historyLoading}
            onPageChange={setHistoryOffset}
            error={historyError}
            onRetry={refetchHistory}
          />
        </TabsContent>

        <TabsContent value="boosts">
          <BoostsTabPanel />
        </TabsContent>
      </Tabs>
    </div>
  )
}
