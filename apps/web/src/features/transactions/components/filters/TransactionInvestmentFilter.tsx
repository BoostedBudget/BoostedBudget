/**
 * TransactionInvestmentFilter Component
 * Toggle and multi-select for filtering investment activity in the unified feed
 * Issue #1299: Investment transactions in unified feed
 */

import { Switch } from '@/components/ui/switch'
import { ACTIVITY_TYPE_CONFIG, ACTIVITY_TYPES } from '@/shared/components/ActivityTypeChip'
import type { ActivityType } from '../../types'

// ACTIVITY_TYPES is imported rather than re-declared here (#1631). It remains a hand-written
// literal at its definition site for the same reason this file used to hold its own copy:
// Object.keys() at module level trips a cross-chunk TDZ in the Rolldown production bundle.

interface TransactionInvestmentFilterProps {
  includeInvestmentActivity: boolean
  activityTypes: ActivityType[]
  onIncludeChange: (val: boolean) => void
  onActivityTypesChange: (types: ActivityType[]) => void
  disabled?: boolean
}

export function TransactionInvestmentFilter({
  includeInvestmentActivity,
  activityTypes,
  onIncludeChange,
  onActivityTypesChange,
  disabled,
}: TransactionInvestmentFilterProps) {
  const handleTypeToggle = (type: ActivityType) => {
    const isSelected = activityTypes.includes(type)
    const updated = isSelected
      ? activityTypes.filter((t) => t !== type)
      : [...activityTypes, type]
    onActivityTypesChange(updated)
  }

  const activityTypeLabel =
    activityTypes.length === 0 ? 'All types' : `${activityTypes.length} selected`

  return (
    <div className="flex flex-col gap-3">
      {/* Toggle row */}
      <div className="flex items-center justify-between gap-3">
        <label
          htmlFor="include-investment-activity-switch"
          className="text-sm font-medium text-foreground cursor-pointer"
        >
          Include investment activity
        </label>
        <Switch
          id="include-investment-activity-switch"
          aria-label="Include investment activity"
          checked={includeInvestmentActivity}
          onCheckedChange={onIncludeChange}
          disabled={disabled}
        />
      </div>

      {/* Activity type multiselect — visible only when toggle is ON */}
      {includeInvestmentActivity && (
        <div
          role="group"
          aria-label="Activity types"
          className="flex flex-col gap-1.5 pl-1"
        >
          <div className="flex items-center justify-between">
            <span className="text-xs font-medium text-muted-foreground">Activity types</span>
            <span className="text-xs text-muted-foreground">{activityTypeLabel}</span>
          </div>
          <div className="flex flex-wrap gap-x-4 gap-y-1.5">
            {ACTIVITY_TYPES.map((type) => {
              const config = ACTIVITY_TYPE_CONFIG[type]
              const checked = activityTypes.includes(type)
              return (
                <label
                  key={type}
                  className="flex items-center gap-1.5 cursor-pointer text-sm text-foreground"
                >
                  <input
                    type="checkbox"
                    role="checkbox"
                    aria-label={config.label}
                    checked={checked}
                    onChange={() => handleTypeToggle(type)}
                    disabled={disabled}
                    className="h-3.5 w-3.5 rounded border-border text-primary accent-primary cursor-pointer"
                  />
                  <span>{config.label}</span>
                </label>
              )
            })}
          </div>
        </div>
      )}
    </div>
  )
}
