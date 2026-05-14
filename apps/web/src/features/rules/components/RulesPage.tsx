/**
 * RulesPage Component
 * Issue #39: Rule Builder UI
 *
 * Main page for viewing and managing automation rules.
 * Supports card and table views.
 */

import { useCallback, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTrackEvent } from '../../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../../shared/analytics'
import { useQueryClient } from '@tanstack/react-query'
import { AlertCircle, Loader2, Plus, Zap } from 'lucide-react'
import { EmptyState } from '@/components/ui/empty-state'
import { ConfirmationDialog } from '@/components/ui/confirmation-dialog'
import { Button } from '@/components/ui/button'
import {
  useGetAutomationRules,
  useToggleAutomationRule,
  useDeleteAutomationRule,
  useTestAutomationRule,
  getGetAutomationRulesQueryKey,
} from '@boostedbudget/api-client/endpoints/automation-rules/automation-rules'
import type { AutomationRule } from '@boostedbudget/api-client/models'
import { ListViewToggle } from '@/shared/components/ListViewToggle'
import type { ListView } from '@/shared/components/ListViewToggle'
import { STORAGE_KEY_LIST_VIEW } from '../constants'
import { ReadOnlyGuard } from '@/auth/ReadOnlyGuard'
import { RuleCard } from './RuleCard'
import { RuleTable, type SortColumn as RuleSortColumn, type SortDirection } from './RuleTable'
import { useRuleFilters } from '../hooks/useRuleFilters'

/**
 * RulesPage displays the list of automation rules with card/table toggle.
 */
export function RulesPage() {
  const trackEvent = useTrackEvent()
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const [view, setView] = useState<ListView>(() => {
    try {
      const stored = localStorage.getItem(STORAGE_KEY_LIST_VIEW)
      if (stored === 'cards' || stored === 'table') {
        return stored
      }
    } catch {
      // Ignore localStorage errors
    }
    return 'cards'
  })
  const [deleteConfirm, setDeleteConfirm] = useState<AutomationRule | null>(null)

  const { sort, setSort } = useRuleFilters()
  const sortColumn = sort?.columnId as RuleSortColumn | undefined
  const sortDirection: SortDirection = sort?.direction || 'asc'

  // Fetch rules
  const {
    data,
    isLoading,
    error,
    refetch,
  } = useGetAutomationRules()

  const rules = data ?? []

  // Mutations
  // Issue #579: Optimistic updates for toggle - UI updates immediately, rollback on error
  const toggleMutation = useToggleAutomationRule({
    mutation: {
      onMutate: async ({ id, data }) => {
        // Cancel any outgoing refetches to avoid overwriting optimistic update
        await queryClient.cancelQueries({ queryKey: getGetAutomationRulesQueryKey() })
        // Snapshot the previous value for rollback
        const previousRules = queryClient.getQueryData(getGetAutomationRulesQueryKey())
        // Optimistically update the cache
        queryClient.setQueryData(
          getGetAutomationRulesQueryKey(),
          (old: AutomationRule[] | undefined) =>
            old?.map((rule) =>
              rule.id === id ? { ...rule, enabled: data.enabled } : rule
            )
        )
        // Return context with snapshot for rollback
        return { previousRules }
      },
      onError: (_err, _variables, context) => {
        // Rollback to previous value on error
        if (context?.previousRules) {
          queryClient.setQueryData(
            getGetAutomationRulesQueryKey(),
            context.previousRules
          )
        }
      },
      onSettled: () => {
        // Always refetch after error or success to ensure cache is in sync
        queryClient.invalidateQueries({ queryKey: getGetAutomationRulesQueryKey() })
      },
    },
  })
  const deleteMutation = useDeleteAutomationRule()
  const testMutation = useTestAutomationRule()

  // Handlers
  const handleCreateRule = useCallback(() => {
    navigate('/rules/new')
  }, [navigate])

  const handleEditRule = useCallback(
    (rule: AutomationRule) => {
      navigate(`/rules/${rule.id}/edit`)
    },
    [navigate]
  )

  const handleToggleRule = useCallback(
    (rule: AutomationRule, enabled: boolean) => {
      toggleMutation.mutate({
        id: rule.id,
        data: { enabled },
      })
    },
    [toggleMutation]
  )

  const handleDeleteClick = useCallback((rule: AutomationRule) => {
    setDeleteConfirm(rule)
  }, [])

  const handleDeleteConfirm = useCallback(() => {
    if (deleteConfirm) {
      deleteMutation.mutate(
        { id: deleteConfirm.id },
        {
          onSuccess: () => {
            trackEvent(ANALYTICS_EVENTS.RULE_DELETED)
            setDeleteConfirm(null)
            refetch()
          },
        }
      )
    }
  }, [deleteConfirm, deleteMutation, trackEvent, refetch])

  const handleDeleteCancel = useCallback(() => {
    setDeleteConfirm(null)
  }, [])

  const handleSort = useCallback((column: RuleSortColumn, direction: SortDirection) => {
    setSort({ columnId: column, direction })
  }, [setSort])

  const sortedRules = useMemo(() => {
    if (!sortColumn) return rules
    return [...rules].sort((a, b) => {
      let comparison = 0
      switch (sortColumn) {
        case 'name':
          comparison = a.name.localeCompare(b.name)
          break
        case 'type':
          comparison = a.rule_type.localeCompare(b.rule_type)
          break
        case 'status':
          comparison = Number(a.enabled) - Number(b.enabled)
          break
        case 'trigger':
          comparison = (a.trigger_config?.type ?? '').localeCompare(b.trigger_config?.type ?? '')
          break
        case 'lastRun':
          comparison = (a.last_executed_at ?? '').localeCompare(b.last_executed_at ?? '')
          break
        case 'priority':
          comparison = a.priority - b.priority
          break
      }
      return sortDirection === 'asc' ? comparison : -comparison
    })
  }, [rules, sortColumn, sortDirection])

  const handleViewChange = useCallback((newView: ListView) => {
    setView(newView)
  }, [])

  const handleTestRule = useCallback(
    (rule: AutomationRule) => {
      testMutation.mutate({ id: rule.id, data: {} })
    },
    [testMutation]
  )

  // Loading state
  if (isLoading) {
    return (
      <div className="mx-auto max-w-[1200px] p-6">
        <div className="flex flex-col items-center justify-center px-6 py-16 text-center text-muted-foreground">
          <Loader2 className="mb-4 h-8 w-8 animate-spin" />
          <p>Loading automation rules...</p>
        </div>
      </div>
    )
  }

  // Error state
  if (error) {
    return (
      <div className="mx-auto max-w-[1200px] p-6">
        <div className="flex flex-col items-center justify-center px-6 py-16 text-center">
          <AlertCircle className="mb-4 h-12 w-12 text-destructive" />
          <h2 className="mb-2 text-xl font-semibold text-foreground">Error loading rules</h2>
          <p className="mb-6 text-muted-foreground">There was a problem loading your automation rules.</p>
          <button
            type="button"
            onClick={() => refetch()}
            className="rounded-lg border border-blue-200 bg-blue-50 px-5 py-2.5 text-sm font-medium text-blue-700 transition-colors hover:bg-blue-100 dark:border-blue-700 dark:bg-blue-900 dark:text-blue-300 dark:hover:bg-blue-800"
            aria-label="Retry loading rules"
          >
            Retry
          </button>
        </div>
      </div>
    )
  }

  // Empty state
  if (rules.length === 0) {
    return (
      <div className="mx-auto max-w-[1200px] p-6">
        <header className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <h1 className="text-2xl font-semibold text-foreground">Automation Rules</h1>
        </header>
        <EmptyState
          icon={<Zap />}
          title="No automation rules yet"
          description="Create your first automation rule to automatically manage your budgets, transfers, and savings."
          action={
            <ReadOnlyGuard>
              <Button onClick={handleCreateRule}>
                <Plus />
                Create Your First Rule
              </Button>
            </ReadOnlyGuard>
          }
        />
      </div>
    )
  }

  return (
    <div className="mx-auto max-w-[1200px] p-6">
      <header className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-4">
          <h1 className="text-2xl font-semibold text-foreground">Automation Rules</h1>
          <span className="rounded-full bg-blue-50 px-3 py-1 text-sm font-medium text-blue-700 dark:bg-blue-900 dark:text-blue-300">
            {rules.length} {rules.length === 1 ? 'rule' : 'rules'}
          </span>
        </div>

        <div className="flex items-center gap-4">
          <ListViewToggle
            view={view}
            onChange={handleViewChange}
            storageKey={STORAGE_KEY_LIST_VIEW}
          />
          <ReadOnlyGuard>
            <button
              type="button"
              onClick={handleCreateRule}
              className="inline-flex items-center gap-2 rounded-lg bg-primary px-4 py-2.5 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-50 [&>svg]:h-4 [&>svg]:w-4"
              aria-label="Create new rule"
            >
              <Plus aria-hidden="true" />
              Create Rule
            </button>
          </ReadOnlyGuard>
        </div>
      </header>

      <div className="min-h-[200px]">
        {view === 'cards' ? (
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {rules.map((rule) => (
              <RuleCard
                key={rule.id}
                rule={rule}
                onEdit={handleEditRule}
                onDelete={handleDeleteClick}
                onToggle={handleToggleRule}
                onTest={handleTestRule}
                loading={toggleMutation.isPending || deleteMutation.isPending || testMutation.isPending}
              />
            ))}
          </div>
        ) : (
          <RuleTable
            rules={sortedRules}
            onEdit={handleEditRule}
            onDelete={handleDeleteClick}
            onToggle={handleToggleRule}
            onTest={handleTestRule}
            loading={toggleMutation.isPending || deleteMutation.isPending || testMutation.isPending}
            sortable
            sortColumn={sortColumn}
            sortDirection={sortDirection}
            onSort={handleSort}
          />
        )}
      </div>

      {/* Delete Confirmation Dialog — Issue #960 */}
      <ConfirmationDialog
        open={deleteConfirm !== null}
        onOpenChange={(open) => !open && handleDeleteCancel()}
        onConfirm={handleDeleteConfirm}
        title="Delete Rule"
        description={deleteConfirm ? `Are you sure you want to delete "${deleteConfirm.name}"? This action cannot be undone.` : ''}
        variant="destructive"
        loading={deleteMutation.isPending}
      />
    </div>
  )
}
