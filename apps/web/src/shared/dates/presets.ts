import {
  format,
  startOfWeek,
  startOfMonth,
  endOfMonth,
  startOfYear,
  subMonths,
} from 'date-fns'

const API_DATE_FORMAT = 'yyyy-MM-dd'

export type DatePreset =
  | 'this_week'
  | 'this_month'
  | 'last_month'
  | 'last_3_months'
  | 'last_6_months'
  | 'ytd'
  | 'all'
  | 'custom'

export interface DateRange {
  /** YYYY-MM-DD */
  startDate: string
  /** YYYY-MM-DD */
  endDate: string
}

export interface PresetDefinition {
  value: DatePreset
  label: string
  getRange: () => DateRange | null
}

function fmt(d: Date): string {
  return format(d, API_DATE_FORMAT)
}

export const PRESETS: PresetDefinition[] = [
  {
    value: 'this_week',
    label: 'This Week',
    getRange: () => {
      const today = new Date()
      return {
        startDate: fmt(startOfWeek(today, { weekStartsOn: 1 })),
        endDate: fmt(today),
      }
    },
  },
  {
    value: 'this_month',
    label: 'This Month',
    getRange: () => {
      const today = new Date()
      return {
        startDate: fmt(startOfMonth(today)),
        endDate: fmt(endOfMonth(today)),
      }
    },
  },
  {
    value: 'last_month',
    label: 'Last Month',
    getRange: () => {
      const prevMonth = subMonths(new Date(), 1)
      return {
        startDate: fmt(startOfMonth(prevMonth)),
        endDate: fmt(endOfMonth(prevMonth)),
      }
    },
  },
  {
    value: 'last_3_months',
    label: 'Last 3 Months',
    getRange: () => {
      const today = new Date()
      return {
        startDate: fmt(startOfMonth(subMonths(today, 3))),
        endDate: fmt(endOfMonth(subMonths(today, 1))),
      }
    },
  },
  {
    value: 'last_6_months',
    label: 'Last 6 Months',
    getRange: () => {
      const today = new Date()
      return {
        startDate: fmt(startOfMonth(subMonths(today, 6))),
        endDate: fmt(endOfMonth(subMonths(today, 1))),
      }
    },
  },
  {
    value: 'ytd',
    label: 'Year to Date',
    getRange: () => {
      const today = new Date()
      return {
        startDate: fmt(startOfYear(today)),
        endDate: fmt(today),
      }
    },
  },
  {
    value: 'all',
    label: 'All Time',
    getRange: () => null,
  },
  {
    value: 'custom',
    label: 'Custom',
    getRange: () => null,
  },
]

/** Returns the date range for a preset, or null for 'all' and 'custom'. */
export function getDateRange(preset: DatePreset): DateRange | null {
  const def = PRESETS.find((p) => p.value === preset)
  return def?.getRange() ?? null
}
