import { format } from 'date-fns'
import { type DatePreset, PRESETS } from './presets'

const API_DATE_FORMAT = 'yyyy-MM-dd'

/** Format a Date to YYYY-MM-DD for API params. */
export function formatDateForAPI(d: Date): string {
  return format(d, API_DATE_FORMAT)
}

/**
 * Reverse-map a start/end date pair back to a DatePreset value.
 * Returns 'all' if both are empty, 'custom' if no preset matches.
 */
export function inferPreset(
  startDate?: string,
  endDate?: string
): DatePreset {
  if (!startDate && !endDate) return 'all'

  // Try each non-null, non-custom preset
  const candidates: DatePreset[] = [
    'this_week',
    'this_month',
    'last_month',
    'last_3_months',
    'last_6_months',
    'ytd',
  ]

  for (const preset of candidates) {
    const def = PRESETS.find((p) => p.value === preset)
    const range = def?.getRange()
    if (range && range.startDate === startDate && range.endDate === endDate) {
      return preset
    }
  }

  return 'custom'
}
