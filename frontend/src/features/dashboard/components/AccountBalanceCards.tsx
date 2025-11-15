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
      bg: 'bg-green-50',
      border: 'border-green-200',
      text: 'text-green-900',
    },
    credit: {
      bg: 'bg-red-50',
      border: 'border-red-200',
      text: 'text-red-900',
    },
    investment: {
      bg: 'bg-purple-50',
      border: 'border-purple-200',
      text: 'text-purple-900',
    },
    loan: {
      bg: 'bg-amber-50',
      border: 'border-amber-200',
      text: 'text-amber-900',
    },
    other: {
      bg: 'bg-gray-50',
      border: 'border-gray-200',
      text: 'text-gray-900',
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
      <div className="bg-white rounded-lg shadow p-6">
        <p className="text-center text-gray-500">No accounts found</p>
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
                <p className="text-sm text-gray-600">
                  {typeData.account_count} account{typeData.account_count !== 1 ? 's' : ''}
                </p>
              </div>
            </div>

            {/* Individual accounts */}
            <div className="space-y-2">
              {typeData.accounts.map((account) => (
                <div
                  key={account.id}
                  className="flex items-center justify-between bg-white rounded-lg px-4 py-3 shadow-sm"
                >
                  <span className="text-sm font-medium text-gray-900">
                    {account.name}
                  </span>
                  <span className="text-sm font-semibold text-gray-700">
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
