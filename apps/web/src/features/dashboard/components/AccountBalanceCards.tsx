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
      bg: 'bg-green-50 dark:bg-green-950',
      border: 'border-green-200 dark:border-green-800',
      text: 'text-green-900 dark:text-green-100',
    },
    credit: {
      bg: 'bg-red-50 dark:bg-red-950',
      border: 'border-red-200 dark:border-red-800',
      text: 'text-red-900 dark:text-red-100',
    },
    investment: {
      bg: 'bg-purple-50 dark:bg-purple-950',
      border: 'border-purple-200 dark:border-purple-800',
      text: 'text-purple-900 dark:text-purple-100',
    },
    loan: {
      bg: 'bg-amber-50 dark:bg-amber-950',
      border: 'border-amber-200 dark:border-amber-800',
      text: 'text-amber-900 dark:text-amber-100',
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
      <div className="flex items-center justify-center h-full">
        <p className="text-center text-gray-500 dark:text-gray-400">No accounts found</p>
      </div>
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
                  className="flex items-center justify-between bg-white dark:bg-gray-800 rounded-lg px-4 py-3 shadow-sm"
                >
                  <span className="text-sm font-medium text-gray-900 dark:text-white">
                    {account.name}
                  </span>
                  <span className="text-sm font-semibold text-gray-700 dark:text-gray-300">
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
