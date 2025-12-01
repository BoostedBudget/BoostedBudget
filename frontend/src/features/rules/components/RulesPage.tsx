/**
 * RulesPage Component
 *
 * Main page for managing categorization rules.
 * Issue #346: Custom Transaction Categorization Rules
 */

import { useState, useCallback } from 'react'
import { useRules } from '../hooks/useRules'
import { RuleCard } from './RuleCard'
import { RuleEditor } from './RuleEditor'
import type { CategorizationRule, CreateRuleRequest, UpdateRuleRequest } from '../types'
import './RulesPage.css'

export interface RulesPageProps {
  categories: string[][]
  maxRules?: number
  currentTier?: string
}

type EditorMode = 'create' | 'edit' | null

export function RulesPage({ categories, maxRules, currentTier }: RulesPageProps) {
  const {
    rules,
    loading,
    error,
    createRule,
    updateRule,
    deleteRule,
    toggleRule,
    refresh,
  } = useRules()

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
      <div className="rules-page">
        <div className="rules-page__loading">
          <div className="rules-page__spinner" />
          <p>Loading rules...</p>
        </div>
      </div>
    )
  }

  // Error state
  if (error) {
    return (
      <div className="rules-page">
        <div className="rules-page__error">
          <p className="rules-page__error-message">{error}</p>
          <button
            type="button"
            className="rules-page__btn rules-page__btn--retry"
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
      <div className="rules-page">
        <RuleEditor
          mode={editorMode}
          rule={editingRule ?? undefined}
          onSave={handleEditorSave}
          onCancel={handleEditorCancel}
          categories={categories}
        />
      </div>
    )
  }

  // Empty state
  if (rules.length === 0) {
    return (
      <div className="rules-page">
        <div className="rules-page__empty">
          <h2 className="rules-page__empty-title">No rules yet</h2>
          <p className="rules-page__empty-text">
            Create categorization rules to automatically organize your transactions.
          </p>
          <button
            type="button"
            className="rules-page__btn rules-page__btn--primary"
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
    <div className="rules-page">
      <div className="rules-page__header">
        <div className="rules-page__title-row">
          <h1 className="rules-page__title">Categorization Rules</h1>
          <span className="rules-page__count">{rules.length} rules</span>
        </div>
        <button
          type="button"
          className="rules-page__btn rules-page__btn--primary"
          onClick={handleCreateClick}
          disabled={isAtLimit}
        >
          Create Rule
        </button>
      </div>

      {showTierWarning && (
        <div className="rules-page__tier-warning">
          <p>
            <strong>Rule limit reached.</strong> You&apos;ve reached the maximum of{' '}
            {maxRules} rule{maxRules !== 1 ? 's' : ''} for the free tier.
          </p>
          <a href="/settings/subscription" className="rules-page__upgrade-link">
            Upgrade for more rules
          </a>
        </div>
      )}

      <div className="rules-page__list">
        {rules.map((rule) => (
          <RuleCard
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
