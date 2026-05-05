import { useMemo } from 'react'
import { useGetAccounts } from '@boostedbudget/api-client/endpoints/accounts/accounts'
import type { AccountBreakdownRow, NetWorthSide } from '../types'

const LIABILITY_TYPES = new Set(['credit', 'loan'])

function classifyAccount(type: string): NetWorthSide {
  return LIABILITY_TYPES.has(type) ? 'liabilities' : 'assets'
}

export interface UseAccountBreakdownResult {
  assets: AccountBreakdownRow[]
  liabilities: AccountBreakdownRow[]
  loading: boolean
  error: Error | null
}

export function useAccountBreakdown(netWorth: number): UseAccountBreakdownResult {
  const { data, isLoading: loading, error } = useGetAccounts()

  const { assets, liabilities } = useMemo(() => {
    const accounts = data?.accounts ?? []
    const absNetWorth = Math.abs(netWorth)
    const result = { assets: [] as AccountBreakdownRow[], liabilities: [] as AccountBreakdownRow[] }

    for (const account of accounts) {
      if (account.is_hidden) continue

      const balance = account.current_balance ?? 0
      const percent = absNetWorth === 0 ? 0 : (Math.abs(balance) / absNetWorth) * 100
      const side = classifyAccount(account.type)

      const row: AccountBreakdownRow = {
        id: account.id,
        name: account.nickname ?? account.name,
        type: account.type,
        currencyCode: account.currency_code,
        currentBalance: balance,
        percentOfNetWorth: percent,
        side,
        mask: account.mask ?? undefined,
      }

      result[side].push(row)
    }

    return result
  }, [data, netWorth])

  return { assets, liabilities, loading, error: error ?? null }
}
