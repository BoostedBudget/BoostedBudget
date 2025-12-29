/**
 * AutoCategorizationPage Component
 *
 * Main page for managing auto-categorization rules.
 * Issue #346: Custom Transaction Categorization Rules
 * Issue #584: Unified Card/Table View System
 */

import { useState, useCallback, useMemo } from 'react'
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
import './AutoCategorizationPage.css'

export interface AutoCategorizationPageProps {
  categories: string[][]
  plaidCategories?: string[]  // Plaid categories from user's transactions for "existing_category" condition
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

export function AutoCategorizationPage({ categories, plaidCategories = [], accounts = [], tags = [], maxRules, currentTier }: AutoCategorizationPageProps) {
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
    const cats = new Set<string>()
    rules.forEach((rule) => {
      if (rule.target_category?.[0]) {
        cats.add(rule.target_category[0])
      }
    })
    return Array.from(cats)
      .sort()
      .map((cat) => ({
        value: cat,
        label: cat,
      }))
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

    // Apply category filter
    if (categoryFilter.length > 0) {
      filtered = filtered.filter((rule) =>
        rule.target_category?.[0] && categoryFilter.includes(rule.target_category[0])
      )
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
          const catA = a.target_category?.[0] || ''
          const catB = b.target_category?.[0] || ''
          comparison = catA.localeCompare(catB)
          break
        }
      }

      return sortDirection === 'asc' ? comparison : -comparison
    })
  }, [rules, sortColumn, sortDirection, statusFilter, categoryFilter, priorityRange])

  // Loading state
  if (loading) {
    return (
      <div className="auto-categorization-page">
        <div className="auto-categorization-page__loading">
          <div className="auto-categorization-page__spinner" />
          <p>Loading rules...</p>
        </div>
      </div>
    )
  }

  // Error state
  if (error) {
    return (
      <div className="auto-categorization-page">
        <div className="auto-categorization-page__error">
          <p className="auto-categorization-page__error-message">{error}</p>
          <button
            type="button"
            className="auto-categorization-page__btn auto-categorization-page__btn--retry"
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
      <div className="auto-categorization-page">
        <AutoCategorizationRuleEditor
          mode={editorMode}
          rule={editingRule ?? undefined}
          onSave={handleEditorSave}
          onCancel={handleEditorCancel}
          categories={categories}
          plaidCategories={plaidCategories}
          accounts={accounts}
          tags={tags}
        />
      </div>
    )
  }

  // Empty state
  if (rules.length === 0) {
    return (
      <div className="auto-categorization-page">
        <div className="auto-categorization-page__empty">
          <h2 className="auto-categorization-page__empty-title">No rules yet</h2>
          <p className="auto-categorization-page__empty-text">
            Create auto-categorization rules to automatically organize your transactions.
          </p>
          <button
            type="button"
            className="auto-categorization-page__btn auto-categorization-page__btn--primary"
            onClick={handleCreateClick}
          >
            Create Your First Rule
          </button>
        </div>
      </div>
    )
  }

  // Rules list
  return (
    <div className="auto-categorization-page">
      <div className="auto-categorization-page__header">
        <div className="auto-categorization-page__title-row">
          <h1 className="auto-categorization-page__title">Auto-Categorization Rules</h1>
          <span className="auto-categorization-page__count">{rules.length} rules</span>
        </div>
        {/* Issue #584: View toggle and actions */}
        <div className="auto-categorization-page__actions">
          <ListViewToggle
            view={view}
            onChange={handleViewChange}
            storageKey={STORAGE_KEY_LIST_VIEW}
          />
          <button
            type="button"
            className="auto-categorization-page__btn auto-categorization-page__btn--primary"
            onClick={handleCreateClick}
            disabled={isAtLimit}
          >
            Create Rule
          </button>
        </div>
      </div>

      {showTierWarning && (
        <div className="auto-categorization-page__tier-warning">
          <p>
            <strong>Rule limit reached.</strong> You&apos;ve reached the maximum of{' '}
            {maxRules} rule{maxRules !== 1 ? 's' : ''} for the free tier.
          </p>
          <a href="/settings/subscription" className="auto-categorization-page__upgrade-link">
            Upgrade for more rules
          </a>
        </div>
      )}

      {/* Issue #584: Conditional cards/table rendering */}
      <div className="auto-categorization-page__content">
        {view === 'cards' ? (
          <div className="auto-categorization-page__list">
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
            onDelete={(rule) => handleDelete(rule.id)}
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
    </div>
  )
}
