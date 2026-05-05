export type NetWorthSide = 'assets' | 'liabilities'

export interface AccountBreakdownRow {
  id: string
  name: string
  type: string
  currencyCode: string
  currentBalance: number
  percentOfNetWorth: number
  side: NetWorthSide
  mask?: string
}
