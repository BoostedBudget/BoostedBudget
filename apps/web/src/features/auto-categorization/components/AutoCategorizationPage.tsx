/**
 * AutoCategorizationPage Component
 *
 * Main page for managing auto-categorization rules.
 * Issue #346: Custom Transaction Categorization Rules
 * Issue #584: Unified Card/Table View System
 */

import { useCallback, useMemo, useState } from 'react'
import { Tags } from 'lucide-react'
import { EmptyState } from '@/components/ui/empty-state'
import { Button } from '@/components/ui/button'
import { ConfirmationDialog } from '@/components/ui/confirmation-dialog'
import { useAutoCategorizationRules } from '../hooks/useAutoCategorizationRules'
import { useRuleFilters } from '../hooks/useRuleFilters'
import { AutoCategorizationRuleCard } from './AutoCategorizationRuleCard'
import { AutoCategorizationRuleTable, type SortColumn as RuleSortColumn, type SortDirection } from './AutoCategorizationRuleTable'
import { AutoCategorizationRuleEditor } from './AutoCategorizationRuleEditor'
import { ListViewToggle } from '@/shared/components/ListViewToggle'
import type { ListView } from '@/shared/components/ListViewToggle'
import type { FilterOption } from '@/shared/components/ColumnFilterDropdown'
import { STORAGE_KEY_LIST_VIEW } from '../constants'
import type { CategorizationRule, CreateRuleRequest, UpdateRuleRequest } from '../types'
import type { Account } from '../../accounts/types'
import type { UserTag } from '../../tags/types'
import type { Primary } from '@boostedbudget/api-client/models/primary'

export interface AutoCategorizationPageProps {
  categories: string[][]
  plaidPrimaries?: Primary[]
  accounts?: Account[]
  tags?: UserTag[]  // User tags for target tags autocomplete
  maxRules?: number
  currentTier?: string
}

type EditorMode = 'create' | 'edit' | null

// Issue #584: Get initial view from localStorage
const getInitialView = (): ListView => {
  try {
    const stored = localStorage.getItem(STORAGE_KEY_LIST_VIEW)
    if (stored === 'cards' || stored === 'table') {
      return stored
    }
  } catch {
    // Ignore localStorage errors
  }
  return 'cards'
}

export function AutoCategorizationPage({ categories, plaidPrimaries = [], accounts = [], tags = [], maxRules, currentTier }: AutoCategorizationPageProps) {
  const {
    rules,
    loading,
    error,
    createRule,
    updateRule,
    deleteRule,
    toggleRule,
    refresh,
  } = useAutoCategorizationRules()

  const [editorMode, setEditorMode] = useState<EditorMode>(null)
  const [editingRule, setEditingRule] = useState<CategorizationRule | null>(null)
  const [view, setView] = useState<ListView>(getInitialView)
  // Issue #960: Table view delete confirmation (cards view uses AutoCategorizationRuleCard's built-in dialog)
  const [tableDeleteTarget, setTableDeleteTarget] = useState<CategorizationRule | null>(null)
  const [tableDeleting, setTableDeleting] = useState(false)

  // Issue #590: Sorting state
  const [sortColumn, setSortColumn] = useState<RuleSortColumn | undefined>(undefined)
  const [sortDirection, setSortDirection] = useState<SortDirection>('asc')

  // Issue #590: Inline filter state (URL-synced)
  const {
    statusFilter,
    categoryFilter,
    priorityRange,
    setStatusFilter,
    setCategoryFilter,
    setPriorityRange,
  } = useRuleFilters()

  const isAtLimit = maxRules !== undefined && rules.length >= maxRules
  const showTierWarning = isAtLimit && currentTier === 'free'

  const handleCreateClick = useCallback(() => {
    setEditingRule(null)
    setEditorMode('create')
  }, [])

  const handleEditClick = useCallback((rule: CategorizationRule) => {
    setEditingRule(rule)
    setEditorMode('edit')
  }, [])

  const handleEditorCancel = useCallback(() => {
    setEditorMode(null)
    setEditingRule(null)
  }, [])

  const handleEditorSave = useCallback(
    async (data: CreateRuleRequest | UpdateRuleRequest) => {
      if (editorMode === 'create') {
        await createRule(data as CreateRuleRequest)
      } else if (editorMode === 'edit' && editingRule) {
        await updateRule(editingRule.id, data as UpdateRuleRequest)
      }
      setEditorMode(null)
      setEditingRule(null)
    },
    [editorMode, editingRule, createRule, updateRule]
  )

  const handleDelete = useCallback(
    async (ruleId: string) => {
      await deleteRule(ruleId)
    },
    [deleteRule]
  )

  const handleToggle = useCallback(
    async (ruleId: string, enabled: boolean) => {
      await toggleRule(ruleId, enabled)
    },
    [toggleRule]
  )

  // Issue #584: Handle toggle from table component (takes rule object instead of ruleId)
  const handleTableToggle = useCallback(
    async (rule: CategorizationRule, enabled: boolean) => {
      await toggleRule(rule.id, enabled)
    },
    [toggleRule]
  )

  // Issue #584: Handle view change
  const handleViewChange = useCallback((newView: ListView) => {
    setView(newView)
  }, [])

  // Issue #590: Handle sort change
  const handleSort = useCallback((column: RuleSortColumn, direction: SortDirection) => {
    setSortColumn(column)
    setSortDirection(direction)
  }, [])

  // Issue #590: Create filter options from rules data
  const statusOptions = useMemo<FilterOption[]>(() => [
    { value: 'enabled', label: 'Enabled' },
    { value: 'disabled', label: 'Disabled' },
  ], [])

  const categoryOptions = useMemo<FilterOption[]>(() => {
    // Build filter options: value = raw enum/id (for filtering), label = display text
    const seen = new Map<string, string>()
    rules.forEach((rule) => {
      if (rule.target_kind === 'pfc' && rule.target_pfc_primary) {
        const value = rule.target_pfc_primary
        if (!seen.has(value)) {
          const label = rule.target_pfc_primary_display ?? rule.target_pfc_primary
          seen.set(value, label)
        }
      } else if (rule.target_kind === 'custom' && rule.target_custom_category_id) {
        const value = rule.target_custom_category_id
        if (!seen.has(value)) seen.set(value, rule.target_custom_category_display ?? value)
      }
    })
    return Array.from(seen.entries())
      .sort(([, a], [, b]) => a.localeCompare(b))
      .map(([value, label]) => ({ value, label }))
  }, [rules])

  // Issue #590: Filter handlers from useRuleFilters hook (URL-synced)
  // setStatusFilter, setCategoryFilter, setPriorityRange come from the hook

  // Issue #590: Filter and sort rules client-side
  const filteredAndSortedRules = useMemo(() => {
    let filtered = [...rules]

    // Apply status filter
    if (statusFilter.length > 0) {
      filtered = filtered.filter((rule) => {
        const status = rule.enabled ? 'enabled' : 'disabled'
        return statusFilter.includes(status)
      })
    }

    // Apply category filter — match either PFC primary or custom category id
    if (categoryFilter.length > 0) {
      filtered = filtered.filter((rule) => {
        if (rule.target_kind === 'pfc' && rule.target_pfc_primary) {
          return categoryFilter.includes(rule.target_pfc_primary)
        }
        if (rule.target_kind === 'custom' && rule.target_custom_category_id) {
          return categoryFilter.includes(rule.target_custom_category_id)
        }
        return false
      })
    }

    // Apply priority range filter
    if (priorityRange) {
      filtered = filtered.filter((rule) => {
        const priority = rule.priority
        if (priorityRange.min !== undefined && priority < priorityRange.min) return false
        if (priorityRange.max !== undefined && priority > priorityRange.max) return false
        return true
      })
    }

    // Apply sorting if sortColumn is set
    if (!sortColumn) return filtered

    return filtered.sort((a, b) => {
      let comparison = 0

      switch (sortColumn) {
        case 'name': {
          comparison = a.name.localeCompare(b.name)
          break
        }
        case 'status': {
          comparison = (a.enabled ? 1 : 0) - (b.enabled ? 1 : 0)
          break
        }
        case 'priority': {
          comparison = a.priority - b.priority
          break
        }
        case 'matches': {
          comparison = a.match_count - b.match_count
          break
        }
        case 'category': {
          const ruleTargetKey = (r: typeof a): string => {
            if (r.target_kind === 'pfc' && r.target_pfc_primary) return r.target_pfc_primary
            if (r.target_kind === 'custom' && r.target_custom_category_id) return r.target_custom_category_display ?? r.target_custom_category_id
            return ''
          }
          comparison = ruleTargetKey(a).localeCompare(ruleTargetKey(b))
          break
        }
      }

      return sortDirection === 'asc' ? comparison : -comparison
    })
  }, [rules, sortColumn, sortDirection, statusFilter, categoryFilter, priorityRange])

  // Loading state
  if (loading) {
    return (
      <div className="max-w-[800px] mx-auto py-8 px-4">
        <div className="flex flex-col items-center justify-center py-16 px-8 text-center text-muted-foreground">
          <div className="w-10 h-10 border-[3px] border-border border-t-primary rounded-full animate-spin mb-4" />
          <p>Loading rules...</p>
        </div>
      </div>
    )
  }

  // Error state
  if (error) {
    return (
      <div className="max-w-[800px] mx-auto py-8 px-4">
        <div className="flex flex-col items-center py-12 px-8 text-center bg-destructive/10 rounded-lg">
          <p className="m-0 mb-4 text-destructive">{error}</p>
          <button
            type="button"
            className="px-4 py-2 text-sm font-medium text-foreground bg-background border border-border rounded-md cursor-pointer hover:bg-muted transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            onClick={refresh}
          >
            Retry
          </button>
        </div>
      </div>
    )
  }

  // Editor mode
  if (editorMode) {
    return (
      <div className="max-w-[800px] mx-auto py-8 px-4">
        <AutoCategorizationRuleEditor
          mode={editorMode}
          rule={editingRule ?? undefined}
          onSave={handleEditorSave}
          onCancel={handleEditorCancel}
          categories={categories}
          plaidPrimaries={plaidPrimaries}
          accounts={accounts}
          tags={tags}
        />
      </div>
    )
  }

  // Empty state
  if (rules.length === 0) {
    return (
      <div className="max-w-[800px] mx-auto py-8 px-4">
        <EmptyState
          icon={<Tags />}
          title="No rules yet"
          description="Create auto-categorization rules to automatically organize your transactions."
          action={
            <Button onClick={handleCreateClick}>
              Create Your First Rule
            </Button>
          }
        />
      </div>
    )
  }

  // Rules list
  return (
    <div className="max-w-[800px] mx-auto py-8 px-4">
      <div className="flex items-center justify-between mb-6">
        <div className="flex items-baseline gap-3">
          <h1 className="m-0 text-2xl font-semibold text-foreground">Auto-Categorization Rules</h1>
          <span className="text-sm text-muted-foreground">{rules.length} rules</span>
        </div>
        {/* Issue #584: View toggle and actions */}
        <div className="flex items-center gap-3">
          <ListViewToggle
            view={view}
            onChange={handleViewChange}
            storageKey={STORAGE_KEY_LIST_VIEW}
          />
          <button
            type="button"
            className="px-4 py-2 text-sm font-medium bg-primary text-primary-foreground border border-transparent rounded-md cursor-pointer hover:bg-primary/90 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            onClick={handleCreateClick}
            disabled={isAtLimit}
          >
            Create Rule
          </button>
        </div>
      </div>

      {showTierWarning && (
        <div className="flex items-center justify-between gap-4 px-4 py-3 mb-4 bg-warning/10 border border-warning/30 rounded-md">
          <p className="m-0 text-sm text-warning-foreground">
            <strong>Rule limit reached.</strong> You&apos;ve reached the maximum of{' '}
            {maxRules} rule{maxRules !== 1 ? 's' : ''} for the free tier.
          </p>
          <a href="/settings/subscription" className="flex-shrink-0 text-sm font-medium text-primary hover:underline">
            Upgrade for more rules
          </a>
        </div>
      )}

      {/* Issue #584: Conditional cards/table rendering */}
      <div className="mt-4">
        {view === 'cards' ? (
          <div className="flex flex-col gap-0">
            {rules.map((rule) => (
              <AutoCategorizationRuleCard
                key={rule.id}
                rule={rule}
                onToggle={(enabled) => handleToggle(rule.id, enabled)}
                onEdit={() => handleEditClick(rule)}
                onDelete={() => handleDelete(rule.id)}
              />
            ))}
          </div>
        ) : (
          <AutoCategorizationRuleTable
            rules={filteredAndSortedRules}
            onToggle={handleTableToggle}
            onEdit={handleEditClick}
            onDelete={(rule) => setTableDeleteTarget(rule)}
            loading={loading}
            sortable
            sortColumn={sortColumn}
            sortDirection={sortDirection}
            onSort={handleSort}
            // Issue #590: Inline filter props (URL-synced via useRuleFilters)
            statusOptions={statusOptions}
            statusFilter={statusFilter}
            onStatusFilterChange={setStatusFilter}
            categoryOptions={categoryOptions}
            categoryFilter={categoryFilter}
            onCategoryFilterChange={setCategoryFilter}
            priorityRange={priorityRange}
            onPriorityRangeChange={setPriorityRange}
          />
        )}
      </div>

      {/* Issue #960: Table view delete confirmation */}
      <ConfirmationDialog
        open={tableDeleteTarget !== null}
        onOpenChange={(open) => !open && !tableDeleting && setTableDeleteTarget(null)}
        onConfirm={async () => {
          if (!tableDeleteTarget) return
          setTableDeleting(true)
          try {
            await handleDelete(tableDeleteTarget.id)
            setTableDeleteTarget(null)
          } finally {
            setTableDeleting(false)
          }
        }}
        title="Delete Rule"
        description={tableDeleteTarget ? `Are you sure you want to delete "${tableDeleteTarget.name}"? This action cannot be undone.` : ''}
        variant="destructive"
        loading={tableDeleting}
      />
    </div>
  )
}
