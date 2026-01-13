/**
 * Transaction Amount Filter Component
 * Min/max amount inputs for filtering by transaction amount range
 * Includes quick preset shortcuts
 * Issue #529: Transaction page filtering
 */

import { useState, useCallback, useEffect } from 'react'

interface TransactionAmountFilterProps {
  minAmount?: number
  maxAmount?: number
  onChange: (min?: number, max?: number) => void
  disabled?: boolean
}

// Quick preset shortcuts for common amount ranges
const amountShortcuts = [
  { label: '<$25', min: undefined, max: 25 },
  { label: '$50+', min: 50, max: undefined },
  { label: '$100+', min: 100, max: undefined },
  { label: '$500+', min: 500, max: undefined },
]

/**
 * Format number as currency string (without $ symbol for input)
 */
function formatAmount(value: number | undefined): string {
  if (value === undefined) return ''
  return value.toFixed(2)
}

/**
 * Parse currency string to number
 */
function parseAmount(value: string): number | undefined {
  const cleaned = value.replace(/[^0-9.]/g, '')
  if (cleaned === '') return undefined
  const parsed = parseFloat(cleaned)
  return isNaN(parsed) ? undefined : parsed
}

export function TransactionAmountFilter({
  minAmount,
  maxAmount,
  onChange,
  disabled,
}: TransactionAmountFilterProps) {
  // Local state for controlled inputs
  const [minValue, setMinValue] = useState(formatAmount(minAmount))
  const [maxValue, setMaxValue] = useState(formatAmount(maxAmount))

  // Sync with external changes
  useEffect(() => {
    setMinValue(formatAmount(minAmount))
  }, [minAmount])

  useEffect(() => {
    setMaxValue(formatAmount(maxAmount))
  }, [maxAmount])

  // Handle blur to commit changes
  const handleMinBlur = useCallback(() => {
    const parsed = parseAmount(minValue)
    onChange(parsed, maxAmount)
  }, [minValue, maxAmount, onChange])

  const handleMaxBlur = useCallback(() => {
    const parsed = parseAmount(maxValue)
    onChange(minAmount, parsed)
  }, [maxValue, minAmount, onChange])

  // Handle Enter key
  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent, type: 'min' | 'max') => {
      if (e.key === 'Enter') {
        if (type === 'min') {
          handleMinBlur()
        } else {
          handleMaxBlur()
        }
      }
    },
    [handleMinBlur, handleMaxBlur]
  )

  // Check if a shortcut is currently active
  const isShortcutActive = (shortcut: (typeof amountShortcuts)[0]) => {
    return shortcut.min === minAmount && shortcut.max === maxAmount
  }

  // Handle shortcut click
  const handleShortcutClick = (shortcut: (typeof amountShortcuts)[0]) => {
    // If already selected, clear it
    if (isShortcutActive(shortcut)) {
      onChange(undefined, undefined)
      setMinValue('')
      setMaxValue('')
    } else {
      onChange(shortcut.min, shortcut.max)
      setMinValue(formatAmount(shortcut.min))
      setMaxValue(formatAmount(shortcut.max))
    }
  }

  return (
    <div className="flex flex-col gap-1">
      <label className="text-sm font-medium text-gray-700">Amount Range</label>

      {/* Quick shortcuts */}
      <div className="flex flex-wrap gap-1 mb-1">
        {amountShortcuts.map((shortcut) => (
          <button
            key={shortcut.label}
            type="button"
            onClick={() => handleShortcutClick(shortcut)}
            disabled={disabled}
            className={`
              px-2 py-1 text-xs font-medium rounded-md transition-colors
              ${
                isShortcutActive(shortcut)
                  ? 'bg-blue-600 text-white'
                  : 'bg-gray-100 text-gray-700 hover:bg-gray-200'
              }
              ${disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'}
            `}
          >
            {shortcut.label}
          </button>
        ))}
      </div>

      <div className="flex items-center gap-2">
        <div className="relative flex-1">
          <span className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500">$</span>
          <input
            type="text"
            inputMode="decimal"
            placeholder="Min"
            value={minValue}
            onChange={(e) => setMinValue(e.target.value)}
            onBlur={handleMinBlur}
            onKeyDown={(e) => handleKeyDown(e, 'min')}
            disabled={disabled}
            className={`
              w-full pl-7 pr-3 py-2 text-sm border border-gray-300 rounded-lg
              focus:ring-2 focus:ring-blue-500 focus:border-blue-500
              ${disabled ? 'bg-gray-100 cursor-not-allowed' : 'bg-white'}
            `}
            aria-label="Minimum amount"
          />
        </div>
        <span className="text-gray-400">-</span>
        <div className="relative flex-1">
          <span className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500">$</span>
          <input
            type="text"
            inputMode="decimal"
            placeholder="Max"
            value={maxValue}
            onChange={(e) => setMaxValue(e.target.value)}
            onBlur={handleMaxBlur}
            onKeyDown={(e) => handleKeyDown(e, 'max')}
            disabled={disabled}
            className={`
              w-full pl-7 pr-3 py-2 text-sm border border-gray-300 rounded-lg
              focus:ring-2 focus:ring-blue-500 focus:border-blue-500
              ${disabled ? 'bg-gray-100 cursor-not-allowed' : 'bg-white'}
            `}
            aria-label="Maximum amount"
          />
        </div>
      </div>
    </div>
  )
}
