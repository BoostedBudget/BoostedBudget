import { useState, useCallback, useEffect } from 'react'
import { useAuth } from '@clerk/react-router'
import { ReadOnlyGuard } from '@/auth'
import { PlusIcon, PencilIcon, ArrowPathIcon } from '@heroicons/react/24/outline'
import { LightbulbIcon } from 'lucide-react'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { DashboardProvider, useDashboardContext } from '../context/DashboardContext'
import { useDashboardLayout } from '../hooks/useDashboardLayout'
import { useDashboardOnboarding } from '../hooks/useDashboardOnboarding'
import { useGettingStarted } from '../hooks/useGettingStarted'
import { DashboardGrid, WidgetCatalog } from '../widgets'
import { AccountTypeFilter } from './AccountTypeFilter'
import { TimeframeSelector } from './TimeframeSelector'
import { GettingStartedChecklist } from './GettingStartedChecklist'
import { useDashboardData } from '../hooks/useDashboardData'
import { useTrackEvent } from '../../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../../shared/analytics'
import type { WidgetType } from '../types'

// Import widgets to register them
import '../widgets'

/**
 * DashboardPage - Main dashboard view with customizable widget system
 *
 * Features:
 * - Drag-and-drop widget reordering
 * - Widget resizing
 * - Add/remove widgets from catalog
 * - Layout persistence to backend
 * - Cash flow analysis widgets (Issue #354)
 * - Timeframe and account type filtering
 * - Responsive grid layout
 *
 * Issue #354: Cash Flow Analysis + Customizable Dashboard
 */

function DashboardContent() {
  const { getToken } = useAuth()
  const { filters, setFilters, isEditing, setIsEditing, hasAccounts } = useDashboardContext()
  const trackEvent = useTrackEvent()

  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.DASHBOARD_VIEWED)
  }, [trackEvent])

  // Issue #470: Pass getAccessToken for authenticated save on beforeunload
  const { layout, loading, error, saving, updateLayout, addWidget, removeWidget, resetToDefault, saveNow } = useDashboardLayout({
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

  // Fetch account types for filter
  const { accountTypes } = useDashboardData({
    startDate: filters.startDate,
    endDate: filters.endDate,
    accountTypes: filters.accountTypes,
  })

  const handleAccountTypeChange = useCallback((types: string[]) => {
    setFilters({ accountTypes: types.length > 0 ? types : undefined })
  }, [setFilters])

  const handleTimeframeChange = useCallback((startDate: string, endDate: string) => {
    setFilters({ startDate, endDate })
    trackEvent(ANALYTICS_EVENTS.TIMEFRAME_CHANGED, { startDate, endDate })
  }, [setFilters, trackEvent])

  const handleResetLayout = useCallback(async () => {
    await resetToDefault()
    setIsEditing(false)
  }, [resetToDefault, setIsEditing])

  // Issue #470: Save pending changes immediately when exiting edit mode
  // This ensures changes are persisted before the user can navigate away
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
      <div className="min-h-screen bg-gray-50 dark:bg-gray-900 flex items-center justify-center">
        <div className="text-center">
          <div className="inline-block animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600"></div>
          <p className="mt-4 text-gray-600 dark:text-gray-400">Loading dashboard...</p>
        </div>
      </div>
    )
  }

  // Error state
  if (error) {
    return (
      <div className="min-h-screen bg-gray-50 dark:bg-gray-900 flex items-center justify-center">
        <div className="bg-white dark:bg-gray-800 rounded-lg shadow-lg p-8 max-w-md text-center">
          <div className="text-red-600 text-5xl mb-4">⚠️</div>
          <h2 className="text-xl font-semibold text-gray-900 dark:text-white mb-2">
            Failed to Load Dashboard
          </h2>
          <p className="text-gray-600 dark:text-gray-400 mb-6">{error}</p>
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
    <div className="min-h-screen bg-gray-50 dark:bg-gray-900 py-8">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
        {/* Header */}
        <div className="mb-8 flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
          <div>
            <h1 className="text-3xl font-bold text-gray-900 dark:text-white">Dashboard</h1>
            <p className="mt-2 text-gray-600 dark:text-gray-400">
              {isEditing ? 'Drag widgets to reorder, resize from corners' : 'Track your balance and cash flow'}
            </p>
            {/* Onboarding hint - Issue #412: only show after first bank connected */}
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
              <span className="text-sm text-gray-500 dark:text-gray-400 flex items-center gap-1">
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
                    onClick={handleResetLayout}
                    variant="outline"
                    className="gap-1.5"
                  >
                    <ArrowPathIcon className="h-4 w-4" />
                    Reset
                  </Button>
                </ReadOnlyGuard>
              </>
            )}

            {/* Issue #470: Use handleExitEditMode to save on Done click */}
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

        {/* Getting Started checklist for new users - Issue #848 */}
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

        {/* Filters Row - Issue #414: Moved from sidebar to header */}
        <div className="mb-6 flex flex-wrap items-start gap-4">
          <TimeframeSelector onRangeChange={handleTimeframeChange} />
          {accountTypes && (
            <AccountTypeFilter
              accountTypes={accountTypes}
              selectedTypes={filters.accountTypes || []}
              onChange={handleAccountTypeChange}
            />
          )}
        </div>

        {/* Widget Grid - Full Width */}
        <DashboardGrid
          layout={layout}
          isEditing={isEditing}
          onLayoutChange={updateLayout}
          onRemoveWidget={handleRemoveWidget}
        />
      </div>

      {/* Widget Catalog Modal */}
      <WidgetCatalog
        isOpen={catalogOpen}
        onClose={() => setCatalogOpen(false)}
        onAddWidget={handleAddWidget}
        existingWidgetTypes={layout.widgets.map(w => w.type)}
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
