/**
 * useWebhookFilters Hook
 * Issue #747: Paddle Webhook Admin UI
 * Issue #1148: Server-side sort for admin tables
 *
 * State management for webhook list filters.
 * Uses plain useState for filter fields (not URL params) since webhook filters
 * don't need bookmarking. Sort state is URL-synced via useSearchParams so
 * users can share/bookmark sorted views.
 */

import { useState, useCallback, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import type { ListAdminPaddleWebhooksParams, ListAdminPaddleWebhooksSortBy, ListAdminPaddleWebhooksSortDir } from '@boostedbudget/api-client/models'
import { parseSortParam, serializeSortParam } from '@/shared/hooks/useSortUrlState'
import type { SortState } from '@/shared/components/DataTable'

const PAGE_SIZE = 20

const WEBHOOKS_SORT_COLUMNS = new Set([
  'created_at',
  'attempts',
  'status',
]) as ReadonlySet<string>

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
  /** Current sort state (null = unsorted) */
  sort: SortState
  /** Set sort state; passing null clears sort. Resets page to 1. */
  setSort: (next: SortState) => void
  /** Params formatted for API call */
  apiParams: ListAdminPaddleWebhooksParams
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
  const [searchParams, setSearchParams] = useSearchParams()

  // Parse sort from URL
  const sort = useMemo<SortState>(
    () => parseSortParam(searchParams.get('sort'), WEBHOOKS_SORT_COLUMNS),
    [searchParams]
  )

  // Set sort (URL-synced) and reset page (local state) atomically
  const setSort = useCallback(
    (next: SortState) => {
      setSearchParams(
        (prev) => {
          const updated = new URLSearchParams(prev)
          const serialized = serializeSortParam(next)
          if (serialized) {
            updated.set('sort', serialized)
          } else {
            updated.delete('sort')
          }
          return updated
        },
        { replace: true }
      )
      setFilters((prev) => ({ ...prev, page: 1 }))
    },
    [setSearchParams]
  )

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

  const hasActiveFilters = [
    filters.eventType,
    filters.processed,
    filters.startDate,
    filters.endDate,
  ].some((f) => f !== '')

  // Format params for API call
  const apiParams = useMemo<ListAdminPaddleWebhooksParams>(() => {
    const params: ListAdminPaddleWebhooksParams = {
      limit: PAGE_SIZE,
      offset: (filters.page - 1) * PAGE_SIZE,
    }

    if (filters.eventType) {
      params.event_type = filters.eventType
    }

    if (filters.processed !== '') {
      params.processed = filters.processed
    }

    if (filters.startDate) {
      params.start_date = filters.startDate
    }

    if (filters.endDate) {
      params.end_date = filters.endDate
    }

    if (sort) {
      params.sort_by = sort.columnId as ListAdminPaddleWebhooksSortBy
      params.sort_dir = sort.direction as ListAdminPaddleWebhooksSortDir
    }

    return params
  }, [filters, sort])

  return {
    filters,
    setEventType,
    setProcessed,
    setStartDate,
    setEndDate,
    setPage,
    hasActiveFilters,
    sort,
    setSort,
    apiParams,
  }
}
