import { useState, useCallback, useEffect } from 'react'
import { useAuth } from '@clerk/react-router'
import { ReadOnlyGuard } from '@/auth'
import { PlusIcon, PencilIcon, ArrowPathIcon } from '@heroicons/react/24/outline'
import { LightbulbIcon } from 'lucide-react'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { ConfirmationDialog } from '@/components/ui/confirmation-dialog'
import { DashboardProvider, useDashboardContext } from '../context/DashboardContext'
import { useDashboardLayout } from '../hooks/useDashboardLayout'
import { useDashboardOnboarding } from '../hooks/useDashboardOnboarding'
import { useGettingStarted } from '../hooks/useGettingStarted'
import { DashboardGrid, WidgetCatalog } from '../widgets'
import { AccountTypeFilter } from './AccountTypeFilter'
import { TimeframeSelector } from './TimeframeSelector'
import { ScopeSegmentedControl } from './ScopeSegmentedControl'
import { GettingStartedChecklist } from './GettingStartedChecklist'
import { useDashboardData } from '../hooks/useDashboardData'
import { useTrackEvent } from '../../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../../shared/analytics'
import type { WidgetType } from '../types'

// Import widgets to register them
import '../widgets'

function DashboardContent() {
  const { getToken } = useAuth()
  const { filters, setFilters, isEditing, setIsEditing, hasAccounts, scope } = useDashboardContext()
  const trackEvent = useTrackEvent()

  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.DASHBOARD_VIEWED)
  }, [trackEvent])

  const { layout, loading, error, saving, updateLayout, addWidget, removeWidget, updateWidgetSettings, resetToDefault, saveNow } = useDashboardLayout({
    getAccessToken: async () => {
      try {
        return await getToken({ template: 'BoostedBudget' })
      } catch {
        return null
      }
    },
  })

  const { showHint, dismissHint } = useDashboardOnboarding()
  const gettingStarted = useGettingStarted()
  const [catalogOpen, setCatalogOpen] = useState(false)
  const [resetConfirmOpen, setResetConfirmOpen] = useState(false)

  useEffect(() => {
    if (gettingStarted.shouldShow && !gettingStarted.isLoading) {
      trackEvent(ANALYTICS_EVENTS.GETTING_STARTED_VIEWED)
    }
  }, [gettingStarted.shouldShow, gettingStarted.isLoading, trackEvent])

  useEffect(() => {
    if (gettingStarted.isCelebrating) {
      trackEvent(ANALYTICS_EVENTS.GETTING_STARTED_COMPLETED)
    }
  }, [gettingStarted.isCelebrating, trackEvent])

  // Fetch account types for filter. accountTypes itself isn't scope-sensitive (not one
  // of the 9 scoped endpoints), but this call also fires useGetBalanceSummary/History
  // under the hood (their results are unused here) — passing `scope` keeps those
  // incidental fetches consistent with the user's current toggle selection instead of
  // always querying personal scope regardless of what's selected.
  const { accountTypes } = useDashboardData({
    startDate: filters.startDate,
    endDate: filters.endDate,
    accountTypes: filters.accountTypes,
    scope,
  })

  const handleAccountTypeChange = useCallback((types: string[]) => {
    setFilters({ accountTypes: types.length > 0 ? types : undefined })
  }, [setFilters])

  const handleTimeframeChange = useCallback((startDate: string, endDate: string) => {
    setFilters({ startDate, endDate })
    trackEvent(ANALYTICS_EVENTS.TIMEFRAME_CHANGED, { startDate, endDate })
  }, [setFilters, trackEvent])

  const handleResetConfirm = useCallback(async () => {
    const success = await resetToDefault()
    if (success) {
      setResetConfirmOpen(false)
      setIsEditing(false)
    }
  }, [resetToDefault, setIsEditing])

  const handleExitEditMode = useCallback(async () => {
    await saveNow()
    setIsEditing(false)
  }, [saveNow, setIsEditing])

  const handleAddWidget = useCallback((type: WidgetType) => {
    addWidget(type)
    trackEvent(ANALYTICS_EVENTS.WIDGET_ADDED, { widget_type: type })
  }, [addWidget, trackEvent])

  const handleRemoveWidget = useCallback((widgetId: string) => {
    removeWidget(widgetId)
    trackEvent(ANALYTICS_EVENTS.WIDGET_REMOVED, { widget_id: widgetId })
  }, [removeWidget, trackEvent])

  // Loading state
  if (loading) {
    return (
      <div className="min-h-screen bg-background flex items-center justify-center">
        <div className="text-center">
          <div className="inline-block animate-spin rounded-full h-12 w-12 border-b-2 border-primary"></div>
          <p className="mt-4 text-muted-foreground">Loading dashboard...</p>
        </div>
      </div>
    )
  }

  // Error state
  if (error) {
    return (
      <div className="min-h-screen bg-background flex items-center justify-center">
        <div className="bg-card rounded-lg shadow-lg p-8 max-w-md text-center">
          <div className="text-destructive text-5xl mb-4">⚠️</div>
          <h2 className="text-xl font-semibold text-foreground mb-2">
            Failed to Load Dashboard
          </h2>
          <p className="text-muted-foreground mb-6">{error}</p>
          <Button
            onClick={() => window.location.reload()}
          >
            Try Again
          </Button>
        </div>
      </div>
    )
  }

  return (
    <div className="min-h-screen bg-background py-8">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
        {/* Header */}
        <div className="mb-8 flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
          <div>
            <h1 className="text-3xl font-bold text-foreground">Dashboard</h1>
            <p className="mt-2 text-muted-foreground">
              {isEditing ? 'Drag widgets to reorder, resize from corners' : 'Track your balance and cash flow'}
            </p>
            {showHint && !isEditing && hasAccounts && (
              <Alert
                variant="info"
                dismissible
                onDismiss={dismissHint}
                className="mt-2 py-2"
              >
                <LightbulbIcon className="h-4 w-4" />
                <span>Tip: Click "Edit" to customize your dashboard layout</span>
              </Alert>
            )}
          </div>

          <div className="flex items-center gap-3">
            {saving && (
              <span className="text-sm text-muted-foreground flex items-center gap-1">
                <ArrowPathIcon className="h-4 w-4 animate-spin" />
                Saving...
              </span>
            )}

            {isEditing && (
              <>
                <ReadOnlyGuard message="Resubscribe to customize your dashboard">
                  <Button
                    onClick={() => setCatalogOpen(true)}
                    className="gap-1.5"
                  >
                    <PlusIcon className="h-4 w-4" />
                    Add Widget
                  </Button>
                </ReadOnlyGuard>
                <ReadOnlyGuard message="Resubscribe to customize your dashboard">
                  <Button
                    onClick={() => setResetConfirmOpen(true)}
                    variant="outline"
                    className="gap-1.5"
                  >
                    <ArrowPathIcon className="h-4 w-4" />
                    Reset
                  </Button>
                </ReadOnlyGuard>
              </>
            )}

            <ReadOnlyGuard message="Resubscribe to customize your dashboard">
              <Button
                onClick={() => (isEditing ? handleExitEditMode() : setIsEditing(true))}
                variant={isEditing ? 'default' : 'outline'}
                className="gap-1.5"
              >
                <PencilIcon className="h-4 w-4" />
                {isEditing ? 'Done' : 'Edit'}
              </Button>
            </ReadOnlyGuard>
          </div>
        </div>

        {gettingStarted.shouldShow && !gettingStarted.isLoading && (
          <div className="mb-6">
            <GettingStartedChecklist
              steps={gettingStarted.steps}
              completedCount={gettingStarted.completedCount}
              totalSteps={gettingStarted.totalSteps}
              isCelebrating={gettingStarted.isCelebrating}
              onDismiss={() => {
                gettingStarted.dismiss()
                trackEvent(ANALYTICS_EVENTS.GETTING_STARTED_DISMISSED)
              }}
              onStepClick={(stepId) => trackEvent(ANALYTICS_EVENTS.GETTING_STARTED_STEP_CLICKED, { step_id: stepId })}
            />
          </div>
        )}

        <div className="mb-6 flex flex-wrap items-start gap-4">
          <TimeframeSelector onRangeChange={handleTimeframeChange} />
          {accountTypes && (
            <AccountTypeFilter
              accountTypes={accountTypes}
              selectedTypes={filters.accountTypes || []}
              onChange={handleAccountTypeChange}
            />
          )}
          <ScopeSegmentedControl />
        </div>

        {/* Widget Grid - Full Width */}
        <DashboardGrid
          layout={layout}
          isEditing={isEditing}
          onLayoutChange={updateLayout}
          onRemoveWidget={handleRemoveWidget}
          onUpdateWidgetSettings={updateWidgetSettings}
        />
      </div>

      {/* Widget Catalog Modal */}
      <WidgetCatalog
        isOpen={catalogOpen}
        onClose={() => setCatalogOpen(false)}
        onAddWidget={handleAddWidget}
        existingWidgetTypes={layout.widgets.map(w => w.type)}
      />

      <ConfirmationDialog
        open={resetConfirmOpen}
        onOpenChange={setResetConfirmOpen}
        onConfirm={handleResetConfirm}
        title="Reset Dashboard Layout"
        description="This will restore all widgets and positions to their defaults. Your current layout will be lost."
        variant="warning"
        confirmButtonText="Reset"
        loading={saving}
      />
    </div>
  )
}

export function DashboardPage() {
  return (
    <DashboardProvider>
      <DashboardContent />
    </DashboardProvider>
  )
}
