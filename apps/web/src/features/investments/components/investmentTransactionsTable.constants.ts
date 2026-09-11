/**
 * Shared constants and pure helpers for InvestmentTransactionsTable.
 * Issue #1296 — extracted to a .ts module so react-refresh/only-export-components
 * does not flag non-component exports (lint rule only applies to .tsx files).
 */

import type { InvestmentTransaction } from '@boostedbudget/api-client/models/investmentTransaction'

/**
 * #1631 C1: single source of truth for the classified activity type. The Type column's SORT
 * accessor and its chip RENDER used to write this fallback separately -- `render` fell back to
 * the raw `type` for a row missing `activity_type` (omitempty), but `key: 'activity_type'` did
 * not, so that same row displayed its classified chip while sorting under `undefined`. Both call
 * sites now call this one function, so they cannot diverge again.
 */
export function activityTypeSortValue(
  r: Pick<InvestmentTransaction, 'activity_type' | 'type'>,
): string {
  return r.activity_type ?? r.type
}

/** Column IDs in display order — version key for useTablePreferences. */
export const TXN_COLUMN_IDS = [
  'date',
  'type',
  'security',
  'quantity',
  'price',
  'amount',
  'fees',
] as const

export type TxnColumnId = (typeof TXN_COLUMN_IDS)[number]

/**
 * Column IDs whose cells must be wrapped in data-ph-mask.
 * DataTable applies masking automatically when a column id appears here.
 * IMPORTANT: keep in sync with the columns definition in InvestmentTransactionsTable.tsx.
 */
export const MASKED_COLUMN_IDS: string[] = ['security', 'quantity', 'price', 'amount', 'fees']

/**
 * Builds the DataTable virtualization config from the infinite-scroll hook values.
 * Extracted so unit tests can assert onScrollEnd === fetchNextPage without mocking
 * the shared DataTable component.
 */
export function buildInvestmentVirtualization({
  fetchNextPage,
  hasNextPage,
  isFetchingNextPage,
}: {
  fetchNextPage: () => void
  hasNextPage: boolean
  isFetchingNextPage: boolean
}) {
  return {
    enabled: true,
    estimateRowHeight: 56,
    overscan: 8,
    dynamicRowHeight: true,
    scrollContainerHeight: 'calc(100dvh - 240px)',
    onScrollEnd: fetchNextPage,
    hasMore: hasNextPage,
    isFetchingMore: isFetchingNextPage,
    prefetchRows: 5,
  }
}
