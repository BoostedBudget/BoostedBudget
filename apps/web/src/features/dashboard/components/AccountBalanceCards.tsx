import { CreditCard } from 'lucide-react'
import { EmptyState } from '@/components/ui/empty-state'
import type { BalanceSummary } from '../types'

interface AccountBalanceCardsProps {
  summary: BalanceSummary
}

/**
 * AccountBalanceCards component displays individual account balances
 * grouped by account type
 *
 * Features:
 * - Cards for each account type
 * - List of accounts with balances
 * - Color-coded by account type
 * - Total per type
 *
 * Issue #353: Balance Tracking Dashboard
 */
export function AccountBalanceCards({ summary }: AccountBalanceCardsProps) {
  const { by_type } = summary

  const formatCurrency = (value: number) => {
    return new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency: 'USD',
    }).format(value)
  }

  // Color scheme for account types
  const typeColors: Record<string, { bg: string; border: string; text: string }> = {
    depository: {
      bg: 'bg-success/10',
      border: 'border-success/30',
      text: 'text-success',
    },
    credit: {
      bg: 'bg-destructive/10',
      border: 'border-destructive/30',
      text: 'text-destructive',
    },
    investment: {
      bg: 'bg-muted/30',
      border: 'border-border',
      text: 'text-foreground',
    },
    loan: {
      bg: 'bg-warning/10',
      border: 'border-warning/30',
      text: 'text-warning',
    },
    other: {
      bg: 'bg-muted',
      border: 'border-border',
      text: 'text-foreground',
    },
  }

  // Type labels
  const typeLabels: Record<string, string> = {
    depository: 'Checking & Savings',
    credit: 'Credit Cards',
    investment: 'Investments',
    loan: 'Loans',
    other: 'Other',
  }

  const accountTypes = Object.keys(by_type).sort()

  if (accountTypes.length === 0) {
    return (
      <EmptyState
        size="compact"
        icon={<CreditCard />}
        title="No accounts found"
      />
    )
  }

  return (
    <div className="space-y-4">
      {accountTypes.map((typeKey) => {
        const typeData = by_type[typeKey]
        const colors = typeColors[typeKey] || typeColors.other
        const label = typeLabels[typeKey] || typeKey

        return (
          <div
            key={typeKey}
            className={`rounded-lg border-2 ${colors.border} ${colors.bg} p-5`}
          >
            {/* Type header */}
            <div className="flex items-center justify-between mb-4">
              <h3 className={`text-lg font-semibold ${colors.text}`}>
                {label}
              </h3>
              <div className="text-right">
                <p className={`text-xl font-bold ${colors.text}`}>
                  {formatCurrency(typeData.balance)}
                </p>
                <p className="text-sm text-muted-foreground">
                  {typeData.account_count} account{typeData.account_count !== 1 ? 's' : ''}
                </p>
              </div>
            </div>

            {/* Individual accounts */}
            <div className="space-y-2">
              {typeData.accounts.map((account) => (
                <div
                  key={account.id}
                  className="flex items-center justify-between bg-card rounded-lg px-4 py-3 shadow-sm"
                >
                  <span className="text-sm font-medium text-foreground">
                    {account.name}
                  </span>
                  <span className="text-sm font-semibold text-foreground">
                    {formatCurrency(account.balance)}
                  </span>
                </div>
              ))}
            </div>
          </div>
        )
      })}
    </div>
  )
}
