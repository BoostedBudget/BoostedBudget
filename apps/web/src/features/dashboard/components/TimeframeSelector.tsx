import { useState } from 'react'
import { format, subDays, subYears, startOfDay } from 'date-fns'

interface TimeframeSelectorProps {
  onRangeChange: (startDate: string, endDate: string) => void
}

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
 */
export function TimeframeSelector({ onRangeChange }: TimeframeSelectorProps) {
  const [selectedPeriod, setSelectedPeriod] = useState<PresetPeriod>('30d')
  const [customStartDate, setCustomStartDate] = useState('')
  const [customEndDate, setCustomEndDate] = useState('')

  const today = startOfDay(new Date())

  const handlePresetChange = (period: PresetPeriod) => {
    setSelectedPeriod(period)

    const endDate = format(today, 'yyyy-MM-dd')
    let startDate: string

    switch (period) {
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
        // Use a very old date to get all history
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
    <div className="bg-white rounded-lg shadow p-6" data-testid="timeframe-selector">
      <h3 className="text-lg font-semibold text-gray-900 mb-4">Timeframe</h3>

      {/* Preset buttons */}
      <div className="grid grid-cols-2 gap-2 mb-4">
        {presetButtons.map((button) => (
          <button
            key={button.value}
            onClick={() => handlePresetChange(button.value)}
            className={`px-4 py-2 text-sm font-medium rounded-lg transition-colors ${
              selectedPeriod === button.value
                ? 'bg-blue-600 text-white'
                : 'bg-gray-100 text-gray-700 hover:bg-gray-200'
            }`}
          >
            {button.label}
          </button>
        ))}
      </div>

      {/* Custom date range inputs */}
      {selectedPeriod === 'custom' && (
        <div className="space-y-3 pt-3 border-t border-gray-200">
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">
              Start Date
            </label>
            <input
              type="date"
              value={customStartDate}
              onChange={(e) => setCustomStartDate(e.target.value)}
              max={customEndDate || format(today, 'yyyy-MM-dd')}
              className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">
              End Date
            </label>
            <input
              type="date"
              value={customEndDate}
              onChange={(e) => setCustomEndDate(e.target.value)}
              min={customStartDate}
              max={format(today, 'yyyy-MM-dd')}
              className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
            />
          </div>
          <button
            onClick={handleCustomApply}
            disabled={!customStartDate || !customEndDate}
            className="w-full px-4 py-2 bg-blue-600 text-white font-medium rounded-lg hover:bg-blue-700 disabled:bg-gray-300 disabled:cursor-not-allowed transition-colors"
          >
            Apply Custom Range
          </button>
        </div>
      )}
    </div>
  )
}
