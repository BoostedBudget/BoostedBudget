import { TrendingUp } from 'lucide-react'
import { EmptyState } from '@/components/ui/empty-state'
import type { AccountBreakdownRow } from '../types'

interface AccountBreakdownTableProps {
  assets: AccountBreakdownRow[]
  liabilities: AccountBreakdownRow[]
  netWorth: number
  loading: boolean
  error: Error | null
}

function formatCurrency(value: number, currencyCode = 'USD'): string {
  return Math.abs(value).toLocaleString('en-US', {
    style: 'currency',
    currency: currencyCode,
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  })
}

function AccountRows({ rows }: { rows: AccountBreakdownRow[] }) {
  return (
    <>
      {rows.map((row) => (
        <tr key={row.id}>
          <td className="py-2 pr-4 text-sm text-foreground">
            <span className="flex flex-col">
              <span>{row.name}</span>
              {row.mask && (
                <span data-ph-mask className="text-xs text-muted-foreground">
                  ••••{row.mask}
                </span>
              )}
            </span>
          </td>
          <td className="py-2 pr-4 text-sm text-muted-foreground capitalize">{row.type}</td>
          <td className="py-2 pr-4 text-sm text-right" data-ph-mask>
            {formatCurrency(row.currentBalance, row.currencyCode)}
          </td>
          <td
            className="py-2 text-sm text-right text-muted-foreground"
            aria-label={`${row.percentOfNetWorth.toFixed(1)}% of net worth`}
          >
            {row.percentOfNetWorth.toFixed(1)}%
          </td>
        </tr>
      ))}
    </>
  )
}

function AccountSection({ title, rows }: { title: string; rows: AccountBreakdownRow[] }) {
  if (rows.length === 0) return null
  return (
    <table className="w-full border-collapse">
      <caption className="text-left text-sm font-semibold text-foreground mb-2 caption-top">
        {title}
      </caption>
      <thead>
        <tr className="border-b border-border">
          <th scope="col" className="pb-2 text-left text-xs text-muted-foreground font-medium pr-4">Account</th>
          <th scope="col" className="pb-2 text-left text-xs text-muted-foreground font-medium pr-4">Type</th>
          <th scope="col" className="pb-2 text-right text-xs text-muted-foreground font-medium pr-4">Balance</th>
          <th scope="col" className="pb-2 text-right text-xs text-muted-foreground font-medium">% of Net Worth</th>
        </tr>
      </thead>
      <tbody>
        <AccountRows rows={rows} />
      </tbody>
    </table>
  )
}

export function AccountBreakdownTable({
  assets,
  liabilities,
  loading,
  error,
}: AccountBreakdownTableProps) {
  if (loading) {
    return (
      <div role="status" aria-label="Loading account breakdown" className="flex justify-center py-8">
        <div className="animate-spin rounded-full h-6 w-6 border-b-2 border-primary" />
      </div>
    )
  }

  if (error) {
    return <p className="text-sm text-destructive py-4">{error.message}</p>
  }

  if (assets.length === 0 && liabilities.length === 0) {
    return (
      <EmptyState
        icon={<TrendingUp />}
        title="No accounts found"
        description="Connect a bank account to see your breakdown."
      />
    )
  }

  return (
    <div className="overflow-x-auto space-y-6">
      <AccountSection title="Assets" rows={assets} />
      <AccountSection title="Liabilities" rows={liabilities} />
    </div>
  )
}
