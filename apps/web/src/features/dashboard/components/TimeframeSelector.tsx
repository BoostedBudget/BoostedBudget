import { useState } from 'react'
import { format, subDays, subYears, startOfDay } from 'date-fns'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Button } from '@/components/ui/button'

interface TimeframeSelectorProps {
  onRangeChange: (startDate: string, endDate: string) => void
}

const dateInputClass = "w-full px-3 py-2 rounded-md border border-input bg-background text-foreground focus:ring-2 focus:ring-blue-500 focus:border-blue-500 text-sm"

type PresetPeriod = '7d' | '30d' | '90d' | '1y' | 'all' | 'custom'

/**
 * TimeframeSelector component for choosing date ranges
 *
 * Features:
 * - Preset periods (7 days, 30 days, 90 days, 1 year, all time)
 * - Custom date range picker
 * - ISO date format for API compatibility
 *
 * Issue #353: Balance Tracking Dashboard
 * Issue #832: Design system migration (ToggleGroup)
 */
export function TimeframeSelector({ onRangeChange }: TimeframeSelectorProps) {
  const [selectedPeriod, setSelectedPeriod] = useState<PresetPeriod>('30d')
  const [customStartDate, setCustomStartDate] = useState('')
  const [customEndDate, setCustomEndDate] = useState('')

  const today = startOfDay(new Date())

  const handlePresetChange = (period: string) => {
    // When clicking already-selected item, Radix fires '' (deselect) — use current period
    const effectivePeriod = (period || selectedPeriod) as PresetPeriod
    setSelectedPeriod(effectivePeriod)

    const endDate = format(today, 'yyyy-MM-dd')
    let startDate: string

    switch (effectivePeriod) {
      case '7d':
        startDate = format(subDays(today, 7), 'yyyy-MM-dd')
        break
      case '30d':
        startDate = format(subDays(today, 30), 'yyyy-MM-dd')
        break
      case '90d':
        startDate = format(subDays(today, 90), 'yyyy-MM-dd')
        break
      case '1y':
        startDate = format(subYears(today, 1), 'yyyy-MM-dd')
        break
      case 'all':
        startDate = format(subYears(today, 10), 'yyyy-MM-dd')
        break
      case 'custom':
        return // Don't trigger range change for custom
      default:
        startDate = format(subDays(today, 30), 'yyyy-MM-dd')
    }

    onRangeChange(startDate, endDate)
  }

  const handleCustomApply = () => {
    if (customStartDate && customEndDate) {
      onRangeChange(customStartDate, customEndDate)
    }
  }

  const presetButtons: Array<{ value: PresetPeriod; label: string }> = [
    { value: '7d', label: '7 Days' },
    { value: '30d', label: '30 Days' },
    { value: '90d', label: '90 Days' },
    { value: '1y', label: '1 Year' },
    { value: 'all', label: 'All Time' },
    { value: 'custom', label: 'Custom' },
  ]

  return (
    <div data-testid="timeframe-selector">
      <ToggleGroup
        type="single"
        value={selectedPeriod}
        onValueChange={handlePresetChange}
      >
        {presetButtons.map((button) => (
          <ToggleGroupItem key={button.value} value={button.value}>
            {button.label}
          </ToggleGroupItem>
        ))}
      </ToggleGroup>

      {selectedPeriod === 'custom' && (
        <div className="mt-3 space-y-3 rounded-lg border border-input bg-background p-3">
          <div>
            <label className="block text-sm font-medium text-foreground mb-1">
              Start Date
            </label>
            <input
              type="date"
              value={customStartDate}
              onChange={(e) => setCustomStartDate(e.target.value)}
              max={customEndDate || format(today, 'yyyy-MM-dd')}
              className={dateInputClass}
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-foreground mb-1">
              End Date
            </label>
            <input
              type="date"
              value={customEndDate}
              onChange={(e) => setCustomEndDate(e.target.value)}
              min={customStartDate}
              max={format(today, 'yyyy-MM-dd')}
              className={dateInputClass}
            />
          </div>
          <Button
            onClick={handleCustomApply}
            disabled={!customStartDate || !customEndDate}
            className="w-full"
          >
            Apply Custom Range
          </Button>
        </div>
      )}
    </div>
  )
}
