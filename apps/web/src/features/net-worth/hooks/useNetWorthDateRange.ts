import { useState, useCallback } from 'react'
import { getDateRange, type DatePreset } from '@/shared/dates'

export interface UseNetWorthDateRangeResult {
  preset: DatePreset
  startDate: string
  endDate: string
  setPreset: (preset: DatePreset) => void
  setCustomRange: (startDate: string, endDate: string) => void
}

export function useNetWorthDateRange(): UseNetWorthDateRangeResult {
  const defaultPreset: DatePreset = 'last_3_months'
  const defaultRange = getDateRange(defaultPreset)

  const [preset, setPresetState] = useState<DatePreset>(defaultPreset)
  const [startDate, setStartDate] = useState(defaultRange?.startDate ?? '')
  const [endDate, setEndDate] = useState(defaultRange?.endDate ?? '')

  const setPreset = useCallback((next: DatePreset) => {
    setPresetState(next)
    if (next === 'custom') return
    const range = getDateRange(next)
    setStartDate(range?.startDate ?? '')
    setEndDate(range?.endDate ?? '')
  }, [])

  const setCustomRange = useCallback((start: string, end: string) => {
    setPresetState('custom')
    setStartDate(start)
    setEndDate(end)
  }, [])

  return { preset, startDate, endDate, setPreset, setCustomRange }
}
