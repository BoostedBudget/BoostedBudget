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
  History,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import type { AutomationRule } from '@boostedbudget/api-client/models'
import { formatRelativeTime } from '@/shared/dates'
import { ReadOnlyGuard } from '@/auth/ReadOnlyGuard'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { RULE_TYPE_LABELS } from '../constants'
import { formatRuleConditionsSummary, formatRuleActionSummary } from '../utils'
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
  /** Called when "View recent executions" is clicked; pass to enable the link */
  onViewExecutions?: (rule: AutomationRule) => void
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
 * Design system: Badge + Button + Switch; semantic tokens only (no raw palette).
 */
export function RuleCard({
  rule,
  onEdit,
  onDelete,
  onToggle,
  onTest,
  onViewExecutions,
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

  const conditionSummary = useMemo(
    () => formatRuleConditionsSummary(rule.conditions, rule.condition_logic),
    [rule.conditions, rule.condition_logic],
  )

  const actionSummary = useMemo(
    () => formatRuleActionSummary(rule.rule_type, rule.actions),
    [rule.rule_type, rule.actions],
  )

  return (
    <article
      className={cn(
        'flex flex-col gap-4 rounded-xl border border-border bg-card p-5 transition-shadow hover:shadow-md',
        compact && 'gap-3 p-4',
        className
      )}
    >
      <div className="flex items-center gap-3">
        <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-primary/10">
          <Icon className="h-5 w-5 text-primary" aria-hidden="true" />
        </div>
        <div className="min-w-0 flex-1">
          <h3 className="truncate text-base font-semibold text-foreground"><span data-ph-mask>{rule.name}</span></h3>
          <span className="text-xs text-muted-foreground">{label}</span>
        </div>
        {onToggle && (
          <ReadOnlyGuard>
            <Switch
              checked={rule.enabled}
              onCheckedChange={(v) => !loading && onToggle(rule, v)}
              disabled={loading}
              aria-label={rule.enabled ? 'Disable rule' : 'Enable rule'}
            />
          </ReadOnlyGuard>
        )}
      </div>

      {!compact && rule.description && (
        <p className="text-sm leading-relaxed text-muted-foreground"><span data-ph-mask>{rule.description}</span></p>
      )}

      <div className="flex flex-col gap-2">
        <div className="flex flex-wrap gap-2">
          <Badge variant={rule.enabled ? 'success' : 'secondary'}>
            {rule.enabled ? 'Enabled' : 'Disabled'}
          </Badge>
          <Badge variant="info">
            {triggerType === 'schedule' ? (
              <>
                <Calendar className="h-3 w-3 mr-1" aria-hidden="true" />
                Scheduled
              </>
            ) : (
              <>
                <Zap className="h-3 w-3 mr-1" aria-hidden="true" />
                Event
              </>
            )}
          </Badge>
        </div>

        <div className="flex gap-4 text-xs text-muted-foreground">
          <span className="inline-flex items-center">{rule.execution_count} executions</span>
          <span className="inline-flex items-center">{lastRunText}</span>
        </div>

        {conditionSummary && (
          <p className="text-xs text-muted-foreground italic">{conditionSummary}</p>
        )}

        {actionSummary && (
          <p className="text-xs text-muted-foreground italic" data-ph-mask>{actionSummary}</p>
        )}

        {showPriority && (
          <span className="text-xs text-muted-foreground">Priority: {rule.priority}</span>
        )}
      </div>

      {(onEdit || onDelete || onTest || onViewExecutions) && (
        <div className="flex gap-2 border-t border-border pt-3">
          <ReadOnlyGuard>
            {onTest && (
              <Button
                variant="outline"
                size="sm"
                type="button"
                onClick={() => onTest(rule)}
                disabled={loading}
                aria-label="Backtest rule"
                title="See what this rule would have done with your recent data"
              >
                <Play className="h-4 w-4" aria-hidden="true" />
                Backtest
              </Button>
            )}
            {onEdit && (
              <Button
                variant="ghost"
                size="sm"
                type="button"
                onClick={() => onEdit(rule)}
                disabled={loading}
                aria-label="Edit rule"
              >
                <Edit className="h-4 w-4" aria-hidden="true" />
                Edit
              </Button>
            )}
            {onDelete && (
              <Button
                variant="ghost"
                size="sm"
                type="button"
                onClick={() => onDelete(rule)}
                disabled={loading}
                aria-label="Delete rule"
                className="text-destructive hover:text-destructive hover:bg-destructive/10"
              >
                <Trash2 className="h-4 w-4" aria-hidden="true" />
                Delete
              </Button>
            )}
          </ReadOnlyGuard>
          {onViewExecutions && (
            <Button
              variant="ghost"
              size="sm"
              type="button"
              onClick={() => onViewExecutions(rule)}
              className="ml-auto"
              aria-label="View recent executions"
            >
              <History className="h-4 w-4" aria-hidden="true" />
              View recent executions
            </Button>
          )}
        </div>
      )}
    </article>
  )
}
