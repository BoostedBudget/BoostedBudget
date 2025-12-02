/**
 * AutoCategorizationRuleCard Component
 *
 * Displays a single auto-categorization rule with toggle, edit, and delete actions.
 * Issue #346: Custom Transaction Categorization Rules
 */

import { useState } from 'react'
import type { CategorizationRule } from '../types'
import { CONDITION_TYPE_LABELS } from '../types'
import './AutoCategorizationRuleCard.css'

export interface AutoCategorizationRuleCardProps {
  rule: CategorizationRule
  onToggle: (enabled: boolean) => Promise<void>
  onEdit: () => void
  onDelete: () => Promise<void>
}

/**
 * Formats a condition value for display
 */
function formatConditionValue(value: unknown): string {
  if (Array.isArray(value)) {
    return value.join(', ')
  }
  if (typeof value === 'object' && value !== null) {
    // Handle range objects
    const obj = value as Record<string, unknown>
    if ('min' in obj && 'max' in obj) {
      return `${obj.min} - ${obj.max}`
    }
    if ('start' in obj || 'end' in obj) {
      const parts = []
      if (obj.start) parts.push(`from ${obj.start}`)
      if (obj.end) parts.push(`to ${obj.end}`)
      return parts.join(' ')
    }
  }
  return String(value)
}

export function AutoCategorizationRuleCard({ rule, onToggle, onEdit, onDelete }: AutoCategorizationRuleCardProps) {
  const [toggling, setToggling] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false)

  const handleToggle = async () => {
    setToggling(true)
    try {
      await onToggle(!rule.enabled)
    } finally {
      setToggling(false)
    }
  }

  const handleDeleteClick = () => {
    setShowDeleteConfirm(true)
  }

  const handleDeleteConfirm = async () => {
    setDeleting(true)
    try {
      await onDelete()
    } finally {
      setDeleting(false)
      setShowDeleteConfirm(false)
    }
  }

  const handleDeleteCancel = () => {
    setShowDeleteConfirm(false)
  }

  return (
    <div className={`auto-categorization-rule-card ${!rule.enabled ? 'auto-categorization-rule-card--disabled' : ''}`}>
      <div className="auto-categorization-rule-card__header">
        <div className="auto-categorization-rule-card__title-row">
          <h3 className="auto-categorization-rule-card__name">{rule.name}</h3>
          <span className={`auto-categorization-rule-card__status ${rule.enabled ? 'auto-categorization-rule-card__status--enabled' : 'auto-categorization-rule-card__status--disabled'}`}>
            {rule.enabled ? 'Active' : 'Disabled'}
          </span>
        </div>
        {rule.description && (
          <p className="auto-categorization-rule-card__description">{rule.description}</p>
        )}
      </div>

      <div className="auto-categorization-rule-card__content">
        {/* Conditions */}
        <div className="auto-categorization-rule-card__section">
          <h4 className="auto-categorization-rule-card__section-title">
            Conditions ({rule.condition_logic})
          </h4>
          <ul className="auto-categorization-rule-card__conditions">
            {rule.conditions.map((condition, index) => (
              <li key={index} className="auto-categorization-rule-card__condition">
                <span className="auto-categorization-rule-card__condition-type">
                  {CONDITION_TYPE_LABELS[condition.type] || condition.type}
                </span>
                <span className="auto-categorization-rule-card__condition-value">
                  {formatConditionValue(condition.value)}
                </span>
              </li>
            ))}
          </ul>
        </div>

        {/* Actions */}
        <div className="auto-categorization-rule-card__section">
          <h4 className="auto-categorization-rule-card__section-title">Actions</h4>
          <div className="auto-categorization-rule-card__actions-list">
            {rule.target_category && rule.target_category.length > 0 && (
              <div className="auto-categorization-rule-card__action">
                <span className="auto-categorization-rule-card__action-label">Set category:</span>
                <span className="auto-categorization-rule-card__action-value">
                  {rule.target_category.join(' > ')}
                </span>
              </div>
            )}
            {rule.target_tags && rule.target_tags.length > 0 && (
              <div className="auto-categorization-rule-card__action">
                <span className="auto-categorization-rule-card__action-label">
                  {rule.tag_behavior === 'replace' ? 'Replace tags:' : 'Add tags:'}
                </span>
                <span className="auto-categorization-rule-card__action-value">
                  {rule.target_tags.map((tag) => (
                    <span key={tag} className="auto-categorization-rule-card__tag">{tag}</span>
                  ))}
                </span>
              </div>
            )}
          </div>
        </div>

        {/* Stats */}
        <div className="auto-categorization-rule-card__stats">
          <span className="auto-categorization-rule-card__stat">
            Matched: {rule.match_count}
          </span>
          <span className="auto-categorization-rule-card__stat">
            Overridden: {rule.override_count}
          </span>
          {rule.last_matched_at && (
            <span className="auto-categorization-rule-card__stat">
              Last match: {new Date(rule.last_matched_at).toLocaleDateString()}
            </span>
          )}
        </div>
      </div>

      <div className="auto-categorization-rule-card__footer">
        <div className="auto-categorization-rule-card__priority">
          Priority: {rule.priority}
        </div>
        <div className="auto-categorization-rule-card__buttons">
          <button
            type="button"
            className="auto-categorization-rule-card__button auto-categorization-rule-card__button--toggle"
            onClick={handleToggle}
            disabled={toggling}
          >
            {toggling ? 'Saving...' : rule.enabled ? 'Disable' : 'Enable'}
          </button>
          <button
            type="button"
            className="auto-categorization-rule-card__button auto-categorization-rule-card__button--edit"
            onClick={onEdit}
          >
            Edit
          </button>
          <button
            type="button"
            className="auto-categorization-rule-card__button auto-categorization-rule-card__button--delete"
            onClick={handleDeleteClick}
            disabled={deleting}
          >
            Delete
          </button>
        </div>
      </div>

      {/* Delete confirmation dialog */}
      {showDeleteConfirm && (
        <div className="auto-categorization-rule-card__confirm-overlay">
          <div className="auto-categorization-rule-card__confirm-dialog">
            <p>Are you sure you want to delete "{rule.name}"?</p>
            <div className="auto-categorization-rule-card__confirm-buttons">
              <button
                type="button"
                className="auto-categorization-rule-card__button auto-categorization-rule-card__button--cancel"
                onClick={handleDeleteCancel}
                disabled={deleting}
              >
                Cancel
              </button>
              <button
                type="button"
                className="auto-categorization-rule-card__button auto-categorization-rule-card__button--confirm-delete"
                onClick={handleDeleteConfirm}
                disabled={deleting}
              >
                {deleting ? 'Deleting...' : 'Delete'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
