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

// PII review (issue #1514, G5): `aria-label={`Remove ${label} filter`}` intentionally only
// uses `label`, never `value`. `label` is drawn from a fixed, closed set of filter-type
// strings ('Date' | 'Status' | 'Investments' | 'Activity' — see
// TransactionFilterPanel.tsx's FilterPillsSection), not user-entered free text, so it carries
// no PII. `value` (the actual filter selection, potentially free text) is rendered visibly but
// deliberately kept out of the aria-label.
export function FilterPill({ label, value, onRemove }: FilterPillProps) {
  return (
    <span className="inline-flex items-center gap-1 px-2 py-1 text-xs font-medium bg-info/10 text-info rounded-full border border-info/20">
      <span className="text-info/70">{label}:</span>
      <span data-ph-mask className="max-w-[150px] truncate">{value}</span>
      <button
        type="button"
        onClick={onRemove}
        className="ml-0.5 p-0.5 rounded-full hover:bg-info/20 focus:outline-none focus:ring-1 focus:ring-ring"
        aria-label={`Remove ${label} filter`}
      >
        <XMarkIcon className="w-3 h-3" />
      </button>
    </span>
  )
}
