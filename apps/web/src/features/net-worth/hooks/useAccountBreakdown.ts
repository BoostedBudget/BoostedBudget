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
    const rawAssets: Array<{ account: (typeof accounts)[number]; balance: number | null }> = []
    const rawLiabs: Array<{ account: (typeof accounts)[number]; balance: number | null }> = []
    let assetsTotal = 0
    let liabilitiesTotal = 0

    for (const account of accounts) {
      if (account.is_hidden) continue
      // Issue #1569 whole-branch review, I1: the row keeps "no balance" as `null`. `?? 0` lives
      // ONLY in the accumulators below, where treating an unknown amount as contributing nothing
      // is the only available answer — it must not leak onto the row, where it would render as
      // "$0 at 0.0%" and disagree with the Accounts page's em dash for the same account.
      const balance = account.current_balance ?? null
      const side = classifyAccount(account.type)
      if (side === 'assets') {
        rawAssets.push({ account, balance })
        assetsTotal += assetContrib(balance ?? 0)
      } else {
        rawLiabs.push({ account, balance })
        liabilitiesTotal += liabContrib(balance ?? 0)
      }
    }

    const toRow = (
      { account, balance }: { account: (typeof accounts)[number]; balance: number | null },
      side: 'assets' | 'liabilities'
    ): AccountBreakdownRow => ({
      id: account.id,
      name: account.nickname?.trim() || account.name,
      type: account.type,
      institutionName: account.institution_name ?? 'Unknown',
      currencyCode: account.currency_code,
      currentBalance: balance,
      percentOfSide:
        balance === null
          ? null
          : side === 'assets'
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
