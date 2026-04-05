/**
 * Transaction Date Filter Component
 * Issue #529: Transaction page filtering
 * Issue #953: Migrated to shared DateRangePicker with calendar-aligned presets
 */

import { DateRangePicker } from '@/shared/dates'
import type { DatePreset } from '@/shared/dates'

interface TransactionDateFilterProps {
  preset: DatePreset
  startDate?: string
  endDate?: string
  onPresetChange: (preset: DatePreset) => void
  onCustomRangeChange: (startDate: string, endDate: string) => void
  disabled?: boolean
}

export function TransactionDateFilter(props: TransactionDateFilterProps) {
  return (
    <div className="flex flex-col gap-2">
      <label className="text-sm font-medium text-gray-700">Date Range</label>
      <DateRangePicker {...props} />
    </div>
  )
}
