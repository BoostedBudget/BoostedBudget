import type { NetWorthSummary } from '../types'

export function isNetWorthEmpty(summary: NetWorthSummary | null | undefined): boolean {
  if (!summary) return true
  return (
    summary.net_worth === 0 &&
    summary.total_assets === 0 &&
    summary.total_liabilities === 0 &&
    Object.keys(summary.by_category ?? {}).length === 0
  )
}
