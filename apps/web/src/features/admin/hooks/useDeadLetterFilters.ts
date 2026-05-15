/**
 * useDeadLetterFilters Hook
 * Issue #633: Dead Letter Queue Admin UI
 *
 * Simple state management for DLQ list filters.
 */

import { useState, useCallback } from 'react'

export interface DeadLetterFilters {
  kind: string
  userId: string
  retried: string // '' | 'true' | 'false'
  startDate: string
  endDate: string
  page: number
}

export interface UseDeadLetterFiltersResult {
  filters: DeadLetterFilters
  setKind: (v: string) => void
  setUserId: (v: string) => void
  setRetried: (v: string) => void
  setStartDate: (v: string) => void
  setEndDate: (v: string) => void
  setPage: (v: number) => void
  hasActiveFilters: boolean
  resetFilters: () => void
}

const DEFAULT_FILTERS: DeadLetterFilters = {
  kind: '',
  userId: '',
  retried: '',
  startDate: '',
  endDate: '',
  page: 1,
}

export function useDeadLetterFilters(): UseDeadLetterFiltersResult {
  const [filters, setFilters] = useState<DeadLetterFilters>(DEFAULT_FILTERS)

  const setKind = useCallback(
    (v: string) => setFilters((prev) => ({ ...prev, kind: v, page: 1 })),
    []
  )

  const setUserId = useCallback(
    (v: string) => setFilters((prev) => ({ ...prev, userId: v, page: 1 })),
    []
  )

  const setRetried = useCallback(
    (v: string) => setFilters((prev) => ({ ...prev, retried: v, page: 1 })),
    []
  )

  const setStartDate = useCallback(
    (v: string) => setFilters((prev) => ({ ...prev, startDate: v, page: 1 })),
    []
  )

  const setEndDate = useCallback(
    (v: string) => setFilters((prev) => ({ ...prev, endDate: v, page: 1 })),
    []
  )

  const setPage = useCallback((v: number) => setFilters((prev) => ({ ...prev, page: v })), [])

  const hasActiveFilters =
    filters.kind !== '' ||
    filters.userId !== '' ||
    filters.retried !== '' ||
    filters.startDate !== '' ||
    filters.endDate !== ''

  const resetFilters = useCallback(() => setFilters(DEFAULT_FILTERS), [])

  return {
    filters,
    setKind,
    setUserId,
    setRetried,
    setStartDate,
    setEndDate,
    setPage,
    hasActiveFilters,
    resetFilters,
  }
}
