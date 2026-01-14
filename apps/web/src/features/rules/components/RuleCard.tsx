/**
 * RuleCard Component
 * Issue #39: Rule Builder UI
 *
 * Displays a single automation rule in card format.
 */

import { useMemo } from 'react'
import {
  Bell,
  ArrowRightLeft,
  RefreshCw,
  PiggyBank,
  Calendar,
  Zap,
  Edit,
  Trash2,
  Play,
} from 'lucide-react'
import type { AutomationRule } from '@boostedbudget/api-client/models'
import { RULE_TYPE_LABELS } from '../constants'
import type { RuleType } from '../types'
import './RuleCard.css'

export interface RuleCardProps {
  /** The rule to display */
  rule: AutomationRule
  /** Called when edit button is clicked */
  onEdit?: (rule: AutomationRule) => void
  /** Called when delete button is clicked */
  onDelete?: (rule: AutomationRule) => void
  /** Called when toggle switch is changed */
  onToggle?: (rule: AutomationRule, enabled: boolean) => void
  /** Called when test button is clicked */
  onTest?: (rule: AutomationRule) => void
  /** Show loading state on actions */
  loading?: boolean
  /** Compact display mode (hide description) */
  compact?: boolean
  /** Show priority value */
  showPriority?: boolean
  /** Additional CSS class */
  className?: string
}

const RULE_TYPE_ICONS: Record<RuleType, typeof Bell> = {
  budget_alert: Bell,
  transfer_suggestion: ArrowRightLeft,
  category_rollover: RefreshCw,
  savings_goal: PiggyBank,
}

function formatRelativeTime(dateStr: string): string {
  const date = new Date(dateStr)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffMins = Math.floor(diffMs / 60000)
  const diffHours = Math.floor(diffMs / 3600000)
  const diffDays = Math.floor(diffMs / 86400000)

  if (diffMins < 1) return 'just now'
  if (diffMins < 60) return `${diffMins}m ago`
  if (diffHours < 24) return `${diffHours}h ago`
  if (diffDays < 7) return `${diffDays}d ago`
  return date.toLocaleDateString()
}

/**
 * RuleCard component displays a single automation rule in card format.
 */
export function RuleCard({
  rule,
  onEdit,
  onDelete,
  onToggle,
  onTest,
  loading = false,
  compact = false,
  showPriority = false,
  className = '',
}: RuleCardProps) {
  const ruleType = rule.rule_type as RuleType
  const Icon = RULE_TYPE_ICONS[ruleType] || Bell
  const label = RULE_TYPE_LABELS[ruleType] || rule.rule_type

  const triggerType = useMemo(() => {
    const config = rule.trigger_config
    if (config?.type === 'event') {
      return 'event'
    }
    return 'schedule'
  }, [rule.trigger_config])

  const lastRunText = useMemo(() => {
    if (!rule.last_executed_at) {
      return 'Never run'
    }
    return `Last run ${formatRelativeTime(rule.last_executed_at)}`
  }, [rule.last_executed_at])

  const handleToggle = () => {
    if (onToggle && !loading) {
      onToggle(rule, !rule.enabled)
    }
  }

  return (
    <article
      className={`rule-card ${compact ? 'rule-card--compact' : ''} ${className}`.trim()}
    >
      <div className="rule-card__header">
        <div className="rule-card__icon-wrapper">
          <Icon className="rule-card__icon" aria-hidden="true" />
        </div>
        <div className="rule-card__title-section">
          <h3 className="rule-card__name">{rule.name}</h3>
          <span className="rule-card__type-label">{label}</span>
        </div>
        {onToggle && (
          <button
            type="button"
            role="switch"
            aria-checked={rule.enabled}
            onClick={handleToggle}
            disabled={loading}
            className={`rule-card__toggle ${rule.enabled ? 'rule-card__toggle--on' : ''}`}
            aria-label={rule.enabled ? 'Disable rule' : 'Enable rule'}
          >
            <span className="rule-card__toggle-slider" />
          </button>
        )}
      </div>

      {!compact && rule.description && (
        <p className="rule-card__description">{rule.description}</p>
      )}

      <div className="rule-card__meta">
        <div className="rule-card__badges">
          <span
            className={`rule-card__status ${rule.enabled ? 'rule-card__status--enabled' : 'rule-card__status--disabled'}`}
          >
            {rule.enabled ? 'Enabled' : 'Disabled'}
          </span>
          <span className="rule-card__trigger-badge">
            {triggerType === 'schedule' ? (
              <>
                <Calendar className="rule-card__badge-icon" aria-hidden="true" />
                Scheduled
              </>
            ) : (
              <>
                <Zap className="rule-card__badge-icon" aria-hidden="true" />
                Event
              </>
            )}
          </span>
        </div>

        <div className="rule-card__stats">
          <span className="rule-card__execution-count">
            {rule.execution_count} executions
          </span>
          <span className="rule-card__last-run">{lastRunText}</span>
        </div>

        {showPriority && (
          <span className="rule-card__priority">Priority: {rule.priority}</span>
        )}
      </div>

      {(onEdit || onDelete || onTest) && (
        <div className="rule-card__actions">
          {onTest && (
            <button
              type="button"
              onClick={() => onTest(rule)}
              disabled={loading}
              className="rule-card__action-btn rule-card__action-btn--test"
              aria-label="Backtest rule"
              title="See what this rule would have done with your recent data"
            >
              <Play className="rule-card__action-icon" aria-hidden="true" />
              Backtest
            </button>
          )}
          {onEdit && (
            <button
              type="button"
              onClick={() => onEdit(rule)}
              disabled={loading}
              className="rule-card__action-btn"
              aria-label="Edit rule"
            >
              <Edit className="rule-card__action-icon" aria-hidden="true" />
              Edit
            </button>
          )}
          {onDelete && (
            <button
              type="button"
              onClick={() => onDelete(rule)}
              disabled={loading}
              className="rule-card__action-btn rule-card__action-btn--danger"
              aria-label="Delete rule"
            >
              <Trash2 className="rule-card__action-icon" aria-hidden="true" />
              Delete
            </button>
          )}
        </div>
      )}
    </article>
  )
}
