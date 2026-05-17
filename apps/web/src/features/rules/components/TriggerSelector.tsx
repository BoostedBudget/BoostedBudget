/**
 * TriggerSelector Component
 * Issue #39: Rule Builder UI
 *
 * Component for selecting trigger type (schedule or event).
 * Integrates CronBuilder for scheduled triggers.
 */

import { useCallback } from 'react'
import { Calendar, Zap } from 'lucide-react'
import { cn } from '@/lib/utils'
import { CronBuilder } from './CronBuilder'
import {
  TRIGGER_TYPE_LABELS,
  TRIGGER_TYPE_DESCRIPTIONS,
  EVENT_TYPE_OPTIONS,
  DEFAULT_CRON,
  DEFAULT_TIMEZONE,
} from '../constants'
import type { TriggerFormState, TriggerType, EventType } from '../types'

export interface TriggerSelectorProps {
  /** Current trigger configuration */
  config: TriggerFormState
  /** Called when configuration changes */
  onChange: (config: TriggerFormState) => void
  /** Disable all controls */
  disabled?: boolean
  /** Additional CSS class */
  className?: string
}

/**
 * TriggerSelector component for choosing between scheduled and event-based triggers.
 *
 * @example
 * ```tsx
 * function RuleForm() {
 *   const [config, setConfig] = useState<TriggerFormState>({
 *     type: 'schedule',
 *     cron: '0 9 * * *',
 *     timezone: 'America/New_York',
 *   })
 *
 *   return (
 *     <TriggerSelector
 *       config={config}
 *       onChange={setConfig}
 *     />
 *   )
 * }
 * ```
 */
export function TriggerSelector({
  config,
  onChange,
  disabled = false,
  className = '',
}: TriggerSelectorProps) {
  const handleTypeChange = useCallback(
    (newType: TriggerType) => {
      if (disabled) return

      if (newType === 'schedule') {
        onChange({
          type: 'schedule',
          cron: config.cron ?? DEFAULT_CRON,
          timezone: config.timezone ?? DEFAULT_TIMEZONE,
        })
      } else {
        onChange({
          type: 'event',
          event: config.event ?? 'budget_updated',
        })
      }
    },
    [config, onChange, disabled]
  )

  const handleCronChange = useCallback(
    (cron: string) => {
      onChange({
        ...config,
        type: 'schedule',
        cron,
      })
    },
    [config, onChange]
  )

  const handleTimezoneChange = useCallback(
    (timezone: string) => {
      onChange({
        ...config,
        timezone,
      })
    },
    [config, onChange]
  )

  const handleEventChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      onChange({
        ...config,
        type: 'event',
        event: e.target.value as EventType,
      })
    },
    [config, onChange]
  )

  return (
    <div className={cn('flex flex-col gap-6', className)}>
      {/* Trigger Type Radio Group */}
      <fieldset
        className="m-0 border-none p-0"
        role="radiogroup"
        aria-label="Trigger type"
      >
        <legend className="mb-3 text-base font-semibold text-foreground">When should this rule run?</legend>

        <div className="flex flex-col gap-3">
          {/* Schedule Option */}
          <label
            className={cn(
              'flex cursor-pointer items-start gap-3 rounded-xl border-2 bg-card p-4 transition-all',
              config.type === 'schedule'
                ? 'border-primary bg-blue-50 dark:bg-blue-900'
                : 'border-border hover:border-blue-300 dark:hover:border-blue-600',
              disabled && 'cursor-not-allowed opacity-60'
            )}
          >
            <input
              type="radio"
              name="trigger-type"
              value="schedule"
              checked={config.type === 'schedule'}
              onChange={() => handleTypeChange('schedule')}
              disabled={disabled}
              className="mt-0.5 h-5 w-5 accent-primary"
            />
            <div className="flex-1">
              <div className="mb-1 flex items-center gap-2">
                <Calendar className="h-5 w-5 text-primary dark:text-blue-400" aria-hidden="true" />
                <span className="font-semibold text-foreground">
                  {TRIGGER_TYPE_LABELS.schedule}
                </span>
              </div>
              <p className="m-0 text-sm text-muted-foreground">
                {TRIGGER_TYPE_DESCRIPTIONS.schedule}
              </p>
            </div>
          </label>

          {/* Event Option */}
          <label
            className={cn(
              'flex cursor-pointer items-start gap-3 rounded-xl border-2 bg-card p-4 transition-all',
              config.type === 'event'
                ? 'border-primary bg-blue-50 dark:bg-blue-900'
                : 'border-border hover:border-blue-300 dark:hover:border-blue-600',
              disabled && 'cursor-not-allowed opacity-60'
            )}
          >
            <input
              type="radio"
              name="trigger-type"
              value="event"
              checked={config.type === 'event'}
              onChange={() => handleTypeChange('event')}
              disabled={disabled}
              className="mt-0.5 h-5 w-5 accent-primary"
            />
            <div className="flex-1">
              <div className="mb-1 flex items-center gap-2">
                <Zap className="h-5 w-5 text-primary dark:text-blue-400" aria-hidden="true" />
                <span className="font-semibold text-foreground">
                  {TRIGGER_TYPE_LABELS.event}
                </span>
              </div>
              <p className="m-0 text-sm text-muted-foreground">
                {TRIGGER_TYPE_DESCRIPTIONS.event}
              </p>
            </div>
          </label>
        </div>
      </fieldset>

      {/* Schedule Configuration */}
      {config.type === 'schedule' && (
        <div className="rounded-xl border border-border bg-muted/20 p-4 dark:bg-muted/10">
          <CronBuilder
            cron={config.cron ?? DEFAULT_CRON}
            timezone={config.timezone ?? DEFAULT_TIMEZONE}
            onChange={handleCronChange}
            onTimezoneChange={handleTimezoneChange}
            disabled={disabled}
          />
        </div>
      )}

      {/* Event Configuration */}
      {config.type === 'event' && (
        <div className="rounded-xl border border-border bg-muted/20 p-4 dark:bg-muted/10">
          <label htmlFor="event-type" className="mb-2 block text-sm font-medium text-foreground">
            Event type
          </label>
          <select
            id="event-type"
            value={config.event ?? ''}
            onChange={handleEventChange}
            disabled={disabled}
            className="w-full cursor-pointer rounded-lg border border-input bg-background px-3.5 py-2.5 text-sm text-foreground focus:border-ring focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-card"
          >
            <option value="">Select an event...</option>
            {EVENT_TYPE_OPTIONS.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
          {config.event && (
            <p className="mt-2 text-xs text-muted-foreground">
              {EVENT_TYPE_OPTIONS.find((o) => o.value === config.event)?.description}
            </p>
          )}
        </div>
      )}
    </div>
  )
}
