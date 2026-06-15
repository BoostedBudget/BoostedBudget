import { useState, useMemo } from 'react'

export const PORTFOLIO_ALL_FLOOR = '2010-01-01'

export const PORTFOLIO_PRESETS = ['1W', '1M', '3M', 'YTD', '1Y', 'All'] as const
export type PortfolioPreset = (typeof PORTFOLIO_PRESETS)[number]

export interface PortfolioDateRange {
  start_date: string
  end_date: string
}

function toISO(d: Date): string {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const d2 = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${d2}`
}

/** Subtract n calendar months, clamping to the last day of the target month.
 * Prevents JS Date.setMonth() overflow (e.g. Mar 31 - 1M → Feb 28, not Mar 3). */
function subMonths(from: Date, n: number): Date {
  const d = new Date(from)
  const day = d.getDate()
  d.setDate(1)
  d.setMonth(d.getMonth() - n)
  const lastDay = new Date(d.getFullYear(), d.getMonth() + 1, 0).getDate()
  d.setDate(Math.min(day, lastDay))
  return d
}

function computeDateRange(preset: PortfolioPreset): PortfolioDateRange {
  const today = new Date()
  const end_date = toISO(today)
  const start = new Date(today)

  switch (preset) {
    case '1W':
      start.setDate(start.getDate() - 7)
      break
    case '1M':
      return { start_date: toISO(subMonths(today, 1)), end_date }
    case '3M':
      return { start_date: toISO(subMonths(today, 3)), end_date }
    case '1Y':
      start.setFullYear(start.getFullYear() - 1)
      break
    case 'YTD':
      return { start_date: `${today.getFullYear()}-01-01`, end_date }
    case 'All':
      return { start_date: PORTFOLIO_ALL_FLOOR, end_date }
  }

  return { start_date: toISO(start), end_date }
}

export function usePortfolioTimeRange(defaultPreset: PortfolioPreset = '1Y') {
  const [preset, setPreset] = useState<PortfolioPreset>(defaultPreset)

  const dateRange = useMemo(() => computeDateRange(preset), [preset])

  return { preset, setPreset, dateRange }
}
