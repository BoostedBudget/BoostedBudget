/**
 * usePortfolioTimeRange Hook
 * Issue #1296: Preset-based time range for portfolio charts.
 *
 * Maps the 6 human-facing presets (1W/1M/3M/YTD/1Y/All) to concrete
 * `{ start_date, end_date }` strings in YYYY-MM-DD format.
 *
 * 'All' resolves to PORTFOLIO_ALL_FLOOR (a fixed floor far enough back that the
 * API will return all available data) instead of empty strings — the portfolio
 * history endpoint requires both dates and returns HTTP 400 on empty input.
 */

import { useState, useMemo } from 'react'

/** Floor date for the 'All' preset. Chosen to pre-date any realistic user data. */
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
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

function computeDateRange(preset: PortfolioPreset): PortfolioDateRange {
  const today = new Date()
  const end_date = toISO(today)

  switch (preset) {
    case '1W': {
      const start = new Date(today)
      start.setDate(start.getDate() - 7)
      return { start_date: toISO(start), end_date }
    }
    case '1M': {
      const start = new Date(today)
      start.setMonth(start.getMonth() - 1)
      return { start_date: toISO(start), end_date }
    }
    case '3M': {
      const start = new Date(today)
      start.setMonth(start.getMonth() - 3)
      return { start_date: toISO(start), end_date }
    }
    case 'YTD': {
      return { start_date: `${today.getFullYear()}-01-01`, end_date }
    }
    case '1Y': {
      const start = new Date(today)
      start.setFullYear(start.getFullYear() - 1)
      return { start_date: toISO(start), end_date }
    }
    case 'All': {
      return { start_date: PORTFOLIO_ALL_FLOOR, end_date }
    }
  }
}

/**
 * Returns `{ preset, setPreset, dateRange }` for the portfolio time range selector.
 *
 * Default preset: '1Y'.
 * 'All' resolves to PORTFOLIO_ALL_FLOOR (not empty strings) so the history API never 400s.
 */
export function usePortfolioTimeRange(defaultPreset: PortfolioPreset = '1Y') {
  const [preset, setPreset] = useState<PortfolioPreset>(defaultPreset)

  const dateRange = useMemo(() => computeDateRange(preset), [preset])

  return { preset, setPreset, dateRange }
}
