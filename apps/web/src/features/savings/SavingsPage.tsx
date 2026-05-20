/**
 * SavingsPage
 * Issue #954: Main page with savings goals management
 */

import { useCallback, useEffect, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { PiggyBank } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/ui/empty-state'
import { ConfirmationDialog } from '@/components/ui/confirmation-dialog'
import { ReadOnlyGuard } from '@/auth/ReadOnlyGuard'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { useSavingsGoals } from './hooks/useSavingsGoals'
import { useSavingsGoalsView } from './hooks/useSavingsGoalsView'
import type { SortKey, TabKey } from './hooks/useSavingsGoalsView'
import type { SavingsGoal } from './types'
import { SavingsAggregateHeader } from './components/SavingsAggregateHeader'
import { SavingsFiltersBar } from './components/SavingsFiltersBar'
import { SavingsGoalsGrid } from './components/SavingsGoalsGrid'
import { CreateSavingsGoalDialog } from './components/CreateSavingsGoalDialog'
import { UpdateSavingsGoalDialog } from './components/UpdateSavingsGoalDialog'

type ActiveDialog = 'none' | 'create' | 'delete'

export function SavingsPage() {
  const trackEvent = useTrackEvent()
  const location = useLocation()
  const navigate = useNavigate()

  const [activeDialog, setActiveDialog] = useState<ActiveDialog>('none')
  const [editingGoal, setEditingGoal] = useState<SavingsGoal | null>(null)
  const [deletingGoal, setDeletingGoal] = useState<SavingsGoal | null>(null)

  const { goals, goalsWithProgress, loading, error, refetch, deleteGoal } = useSavingsGoals()
  const { view, tab, setTab, sortKey, setSortKey } = useSavingsGoalsView(goalsWithProgress)

  // Fire page-viewed analytics once on mount
  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.SAVINGS_PAGE_VIEWED)
  }, [trackEvent])

  // Auto-open create dialog when navigated here with location.state.openCreateDialog
  useEffect(() => {
    if ((location.state as { openCreateDialog?: boolean } | null)?.openCreateDialog) {
      setActiveDialog('create')
      navigate(location.pathname, { replace: true, state: {} })
    }
    // intentionally run once on mount only
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const handleTabChange = useCallback(
    (newTab: TabKey) => {
      setTab(newTab)
      trackEvent(ANALYTICS_EVENTS.SAVINGS_FILTER_CHANGED, { tab: newTab })
    },
    [setTab, trackEvent],
  )

  const handleSortChange = useCallback(
    (newSort: SortKey) => {
      setSortKey(newSort)
      trackEvent(ANALYTICS_EVENTS.SAVINGS_SORT_CHANGED, { sort: newSort })
    },
    [setSortKey, trackEvent],
  )

  const handleGoalCreated = useCallback(
    (_goal: SavingsGoal) => {
      setActiveDialog('none')
      trackEvent(ANALYTICS_EVENTS.SAVINGS_GOAL_CREATED)
      void refetch()
    },
    [trackEvent, refetch],
  )

  const handleGoalUpdated = useCallback(
    (_goal: SavingsGoal) => {
      setEditingGoal(null)
      void refetch()
    },
    [refetch],
  )

  const handleDeleteClick = useCallback(
    (goalId: string) => {
      const goal = goals.find((g) => g.id === goalId)
      if (goal) {
        setDeletingGoal(goal)
        setActiveDialog('delete')
      }
    },
    [goals],
  )

  const handleConfirmDelete = useCallback(async () => {
    if (!deletingGoal) return
    try {
      await deleteGoal(deletingGoal.id)
      trackEvent(ANALYTICS_EVENTS.SAVINGS_GOAL_DELETED)
    } finally {
      setDeletingGoal(null)
      setActiveDialog('none')
    }
  }, [deletingGoal, deleteGoal, trackEvent])

  const handleCancelDelete = useCallback(() => {
    setDeletingGoal(null)
    setActiveDialog('none')
  }, [])

  if (loading) {
    return (
      <main className="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
        <div role="status" aria-label="Loading savings goals" className="flex justify-center py-16">
          <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary" />
        </div>
      </main>
    )
  }

  if (error) {
    return (
      <main className="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
        <p className="text-sm text-destructive mb-4">{error.message}</p>
        <button
          onClick={() => void refetch()}
          className="text-sm text-primary underline hover:no-underline"
        >
          Retry
        </button>
      </main>
    )
  }

  if (goals.length === 0) {
    return (
      <main className="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
        <h1 className="sr-only">Savings</h1>
        <EmptyState
          icon={<PiggyBank className="h-12 w-12" />}
          title="No savings goals yet"
          description="Create your first goal to start tracking your progress."
          action={
            <ReadOnlyGuard>
              <Button onClick={() => setActiveDialog('create')}>Create goal</Button>
            </ReadOnlyGuard>
          }
        />
        <CreateSavingsGoalDialog
          isOpen={activeDialog === 'create'}
          onCancel={() => setActiveDialog('none')}
          onGoalCreated={handleGoalCreated}
        />
      </main>
    )
  }

  const emptyMessage =
    tab === 'active'
      ? 'No active goals.'
      : tab === 'completed'
        ? 'No completed goals yet.'
        : undefined

  return (
    <main className="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
      <div className="mb-6 flex items-start justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Savings</h1>
          <p className="text-sm text-muted-foreground">All your savings goals in one place.</p>
        </div>
        <ReadOnlyGuard>
          <Button onClick={() => setActiveDialog('create')}>New goal</Button>
        </ReadOnlyGuard>
      </div>

      <SavingsAggregateHeader goals={goals} />

      <SavingsFiltersBar
        tab={tab}
        onTabChange={handleTabChange}
        sortKey={sortKey}
        onSortChange={handleSortChange}
        goalCount={view.length}
      />

      <SavingsGoalsGrid
        goals={view}
        onEdit={setEditingGoal}
        onDelete={handleDeleteClick}
        emptyMessage={emptyMessage}
      />

      <CreateSavingsGoalDialog
        isOpen={activeDialog === 'create'}
        onCancel={() => setActiveDialog('none')}
        onGoalCreated={handleGoalCreated}
      />

      <UpdateSavingsGoalDialog
        key={editingGoal?.id ?? 'none'}
        isOpen={editingGoal !== null}
        goal={editingGoal}
        onCancel={() => setEditingGoal(null)}
        onGoalUpdated={handleGoalUpdated}
      />

      <ConfirmationDialog
        open={activeDialog === 'delete'}
        onOpenChange={(open) => {
          if (!open) handleCancelDelete()
        }}
        onConfirm={() => void handleConfirmDelete()}
        title="Delete savings goal"
        description={
          deletingGoal
            ? <>Are you sure you want to delete &ldquo;<span data-ph-mask>{deletingGoal.name}</span>&rdquo;? This cannot be undone.</>
            : 'Are you sure you want to delete this goal?'
        }
        variant="destructive"
        confirmButtonText="Delete"
      />
    </main>
  )
}
