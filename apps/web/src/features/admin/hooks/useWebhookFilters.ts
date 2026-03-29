/**
 * useWebhookFilters Hook
 * Issue #747: Paddle Webhook Admin UI
 *
 * Simple state management for webhook list filters.
 * Uses plain useState (not URL params) since webhook filters don't need bookmarking.
 */

import { useState, useCallback } from 'react'

export interface WebhookFilters {
  eventType: string
  processed: string // '' | 'true' | 'false'
  startDate: string
  endDate: string
  page: number
}

export interface UseWebhookFiltersResult {
  filters: WebhookFilters
  setEventType: (v: string) => void
  setProcessed: (v: string) => void
  setStartDate: (v: string) => void
  setEndDate: (v: string) => void
  setPage: (v: number) => void
  hasActiveFilters: boolean
  resetFilters: () => void
}

const DEFAULT_FILTERS: WebhookFilters = {
  eventType: '',
  processed: '',
  startDate: '',
  endDate: '',
  page: 1,
}

export function useWebhookFilters(): UseWebhookFiltersResult {
  const [filters, setFilters] = useState<WebhookFilters>(DEFAULT_FILTERS)

  const setEventType = useCallback(
    (v: string) => setFilters((prev) => ({ ...prev, eventType: v, page: 1 })),
    []
  )

  const setProcessed = useCallback(
    (v: string) => setFilters((prev) => ({ ...prev, processed: v, page: 1 })),
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
    filters.eventType !== '' ||
    filters.processed !== '' ||
    filters.startDate !== '' ||
    filters.endDate !== ''

  const resetFilters = useCallback(() => setFilters(DEFAULT_FILTERS), [])

  return {
    filters,
    setEventType,
    setProcessed,
    setStartDate,
    setEndDate,
    setPage,
    hasActiveFilters,
    resetFilters,
  }
}
