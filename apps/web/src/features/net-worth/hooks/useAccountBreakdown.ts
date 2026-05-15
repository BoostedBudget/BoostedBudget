import { useMemo } from 'react'
import { useGetAccounts } from '@boostedbudget/api-client/endpoints/accounts/accounts'
import { filterActiveAccounts } from '@/features/accounts/hooks'
import type { AccountBreakdownRow, NetWorthSide } from '../types'

const LIABILITY_TYPES = new Set(['credit', 'loan'])

function classifyAccount(type: string): NetWorthSide {
  return LIABILITY_TYPES.has(type) ? 'liabilities' : 'assets'
}

const assetContrib = (balance: number) => Math.max(0, balance)
const liabContrib = (balance: number) => Math.max(0, Math.abs(balance))

export interface UseAccountBreakdownResult {
  assets: AccountBreakdownRow[]
  liabilities: AccountBreakdownRow[]
  assetsTotal: number
  liabilitiesTotal: number
  loading: boolean
  error: Error | null
}

export function useAccountBreakdown(): UseAccountBreakdownResult {
  const { data, isLoading: loading, error } = useGetAccounts()

  const result = useMemo(() => {
    // Issue #1109: exclude replaced accounts so breakdown matches GetNetWorth
    const accounts = filterActiveAccounts(data?.accounts ?? [])
    const rawAssets: Array<{ account: (typeof accounts)[number]; balance: number }> = []
    const rawLiabs: Array<{ account: (typeof accounts)[number]; balance: number }> = []
    let assetsTotal = 0
    let liabilitiesTotal = 0

    for (const account of accounts) {
      if (account.is_hidden) continue
      const balance = account.current_balance ?? 0
      const side = classifyAccount(account.type)
      if (side === 'assets') {
        rawAssets.push({ account, balance })
        assetsTotal += assetContrib(balance)
      } else {
        rawLiabs.push({ account, balance })
        liabilitiesTotal += liabContrib(balance)
      }
    }

    const toRow = (
      { account, balance }: { account: (typeof accounts)[number]; balance: number },
      side: 'assets' | 'liabilities'
    ): AccountBreakdownRow => ({
      id: account.id,
      name: account.nickname?.trim() || account.name,
      type: account.type,
      institutionName: account.institution_name ?? 'Unknown',
      currencyCode: account.currency_code,
      currentBalance: balance,
      percentOfSide:
        side === 'assets'
          ? assetsTotal === 0
            ? 0
            : (assetContrib(balance) / assetsTotal) * 100
          : liabilitiesTotal === 0
            ? 0
            : (liabContrib(balance) / liabilitiesTotal) * 100,
      side,
    })

    // #1109: toRow helper ensures consistent mapping
    const assets = rawAssets.map((item) => toRow(item, 'assets'))
    const liabilities = rawLiabs.map((item) => toRow(item, 'liabilities'))

    return { assets, liabilities, assetsTotal, liabilitiesTotal }
  }, [data])

  return {
    ...result,
    loading,
    error: error ?? null,
  }
}
