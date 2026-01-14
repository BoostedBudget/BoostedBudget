/**
 * Filter Pill Component
 * Displays an active filter as a removable badge/pill
 * Issue #529: Transaction page filtering
 */

import { XMarkIcon } from '@heroicons/react/24/outline'

interface FilterPillProps {
  label: string
  value: string
  onRemove: () => void
}

export function FilterPill({ label, value, onRemove }: FilterPillProps) {
  return (
    <span className="inline-flex items-center gap-1 px-2 py-1 text-xs font-medium bg-blue-50 text-blue-700 rounded-full border border-blue-200">
      <span className="text-blue-500">{label}:</span>
      <span className="max-w-[150px] truncate">{value}</span>
      <button
        type="button"
        onClick={onRemove}
        className="ml-0.5 p-0.5 rounded-full hover:bg-blue-100 focus:outline-none focus:ring-1 focus:ring-blue-500"
        aria-label={`Remove ${label} filter`}
      >
        <XMarkIcon className="w-3 h-3" />
      </button>
    </span>
  )
}
