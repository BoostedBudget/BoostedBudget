export type NetWorthSide = 'assets' | 'liabilities'

export interface AccountBreakdownRow {
  id: string
  name: string
  type: string
  institutionName: string
  currencyCode: string
  /**
   * ⚠ `null` means the app has NO current balance for this account, which is not the same as
   * zero (Issue #1569 whole-branch review, I1). Reachable for an ACTIVE account whose first sync
   * has not landed — `account_service.go` assigns `current_balance` only when Plaid returns a
   * non-nil `balances.current` — as well as for any terminal-status account, which
   * `accountBalanceProjection` renders as NULL. Renderers show an em dash; the side totals treat
   * it as contributing nothing.
   */
  currentBalance: number | null
  /** `null` whenever `currentBalance` is: a share of a total is not derivable from no figure. */
  percentOfSide: number | null
  side: NetWorthSide
}
