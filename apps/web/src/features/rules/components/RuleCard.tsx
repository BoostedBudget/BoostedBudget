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
import { cn } from '@/lib/utils'
import type { AutomationRule } from '@boostedbudget/api-client/models'
import { formatRelativeTime } from '@/shared/dates'
import { RULE_TYPE_LABELS } from '../constants'
import type { RuleType } from '../types'

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
      className={cn(
        'flex flex-col gap-4 rounded-xl border border-border bg-card p-5 transition-shadow hover:shadow-md',
        compact && 'gap-3 p-4',
        className
      )}
    >
      <div className="flex items-center gap-3">
        <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-blue-50 dark:bg-blue-900">
          <Icon className="h-5 w-5 text-blue-600 dark:text-blue-400" aria-hidden="true" />
        </div>
        <div className="min-w-0 flex-1">
          <h3 className="truncate text-base font-semibold text-foreground"><span data-ph-mask>{rule.name}</span></h3>
          <span className="text-xs text-muted-foreground">{label}</span>
        </div>
        {onToggle && (
          <button
            type="button"
            role="switch"
            aria-checked={rule.enabled}
            onClick={handleToggle}
            disabled={loading}
            className={cn(
              'relative h-6 w-11 shrink-0 cursor-pointer rounded-full border-none transition-colors disabled:cursor-not-allowed disabled:opacity-50',
              rule.enabled
                ? 'bg-green-500 dark:bg-green-600'
                : 'bg-muted-foreground/30 dark:bg-muted-foreground/20'
            )}
            aria-label={rule.enabled ? 'Disable rule' : 'Enable rule'}
          >
            <span
              className={cn(
                'absolute top-0.5 left-0.5 h-5 w-5 rounded-full bg-white transition-transform',
                rule.enabled && 'translate-x-5'
              )}
            />
          </button>
        )}
      </div>

      {!compact && rule.description && (
        <p className="text-sm leading-relaxed text-muted-foreground">{rule.description}</p>
      )}

      <div className="flex flex-col gap-2">
        <div className="flex flex-wrap gap-2">
          <span
            className={cn(
              'inline-flex items-center rounded-md px-2 py-1 text-xs font-medium',
              rule.enabled
                ? 'bg-green-50 text-green-700 dark:bg-green-900 dark:text-green-300'
                : 'bg-muted text-muted-foreground'
            )}
          >
            {rule.enabled ? 'Enabled' : 'Disabled'}
          </span>
          <span className="inline-flex items-center gap-1 rounded-md bg-blue-50 px-2 py-1 text-xs font-medium text-blue-700 dark:bg-blue-900 dark:text-blue-300">
            {triggerType === 'schedule' ? (
              <>
                <Calendar className="h-3 w-3" aria-hidden="true" />
                Scheduled
              </>
            ) : (
              <>
                <Zap className="h-3 w-3" aria-hidden="true" />
                Event
              </>
            )}
          </span>
        </div>

        <div className="flex gap-4 text-xs text-muted-foreground">
          <span className="inline-flex items-center">{rule.execution_count} executions</span>
          <span className="inline-flex items-center">{lastRunText}</span>
        </div>

        {showPriority && (
          <span className="text-xs text-muted-foreground">Priority: {rule.priority}</span>
        )}
      </div>

      {(onEdit || onDelete || onTest) && (
        <div className="flex gap-2 border-t border-border pt-3">
          {onTest && (
            <button
              type="button"
              onClick={() => onTest(rule)}
              disabled={loading}
              className="inline-flex items-center gap-1.5 rounded-md bg-blue-50 px-3 py-2 text-sm font-medium text-blue-700 transition-colors hover:bg-blue-100 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-blue-900 dark:text-blue-300 dark:hover:bg-blue-800"
              aria-label="Backtest rule"
              title="See what this rule would have done with your recent data"
            >
              <Play className="h-4 w-4" aria-hidden="true" />
              Backtest
            </button>
          )}
          {onEdit && (
            <button
              type="button"
              onClick={() => onEdit(rule)}
              disabled={loading}
              className="inline-flex items-center gap-1.5 rounded-md bg-muted px-3 py-2 text-sm font-medium text-muted-foreground transition-colors hover:bg-muted/80 disabled:cursor-not-allowed disabled:opacity-50"
              aria-label="Edit rule"
            >
              <Edit className="h-4 w-4" aria-hidden="true" />
              Edit
            </button>
          )}
          {onDelete && (
            <button
              type="button"
              onClick={() => onDelete(rule)}
              disabled={loading}
              className="inline-flex items-center gap-1.5 rounded-md bg-muted px-3 py-2 text-sm font-medium text-destructive transition-colors hover:bg-red-50 disabled:cursor-not-allowed disabled:opacity-50 dark:hover:bg-red-900"
              aria-label="Delete rule"
            >
              <Trash2 className="h-4 w-4" aria-hidden="true" />
              Delete
            </button>
          )}
        </div>
      )}
    </article>
  )
}
