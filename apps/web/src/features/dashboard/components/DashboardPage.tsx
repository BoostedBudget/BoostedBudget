import { useState, useCallback, useMemo } from 'react'
import { Link } from 'react-router'
import { useAuth } from '@clerk/react-router'
import { PlusIcon, PencilIcon, ArrowPathIcon } from '@heroicons/react/24/outline'
import { LightbulbIcon, Landmark, Plus } from 'lucide-react'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/ui/empty-state'
import { DashboardProvider, useDashboardContext } from '../context/DashboardContext'
import { useDashboardLayout } from '../hooks/useDashboardLayout'
import { useDashboardOnboarding } from '../hooks/useDashboardOnboarding'
import { DashboardGrid, WidgetCatalog } from '../widgets'
import { AccountTypeFilter } from './AccountTypeFilter'
import { TimeframeSelector } from './TimeframeSelector'
import { useDashboardData } from '../hooks/useDashboardData'

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
  const { filters, setFilters, isEditing, setIsEditing, hasAccounts, accountsLoading } = useDashboardContext()

  // Issue #470: Pass getAccessToken for authenticated save on beforeunload
  const { layout, loading, error, saving, updateLayout, addWidget, removeWidget, resetToDefault, saveNow } = useDashboardLayout({
    getAccessToken: async () => {
      try {
        return await getToken({ template: 'BoostedBudget' })
      } catch {
        return null
      }
    }
  })

  const { showHint, dismissHint } = useDashboardOnboarding()
  const [catalogOpen, setCatalogOpen] = useState(false)

  // Fetch account types for filter - useMemo to prevent infinite re-renders
  const dashboardFilters = useMemo(() => ({
    startDate: filters.startDate,
    endDate: filters.endDate,
    accountTypes: filters.accountTypes,
  }), [filters.startDate, filters.endDate, filters.accountTypes])
  const { accountTypes } = useDashboardData(dashboardFilters)

  const handleAccountTypeChange = useCallback((types: string[]) => {
    setFilters({ accountTypes: types.length > 0 ? types : undefined })
  }, [setFilters])

  const handleTimeframeChange = useCallback((startDate: string, endDate: string) => {
    setFilters({ startDate, endDate })
  }, [setFilters])

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

  // Get existing widget types for catalog
  const existingWidgetTypes = layout.widgets.map(w => w.type)

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
            {/* Onboarding hint for first-time users - Issue #412 */}
            {showHint && !isEditing && (
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
                <Button
                  onClick={() => setCatalogOpen(true)}
                  className="gap-1.5"
                >
                  <PlusIcon className="h-4 w-4" />
                  Add Widget
                </Button>
                <Button
                  onClick={handleResetLayout}
                  variant="outline"
                  className="gap-1.5"
                >
                  <ArrowPathIcon className="h-4 w-4" />
                  Reset
                </Button>
              </>
            )}

            {/* Issue #470: Use handleExitEditMode to save on Done click */}
            <Button
              onClick={() => isEditing ? handleExitEditMode() : setIsEditing(true)}
              variant={isEditing ? 'default' : 'outline'}
              className="gap-1.5"
            >
              <PencilIcon className="h-4 w-4" />
              {isEditing ? 'Done' : 'Edit'}
            </Button>
          </div>
        </div>

        {/* Welcome banner for new users with no accounts - Issue #834 */}
        {!hasAccounts && !accountsLoading && (
          <div className="mb-6">
            <EmptyState
              icon={<Landmark />}
              title="Welcome to BoostedBudget!"
              description="Connect your first bank account to start tracking your finances"
              action={
                <Button asChild>
                  <Link to="/accounts">
                    <Plus className="h-4 w-4" />
                    Connect Bank Account
                  </Link>
                </Button>
              }
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
          onRemoveWidget={removeWidget}
        />
      </div>

      {/* Widget Catalog Modal */}
      <WidgetCatalog
        isOpen={catalogOpen}
        onClose={() => setCatalogOpen(false)}
        onAddWidget={addWidget}
        existingWidgetTypes={existingWidgetTypes}
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
