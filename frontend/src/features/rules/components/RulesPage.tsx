/**
 * RulesPage Component
 * Issue #39: Rule Builder UI
 *
 * Main page for viewing and managing automation rules.
 * Supports card and table views with tier limit display.
 */

import { useState, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { Plus, AlertCircle, Loader2 } from 'lucide-react'
import {
  useGetAutomationRules,
  useToggleAutomationRule,
  useDeleteAutomationRule,
  useTestAutomationRule,
  getGetAutomationRulesQueryKey,
} from '@/api/generated/endpoints/automation-rules/automation-rules'
import type { AutomationRule } from '@/api/generated/models'
import { ListViewToggle } from '@/shared/components/ListViewToggle'
import type { ListView } from '@/shared/components/ListViewToggle'
import { useRuleTierLimits } from '../hooks/useRuleTierLimits'
import { STORAGE_KEY_LIST_VIEW } from '../constants'
import { RuleCard } from './RuleCard'
import { RuleTable } from './RuleTable'
import './RulesPage.css'

interface DeleteConfirmState {
  isOpen: boolean
  rule: AutomationRule | null
}

/**
 * RulesPage displays the list of automation rules with card/table toggle.
 */
export function RulesPage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  // Get initial view from localStorage
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

  const [view, setView] = useState<ListView>(getInitialView)
  const [deleteConfirm, setDeleteConfirm] = useState<DeleteConfirmState>({
    isOpen: false,
    rule: null,
  })

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

  // Tier limits (simplified - all tiers have unlimited rules)
  const tierLimits = useRuleTierLimits({ currentRuleCount: rules.length })

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
    setDeleteConfirm({ isOpen: true, rule })
  }, [])

  const handleDeleteConfirm = useCallback(() => {
    if (deleteConfirm.rule) {
      deleteMutation.mutate(
        { id: deleteConfirm.rule.id },
        {
          onSuccess: () => {
            setDeleteConfirm({ isOpen: false, rule: null })
            refetch()
          },
        }
      )
    }
  }, [deleteConfirm.rule, deleteMutation, refetch])

  const handleDeleteCancel = useCallback(() => {
    setDeleteConfirm({ isOpen: false, rule: null })
  }, [])

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
      <div className="rules-page">
        <div className="rules-page__loading">
          <Loader2 className="rules-page__spinner" />
          <p>Loading automation rules...</p>
        </div>
      </div>
    )
  }

  // Error state
  if (error) {
    return (
      <div className="rules-page">
        <div className="rules-page__error">
          <AlertCircle className="rules-page__error-icon" />
          <h2>Error loading rules</h2>
          <p>There was a problem loading your automation rules.</p>
          <button
            type="button"
            onClick={() => refetch()}
            className="rules-page__retry-btn"
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
      <div className="rules-page">
        <header className="rules-page__header">
          <h1 className="rules-page__title">Automation Rules</h1>
        </header>
        <div className="rules-page__empty">
          <h2>No automation rules yet</h2>
          <p>
            Create your first automation rule to automatically manage your
            budgets, transfers, and savings.
          </p>
          <button
            type="button"
            onClick={handleCreateRule}
            className="rules-page__create-btn"
            aria-label="Create your first rule"
          >
            <Plus aria-hidden="true" />
            Create Your First Rule
          </button>
        </div>
      </div>
    )
  }

  return (
    <div className="rules-page">
      <header className="rules-page__header">
        <div className="rules-page__title-row">
          <h1 className="rules-page__title">Automation Rules</h1>
          <span className="rules-page__tier-display">
            {tierLimits.rulesCountDisplay}
          </span>
        </div>

        <div className="rules-page__actions">
          <ListViewToggle
            view={view}
            onChange={handleViewChange}
            storageKey={STORAGE_KEY_LIST_VIEW}
          />
          <button
            type="button"
            onClick={handleCreateRule}
            disabled={tierLimits.isAtLimit}
            className="rules-page__create-btn"
            aria-label="Create new rule"
          >
            <Plus aria-hidden="true" />
            Create Rule
          </button>
        </div>
      </header>

      {tierLimits.shouldShowUpgradeBanner && (
        <div className="rules-page__upgrade-banner">
          <p>
            You've reached your rule limit. Upgrade to create more automation
            rules.
          </p>
          <button
            type="button"
            onClick={() => navigate('/pricing')}
            className="rules-page__upgrade-btn"
          >
            Upgrade Now
          </button>
        </div>
      )}

      <div className="rules-page__content">
        {view === 'cards' ? (
          <div className="rules-page__cards-grid">
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
            rules={rules}
            onEdit={handleEditRule}
            onDelete={handleDeleteClick}
            onToggle={handleToggleRule}
            onTest={handleTestRule}
            loading={toggleMutation.isPending || deleteMutation.isPending || testMutation.isPending}
          />
        )}
      </div>

      {/* Delete Confirmation Dialog */}
      {deleteConfirm.isOpen && (
        <div
          className="rules-page__dialog-overlay"
          onClick={handleDeleteCancel}
          role="presentation"
        >
          <div
            className="rules-page__dialog"
            role="alertdialog"
            aria-modal="true"
            aria-labelledby="delete-dialog-title"
            onClick={(e) => e.stopPropagation()}
          >
            <h2 id="delete-dialog-title" className="rules-page__dialog-title">
              Delete Rule
            </h2>
            <p className="rules-page__dialog-message">
              Are you sure you want to delete "{deleteConfirm.rule?.name}"? This
              action cannot be undone.
            </p>
            <div className="rules-page__dialog-actions">
              <button
                type="button"
                onClick={handleDeleteCancel}
                className="rules-page__dialog-btn rules-page__dialog-btn--cancel"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={handleDeleteConfirm}
                className="rules-page__dialog-btn rules-page__dialog-btn--confirm"
                aria-label="Confirm delete"
              >
                Confirm
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
