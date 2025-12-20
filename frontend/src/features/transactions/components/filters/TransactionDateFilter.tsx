/**
 * Transaction Date Filter Component
 * Date presets with custom date range option
 * Issue #529: Transaction page filtering
 */

import { useState, useCallback } from 'react'
import { format } from 'date-fns'
import type { DatePreset } from '../../types'

interface TransactionDateFilterProps {
  preset: DatePreset
  startDate?: string
  endDate?: string
  onPresetChange: (preset: DatePreset) => void
  onCustomRangeChange: (startDate: string, endDate: string) => void
  disabled?: boolean
}

const presets: { value: DatePreset; label: string }[] = [
  { value: '7d', label: '7 days' },
  { value: '30d', label: '30 days' },
  { value: '90d', label: '90 days' },
  { value: '1y', label: '1 year' },
  { value: 'all', label: 'All time' },
  { value: 'custom', label: 'Custom' },
]

export function TransactionDateFilter({
  preset,
  startDate,
  endDate,
  onPresetChange,
  onCustomRangeChange,
  disabled,
}: TransactionDateFilterProps) {
  const [showCustom, setShowCustom] = useState(preset === 'custom')
  const [localStartDate, setLocalStartDate] = useState(startDate || '')
  const [localEndDate, setLocalEndDate] = useState(endDate || '')

  const today = format(new Date(), 'yyyy-MM-dd')

  const handlePresetClick = useCallback(
    (value: DatePreset) => {
      if (value === 'custom') {
        setShowCustom(true)
      } else {
        setShowCustom(false)
        onPresetChange(value)
      }
    },
    [onPresetChange]
  )

  const handleApplyCustomRange = useCallback(() => {
    if (localStartDate && localEndDate) {
      onCustomRangeChange(localStartDate, localEndDate)
    }
  }, [localStartDate, localEndDate, onCustomRangeChange])

  return (
    <div className="flex flex-col gap-2">
      <label className="text-sm font-medium text-gray-700">Date Range</label>

      {/* Preset buttons */}
      <div className="flex flex-wrap gap-1">
        {presets.map((option) => (
          <button
            key={option.value}
            type="button"
            onClick={() => handlePresetClick(option.value)}
            disabled={disabled}
            className={`
              px-3 py-1.5 text-sm font-medium rounded-lg transition-colors
              ${
                preset === option.value
                  ? 'bg-blue-600 text-white'
                  : 'bg-gray-100 text-gray-700 hover:bg-gray-200'
              }
              ${disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'}
            `}
            aria-pressed={preset === option.value}
          >
            {option.label}
          </button>
        ))}
      </div>

      {/* Custom date inputs */}
      {showCustom && (
        <div className="flex flex-col gap-2 mt-2 p-3 bg-gray-50 rounded-lg border border-gray-200">
          <div className="flex items-center gap-2">
            <div className="flex-1">
              <label className="block text-xs text-gray-500 mb-1">Start date</label>
              <input
                type="date"
                value={localStartDate}
                onChange={(e) => setLocalStartDate(e.target.value)}
                max={localEndDate || today}
                disabled={disabled}
                className={`
                  w-full px-3 py-2 text-sm border border-gray-300 rounded-lg
                  focus:ring-2 focus:ring-blue-500 focus:border-blue-500
                  ${disabled ? 'bg-gray-100 cursor-not-allowed' : 'bg-white'}
                `}
              />
            </div>
            <span className="text-gray-400 pt-5">-</span>
            <div className="flex-1">
              <label className="block text-xs text-gray-500 mb-1">End date</label>
              <input
                type="date"
                value={localEndDate}
                onChange={(e) => setLocalEndDate(e.target.value)}
                min={localStartDate}
                max={today}
                disabled={disabled}
                className={`
                  w-full px-3 py-2 text-sm border border-gray-300 rounded-lg
                  focus:ring-2 focus:ring-blue-500 focus:border-blue-500
                  ${disabled ? 'bg-gray-100 cursor-not-allowed' : 'bg-white'}
                `}
              />
            </div>
          </div>
          <button
            type="button"
            onClick={handleApplyCustomRange}
            disabled={disabled || !localStartDate || !localEndDate}
            className={`
              px-4 py-2 text-sm font-medium text-white rounded-lg
              ${
                disabled || !localStartDate || !localEndDate
                  ? 'bg-gray-400 cursor-not-allowed'
                  : 'bg-blue-600 hover:bg-blue-700'
              }
            `}
          >
            Apply Custom Range
          </button>
        </div>
      )}
    </div>
  )
}
