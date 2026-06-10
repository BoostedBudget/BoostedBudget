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
import type { Boost } from '@boostedbudget/api-client/models'
import { formatRelativeTime } from '@/shared/dates'
import { ReadOnlyGuard } from '@/auth/ReadOnlyGuard'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { BOOST_TYPE_LABELS } from '../boostConstants'
import { formatBoostConditionsSummary, formatBoostActionSummary } from '../boostUtils'
import type { BoostType } from '../types'

export interface BoostCardProps {
  /** The boost to display */
  boost: Boost
  /** Called when edit button is clicked */
  onEdit?: (boost: Boost) => void
  /** Called when delete button is clicked */
  onDelete?: (boost: Boost) => void
  /** Called when toggle switch is changed */
  onToggle?: (boost: Boost, enabled: boolean) => void
  /** Called when test button is clicked */
  onTest?: (boost: Boost) => void
  /** Called when "View recent executions" is clicked; pass to enable the link */
  onViewExecutions?: (boost: Boost) => void
  /** Show loading state on actions */
  loading?: boolean
  /** Compact display mode (hide description) */
  compact?: boolean
  /** Show priority value */
  showPriority?: boolean
  /** Additional CSS class */
  className?: string
}

const BOOST_TYPE_ICONS: Record<BoostType, typeof Bell> = {
  budget_alert: Bell,
  transfer_suggestion: ArrowRightLeft,
  category_rollover: RefreshCw,
  savings_goal: PiggyBank,
}

/**
 * BoostCard component displays a single boost in card format.
 * Design system: Badge + Button + Switch; semantic tokens only (no raw palette).
 */
export function BoostCard({
  boost,
  onEdit,
  onDelete,
  onToggle,
  onTest,
  onViewExecutions,
  loading = false,
  compact = false,
  showPriority = false,
  className = '',
}: BoostCardProps) {
  const boostType = boost.boost_type as BoostType
  const Icon = BOOST_TYPE_ICONS[boostType] || Bell
  const label = BOOST_TYPE_LABELS[boostType] || boost.boost_type

  const triggerType = useMemo(() => {
    const config = boost.trigger_config
    if (config?.type === 'event') {
      return 'event'
    }
    return 'schedule'
  }, [boost.trigger_config])

  const lastRunText = useMemo(() => {
    if (!boost.last_executed_at) {
      return 'Never run'
    }
    return `Last run ${formatRelativeTime(boost.last_executed_at)}`
  }, [boost.last_executed_at])

  const conditionSummary = useMemo(
    () => formatBoostConditionsSummary(boost.conditions, boost.condition_logic),
    [boost.conditions, boost.condition_logic],
  )

  const actionSummary = useMemo(
    () => formatBoostActionSummary(boost.boost_type, boost.actions),
    [boost.boost_type, boost.actions],
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
          <h3 className="truncate text-base font-semibold text-foreground"><span data-ph-mask>{boost.name}</span></h3>
          <span className="text-xs text-muted-foreground">{label}</span>
        </div>
        {onToggle && (
          <ReadOnlyGuard>
            <Switch
              checked={boost.enabled}
              onCheckedChange={(v) => !loading && onToggle(boost, v)}
              disabled={loading}
              aria-label={boost.enabled ? 'Disable boost' : 'Enable boost'}
            />
          </ReadOnlyGuard>
        )}
      </div>

      {!compact && boost.description && (
        <p className="text-sm leading-relaxed text-muted-foreground"><span data-ph-mask>{boost.description}</span></p>
      )}

      <div className="flex flex-col gap-2">
        <div className="flex flex-wrap gap-2">
          <Badge variant={boost.enabled ? 'success' : 'secondary'}>
            {boost.enabled ? 'Enabled' : 'Disabled'}
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
          <span className="inline-flex items-center">{boost.execution_count} executions</span>
          <span className="inline-flex items-center">{lastRunText}</span>
        </div>

        {conditionSummary && (
          <p className="text-xs text-muted-foreground italic" data-ph-mask>{conditionSummary}</p>
        )}

        {actionSummary && (
          <p className="text-xs text-muted-foreground italic" data-ph-mask>{actionSummary}</p>
        )}

        {showPriority && (
          <span className="text-xs text-muted-foreground">Priority: {boost.priority}</span>
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
                onClick={() => onTest(boost)}
                disabled={loading}
                aria-label="Backtest boost"
                title="See what this boost would have done with your recent data"
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
                onClick={() => onEdit(boost)}
                disabled={loading}
                aria-label="Edit boost"
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
                onClick={() => onDelete(boost)}
                disabled={loading}
                aria-label="Delete boost"
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
              onClick={() => onViewExecutions(boost)}
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
