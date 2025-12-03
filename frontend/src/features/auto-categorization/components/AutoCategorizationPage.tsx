/**
 * AutoCategorizationPage Component
 *
 * Main page for managing auto-categorization rules.
 * Issue #346: Custom Transaction Categorization Rules
 */

import { useState, useCallback } from 'react'
import { useAutoCategorizationRules } from '../hooks/useAutoCategorizationRules'
import { AutoCategorizationRuleCard } from './AutoCategorizationRuleCard'
import { AutoCategorizationRuleEditor } from './AutoCategorizationRuleEditor'
import type { CategorizationRule, CreateRuleRequest, UpdateRuleRequest } from '../types'
import type { Account } from '../../accounts/types'
import './AutoCategorizationPage.css'

export interface AutoCategorizationPageProps {
  categories: string[][]
  accounts?: Account[]
  maxRules?: number
  currentTier?: string
}

type EditorMode = 'create' | 'edit' | null

export function AutoCategorizationPage({ categories, accounts = [], maxRules, currentTier }: AutoCategorizationPageProps) {
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
          accounts={accounts}
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
        <button
          type="button"
          className="auto-categorization-page__btn auto-categorization-page__btn--primary"
          onClick={handleCreateClick}
          disabled={isAtLimit}
        >
          Create Rule
        </button>
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
    </div>
  )
}
