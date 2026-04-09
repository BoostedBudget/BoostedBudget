/**
 * Transaction Status Filter Component
 * Toggle buttons for filtering by status: All, Posted, Pending
 * Issue #529: Transaction page filtering
 */

import type { StatusFilter } from '../../types'

interface TransactionStatusFilterProps {
  value: StatusFilter
  onChange: (value: StatusFilter) => void
  pendingCount?: number
  disabled?: boolean
}

const options: { value: StatusFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'posted', label: 'Posted' },
  { value: 'pending', label: 'Pending' },
]

export function TransactionStatusFilter({
  value,
  onChange,
  pendingCount,
  disabled,
}: TransactionStatusFilterProps) {
  return (
    <div className="flex flex-col gap-1">
      <label className="text-sm font-medium text-foreground">Status</label>
      <div className="inline-flex rounded-lg border border-border bg-muted p-1">
        {options.map((option) => (
          <button
            key={option.value}
            type="button"
            onClick={() => onChange(option.value)}
            disabled={disabled}
            className={`
              px-3 py-1.5 text-sm font-medium rounded-md transition-colors relative
              ${
                value === option.value
                  ? 'bg-card text-foreground shadow-sm'
                  : 'text-muted-foreground hover:text-foreground'
              }
              ${disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'}
            `}
            aria-pressed={value === option.value}
          >
            {option.label}
            {option.value === 'pending' && pendingCount !== undefined && pendingCount > 0 && (
              <span className="ml-1.5 inline-flex items-center justify-center px-1.5 py-0.5 text-xs font-medium bg-yellow-100 text-yellow-800 rounded-full">
                {pendingCount}
              </span>
            )}
          </button>
        ))}
      </div>
    </div>
  )
}
