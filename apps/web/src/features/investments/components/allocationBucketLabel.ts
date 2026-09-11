/**
 * Shared pure helper for AllocationDonutChart.
 * Extracted to a .ts module (matching investmentTransactionsTable.constants.ts) so
 * react-refresh/only-export-components does not flag a non-component export from a .tsx file,
 * and so #1631 C2's Holdings/donut label-agreement test can import it directly.
 */

import type { GetPortfolioAllocationBreakdownGroupBy } from '../hooks/usePortfolioAllocationConfig'
import { securityTypeLabel } from '@/features/transactions/investmentSubcategory'

// Map raw API security-type tokens to display-ready labels.
// Rules (in priority order):
//   1. Falsy / "unknown" → "Other"
//   2. group_by=account → verbatim (account names are already human-readable)
//   3. Known acronyms → uppercase canonical form (etf → "ETF", reit → "REIT")
//   4. Everything else → title-case by splitting on underscores/spaces
//      ("mutual_fund" → "Mutual Fund", "stock" → "Stock")
// Rules 1/3/4 are the shared `securityTypeLabel` helper (#1631) — reused here so this chart and
// HoldingsTable's Type column render the SAME label for the SAME `securities.type` value.
export function humanizeBucketLabel(
  value: string,
  groupBy: GetPortfolioAllocationBreakdownGroupBy,
): string {
  if (!value || value.toLowerCase() === 'unknown') return 'Other'
  if (groupBy === 'account') return value
  return securityTypeLabel(value, 'Other')
}
