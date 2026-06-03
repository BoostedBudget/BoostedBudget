import { useState } from 'react'
import { DateRangePicker, getDateRange, type DatePreset } from '@/shared/dates'

interface TimeframeSelectorProps {
  onRangeChange: (startDate: string, endDate: string) => void
}

/**
 * TimeframeSelector component for choosing date ranges
 *
 * Features:
 * - Preset periods: This Week, This Month, Last Month (calendar-aligned), Last 3/6 Months
 *   (rolling windows ending today — see #1173), YTD, All Time
 * - Custom date range picker
 * - ISO date format for API compatibility
 *
 * Issue #353: Balance Tracking Dashboard
 * Issue #832: Design system migration (ToggleGroup)
 * Issue #953: Migrated to shared DateRangePicker
 * Issue #1173: last_3_months / last_6_months changed to rolling windows ending today
 */
export function TimeframeSelector({ onRangeChange }: TimeframeSelectorProps) {
  const [preset, setPreset] = useState<DatePreset>('this_month')

  const handlePresetChange = (newPreset: DatePreset) => {
    setPreset(newPreset)
    if (newPreset === 'custom') return

    const range = getDateRange(newPreset)
    if (range) {
      onRangeChange(range.startDate, range.endDate)
    } else {
      // 'all' — clear date filter
      onRangeChange('', '')
    }
  }

  const handleCustomRangeChange = (startDate: string, endDate: string) => {
    onRangeChange(startDate, endDate)
  }

  return (
    <div data-testid="timeframe-selector">
      <DateRangePicker
        preset={preset}
        onPresetChange={handlePresetChange}
        onCustomRangeChange={handleCustomRangeChange}
      />
    </div>
  )
}
