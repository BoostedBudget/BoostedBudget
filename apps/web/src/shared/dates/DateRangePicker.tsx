import { useState } from 'react'
import { format } from 'date-fns'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Button } from '@/components/ui/button'
import { PRESETS, type DatePreset } from './presets'

interface DateRangePickerProps {
  preset: DatePreset
  startDate?: string
  endDate?: string
  onPresetChange: (preset: DatePreset) => void
  onCustomRangeChange: (startDate: string, endDate: string) => void
  excludePresets?: DatePreset[]
  disabled?: boolean
}

const dateInputClass =
  'w-full px-3 py-2 rounded-md border border-input bg-background text-foreground focus:ring-2 focus:ring-blue-500 focus:border-blue-500 text-sm'

export function DateRangePicker({
  preset,
  startDate,
  endDate,
  onPresetChange,
  onCustomRangeChange,
  excludePresets = [],
  disabled = false,
}: DateRangePickerProps) {
  const [customStart, setCustomStart] = useState(startDate ?? '')
  const [customEnd, setCustomEnd] = useState(endDate ?? '')

  const today = format(new Date(), 'yyyy-MM-dd')

  const visiblePresets = PRESETS.filter((p) => !excludePresets.includes(p.value))

  const handleValueChange = (value: string) => {
    if (!value || disabled) return
    onPresetChange(value as DatePreset)
  }

  return (
    <div>
      <ToggleGroup
        type="single"
        value={preset}
        onValueChange={handleValueChange}
        disabled={disabled}
      >
        {visiblePresets.map((p) => (
          <ToggleGroupItem key={p.value} value={p.value}>
            {p.label}
          </ToggleGroupItem>
        ))}
      </ToggleGroup>

      {preset === 'custom' && (
        <div className="mt-3 space-y-3 rounded-lg border border-input bg-background p-3">
          <div>
            <label htmlFor="drp-start-date" className="block text-sm font-medium text-foreground mb-1">
              Start Date
            </label>
            <input
              id="drp-start-date"
              type="date"
              value={customStart}
              onChange={(e) => setCustomStart(e.target.value)}
              max={customEnd || today}
              disabled={disabled}
              className={dateInputClass}
            />
          </div>
          <div>
            <label htmlFor="drp-end-date" className="block text-sm font-medium text-foreground mb-1">
              End Date
            </label>
            <input
              id="drp-end-date"
              type="date"
              value={customEnd}
              onChange={(e) => setCustomEnd(e.target.value)}
              min={customStart}
              max={today}
              disabled={disabled}
              className={dateInputClass}
            />
          </div>
          <Button
            onClick={() => onCustomRangeChange(customStart, customEnd)}
            disabled={!customStart || !customEnd || disabled}
            className="w-full"
          >
            Apply Custom Range
          </Button>
        </div>
      )}
    </div>
  )
}
