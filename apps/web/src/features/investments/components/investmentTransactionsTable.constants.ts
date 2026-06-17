/**
 * Shared constants and pure helpers for InvestmentTransactionsTable.
 * Issue #1296 — extracted to a .ts module so react-refresh/only-export-components
 * does not flag non-component exports (lint rule only applies to .tsx files).
 */

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
