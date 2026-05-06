export type NetWorthSide = 'assets' | 'liabilities'

export interface AccountBreakdownRow {
  id: string
  name: string
  type: string
  institutionName: string
  currencyCode: string
  currentBalance: number
  percentOfSide: number
  side: NetWorthSide
}
