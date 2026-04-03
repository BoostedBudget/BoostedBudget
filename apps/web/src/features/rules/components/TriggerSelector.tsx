/**
 * TriggerSelector Component
 * Issue #39: Rule Builder UI
 *
 * Component for selecting trigger type (schedule or event).
 * Integrates CronBuilder for scheduled triggers.
 */

import { useCallback } from 'react'
import { Calendar, Zap } from 'lucide-react'
import { CronBuilder } from './CronBuilder'
import {
  TRIGGER_TYPE_LABELS,
  TRIGGER_TYPE_DESCRIPTIONS,
  EVENT_TYPE_OPTIONS,
  DEFAULT_CRON,
  DEFAULT_TIMEZONE,
} from '../constants'
import type { TriggerFormState, TriggerType, EventType } from '../types'
import './TriggerSelector.css'

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
    <div className={`trigger-selector${className ? ` ${className}` : ''}`}>
      {/* Trigger Type Radio Group */}
      <fieldset
        className="trigger-selector__fieldset"
        role="radiogroup"
        aria-label="Trigger type"
      >
        <legend className="trigger-selector__legend">When should this rule run?</legend>

        <div className="trigger-selector__options">
          {/* Schedule Option */}
          <label className="trigger-selector__option">
            <input
              type="radio"
              name="trigger-type"
              value="schedule"
              checked={config.type === 'schedule'}
              onChange={() => handleTypeChange('schedule')}
              disabled={disabled}
              className="trigger-selector__radio"
            />
            <div className="trigger-selector__option-content">
              <div className="trigger-selector__option-header">
                <Calendar className="trigger-selector__icon" aria-hidden="true" />
                <span className="trigger-selector__option-label">
                  {TRIGGER_TYPE_LABELS.schedule}
                </span>
              </div>
              <p className="trigger-selector__option-description">
                {TRIGGER_TYPE_DESCRIPTIONS.schedule}
              </p>
            </div>
          </label>

          {/* Event Option */}
          <label className="trigger-selector__option">
            <input
              type="radio"
              name="trigger-type"
              value="event"
              checked={config.type === 'event'}
              onChange={() => handleTypeChange('event')}
              disabled={disabled}
              className="trigger-selector__radio"
            />
            <div className="trigger-selector__option-content">
              <div className="trigger-selector__option-header">
                <Zap className="trigger-selector__icon" aria-hidden="true" />
                <span className="trigger-selector__option-label">
                  {TRIGGER_TYPE_LABELS.event}
                </span>
              </div>
              <p className="trigger-selector__option-description">
                {TRIGGER_TYPE_DESCRIPTIONS.event}
              </p>
            </div>
          </label>
        </div>
      </fieldset>

      {/* Schedule Configuration */}
      {config.type === 'schedule' && (
        <div className="trigger-selector__config">
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
        <div className="trigger-selector__config">
          <label htmlFor="event-type" className="trigger-selector__label">
            Event type
          </label>
          <select
            id="event-type"
            value={config.event ?? ''}
            onChange={handleEventChange}
            disabled={disabled}
            className="trigger-selector__select"
          >
            <option value="">Select an event...</option>
            {EVENT_TYPE_OPTIONS.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
          {config.event && (
            <p className="trigger-selector__event-description">
              {EVENT_TYPE_OPTIONS.find((o) => o.value === config.event)?.description}
            </p>
          )}
        </div>
      )}
    </div>
  )
}
