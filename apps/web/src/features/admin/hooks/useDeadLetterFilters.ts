/**
 * useDeadLetterFilters Hook
 * Issue #633: Dead Letter Queue Admin UI
 * Issue #1148: Server-side sort for admin tables
 *
 * State management for DLQ list filters.
 * Uses plain useState for filter fields (not URL params) since DLQ filters
 * don't need bookmarking. Sort state is URL-synced via useSearchParams so
 * users can share/bookmark sorted views.
 */

import { useState, useCallback, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import type { ListAdminDeadLetterJobsParams, ListAdminDeadLetterJobsSortBy, ListAdminDeadLetterJobsSortDir } from '@boostedbudget/api-client/models'
import { parseSortParam, serializeSortParam } from '@/shared/hooks/useSortUrlState'
import type { SortState } from '@/shared/components/DataTable'

const PAGE_SIZE = 20

const DLQ_SORT_COLUMNS = new Set([
  'failed_at',
  'kind',
  'retried_at',
]) as ReadonlySet<string>

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
  /** Current sort state (null = unsorted) */
  sort: SortState
  /** Set sort state; passing null clears sort. Resets page to 1. */
  setSort: (next: SortState) => void
  /** Params formatted for API call */
  apiParams: ListAdminDeadLetterJobsParams
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
  const [searchParams, setSearchParams] = useSearchParams()

  // Parse sort from URL
  const sort = useMemo<SortState>(
    () => parseSortParam(searchParams.get('sort'), DLQ_SORT_COLUMNS),
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

  // Format params for API call
  const apiParams = useMemo<ListAdminDeadLetterJobsParams>(() => {
    const params: ListAdminDeadLetterJobsParams = {
      page: filters.page,
      page_size: PAGE_SIZE,
    }

    if (filters.kind) {
      params.kind = filters.kind
    }

    if (filters.userId) {
      params.user_id = filters.userId
    }

    if (filters.retried !== '') {
      params.retried = filters.retried
    }

    if (filters.startDate) {
      params.start_date = filters.startDate
    }

    if (filters.endDate) {
      params.end_date = filters.endDate
    }

    if (sort) {
      params.sort_by = sort.columnId as ListAdminDeadLetterJobsSortBy
      params.sort_dir = sort.direction as ListAdminDeadLetterJobsSortDir
    }

    return params
  }, [filters, sort])

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
    sort,
    setSort,
    apiParams,
  }
}
