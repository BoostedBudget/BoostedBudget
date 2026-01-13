/**
 * CronBuilder Component
 * Issue #39: Rule Builder UI
 *
 * Component for building cron schedules with presets and custom input.
 * Shows human-readable descriptions and timezone selection.
 */

import { useState, useCallback, useMemo } from 'react'
import { Clock, ChevronDown, ChevronUp } from 'lucide-react'
import { CRON_PRESETS, COMMON_TIMEZONES, type CronPreset } from '../constants'
import './CronBuilder.css'

export interface CronBuilderProps {
  /** Current cron expression */
  cron: string
  /** Current timezone (IANA format) */
  timezone?: string
  /** Called when cron expression changes */
  onChange: (cron: string) => void
  /** Called when timezone changes */
  onTimezoneChange?: (timezone: string) => void
  /** Disable all controls */
  disabled?: boolean
  /** Additional CSS class */
  className?: string
}

// Simple cron validation - checks for 5 space-separated parts
function isValidCron(cron: string): boolean {
  const parts = cron.trim().split(/\s+/)
  if (parts.length !== 5) return false

  // Basic validation for each part
  const patterns = [
    /^(\*|[0-5]?\d)$/, // minute: 0-59
    /^(\*|[01]?\d|2[0-3])$/, // hour: 0-23
    /^(\*|[1-9]|[12]\d|3[01]|L)$/, // day of month: 1-31 or L
    /^(\*|[1-9]|1[0-2])$/, // month: 1-12
    /^(\*|[0-6])$/, // day of week: 0-6
  ]

  // Allow more complex expressions with commas, ranges, and steps
  const complexPattern = /^[\d*,\-/L]+$/

  return parts.every((part, i) => {
    if (patterns[i].test(part)) return true
    return complexPattern.test(part)
  })
}

// Generate human-readable description for cron expression
function getCronDescription(cron: string): string {
  // First check if it matches any preset
  const preset = CRON_PRESETS.find((p) => p.cron === cron)
  if (preset) return preset.description

  // Basic parsing for common patterns
  const parts = cron.trim().split(/\s+/)
  if (parts.length !== 5) return 'Invalid cron expression'

  const [minute, hour, dayOfMonth, month, dayOfWeek] = parts

  // Format time
  const formatTime = (h: string, m: string): string => {
    const hourNum = parseInt(h, 10)
    const minNum = parseInt(m, 10)
    if (isNaN(hourNum) || isNaN(minNum)) return ''

    const period = hourNum >= 12 ? 'PM' : 'AM'
    const displayHour = hourNum === 0 ? 12 : hourNum > 12 ? hourNum - 12 : hourNum
    const displayMin = minNum.toString().padStart(2, '0')
    return `${displayHour}:${displayMin} ${period}`
  }

  // Generate description based on pattern
  if (dayOfMonth === '*' && month === '*' && dayOfWeek === '*') {
    const time = formatTime(hour, minute)
    if (time) return `Every day at ${time}`
  }

  if (dayOfMonth === '*' && month === '*' && dayOfWeek !== '*') {
    const days = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']
    const dayName = days[parseInt(dayOfWeek, 10)] || dayOfWeek
    const time = formatTime(hour, minute)
    if (time) return `Every ${dayName} at ${time}`
  }

  if (dayOfMonth !== '*' && month === '*' && dayOfWeek === '*') {
    const time = formatTime(hour, minute)
    if (dayOfMonth === 'L') {
      if (time) return `Last day of each month at ${time}`
    }
    if (time) return `Day ${dayOfMonth} of each month at ${time}`
  }

  return `${minute} ${hour} ${dayOfMonth} ${month} ${dayOfWeek}`
}

/**
 * CronBuilder component for scheduling automation rules.
 *
 * @example
 * ```tsx
 * function TriggerForm() {
 *   const [cron, setCron] = useState('0 9 * * *')
 *   const [timezone, setTimezone] = useState('America/New_York')
 *
 *   return (
 *     <CronBuilder
 *       cron={cron}
 *       timezone={timezone}
 *       onChange={setCron}
 *       onTimezoneChange={setTimezone}
 *     />
 *   )
 * }
 * ```
 */
export function CronBuilder({
  cron,
  timezone = 'America/New_York',
  onChange,
  onTimezoneChange,
  disabled = false,
  className = '',
}: CronBuilderProps) {
  const [showCustom, setShowCustom] = useState(false)
  const [customValue, setCustomValue] = useState(cron)

  // Check if current cron matches a preset
  const activePreset = useMemo(() => {
    return CRON_PRESETS.find((p) => p.cron === cron)
  }, [cron])

  // Validation state
  const isValid = useMemo(() => isValidCron(cron), [cron])
  const description = useMemo(() => getCronDescription(cron), [cron])

  const handlePresetClick = useCallback(
    (preset: CronPreset) => {
      if (disabled) return
      onChange(preset.cron)
      setCustomValue(preset.cron)
    },
    [onChange, disabled]
  )

  const handleCustomChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const value = e.target.value
      setCustomValue(value)
    },
    []
  )

  const handleCustomBlur = useCallback(() => {
    if (customValue.trim() && customValue !== cron) {
      onChange(customValue.trim())
    }
  }, [customValue, cron, onChange])

  const handleTimezoneChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      if (onTimezoneChange) {
        onTimezoneChange(e.target.value)
      }
    },
    [onTimezoneChange]
  )

  const toggleCustom = useCallback(() => {
    setShowCustom((prev) => !prev)
    if (!showCustom) {
      setCustomValue(cron)
    }
  }, [showCustom, cron])

  return (
    <div className={`cron-builder ${className}`.trim()}>
      {/* Presets */}
      <div
        role="group"
        aria-label="Schedule presets"
        className="cron-builder__presets"
      >
        {CRON_PRESETS.map((preset) => (
          <button
            key={preset.cron}
            type="button"
            onClick={() => handlePresetClick(preset)}
            aria-pressed={activePreset?.cron === preset.cron}
            disabled={disabled}
            className={`cron-builder__preset ${
              activePreset?.cron === preset.cron ? 'cron-builder__preset--active' : ''
            }`}
            title={preset.description}
          >
            {preset.label}
          </button>
        ))}
        <button
          type="button"
          onClick={toggleCustom}
          disabled={disabled}
          className="cron-builder__preset cron-builder__preset--custom"
          aria-expanded={showCustom}
        >
          {showCustom ? (
            <>
              <ChevronUp className="cron-builder__icon" aria-hidden="true" />
              <span>Hide custom</span>
            </>
          ) : (
            <>
              <ChevronDown className="cron-builder__icon" aria-hidden="true" />
              <span>Custom</span>
            </>
          )}
        </button>
      </div>

      {/* Custom cron input */}
      {showCustom && (
        <div className="cron-builder__custom">
          <label htmlFor="cron-expression" className="cron-builder__label">
            Cron expression
          </label>
          <input
            id="cron-expression"
            type="text"
            value={customValue}
            onChange={handleCustomChange}
            onBlur={handleCustomBlur}
            disabled={disabled}
            placeholder="* * * * *"
            className={`cron-builder__input ${
              !isValid ? 'cron-builder__input--error' : ''
            }`}
            aria-describedby="cron-format cron-error"
          />
          <p id="cron-format" className="cron-builder__hint">
            Format: minute hour day-of-month month day-of-week
          </p>
          {!isValid && (
            <p id="cron-error" className="cron-builder__error" role="alert">
              Invalid cron expression
            </p>
          )}
        </div>
      )}

      {/* Description */}
      <div className="cron-builder__description">
        <Clock className="cron-builder__icon" aria-hidden="true" />
        <span>{description}</span>
      </div>

      {/* Timezone selector */}
      <div className="cron-builder__timezone">
        <label htmlFor="cron-timezone" className="cron-builder__label">
          Timezone
        </label>
        <select
          id="cron-timezone"
          value={timezone}
          onChange={handleTimezoneChange}
          disabled={disabled}
          className="cron-builder__select"
        >
          {COMMON_TIMEZONES.map((tz) => (
            <option key={tz.value} value={tz.value}>
              {tz.label}
            </option>
          ))}
        </select>
      </div>
    </div>
  )
}
