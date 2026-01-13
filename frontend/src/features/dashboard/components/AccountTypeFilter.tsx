import type { AccountTypesResponse } from '../types'

interface AccountTypeFilterProps {
  accountTypes: AccountTypesResponse
  selectedTypes: string[]
  onChange: (types: string[]) => void
}

/**
 * AccountTypeFilter component for filtering dashboard by account type
 *
 * Features:
 * - Multi-select checkboxes for each account type
 * - Shows account count and total balance for each type
 * - "All" option to select/deselect all types
 *
 * Issue #353: Balance Tracking Dashboard
 */
export function AccountTypeFilter({
  accountTypes,
  selectedTypes,
  onChange,
}: AccountTypeFilterProps) {
  const allTypes = accountTypes.types.map((t) => t.key)
  const allSelected = selectedTypes.length === 0 || selectedTypes.length === allTypes.length

  const handleToggleAll = () => {
    if (allSelected) {
      onChange([])
    } else {
      onChange(allTypes)
    }
  }

  const handleToggleType = (typeKey: string) => {
    if (selectedTypes.includes(typeKey)) {
      onChange(selectedTypes.filter((t) => t !== typeKey))
    } else {
      onChange([...selectedTypes, typeKey])
    }
  }

  const formatCurrency = (value: number) => {
    return new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency: 'USD',
      minimumFractionDigits: 0,
      maximumFractionDigits: 0,
    }).format(value)
  }

  return (
    <div className="bg-white rounded-lg shadow p-6" data-testid="account-type-filter">
      <h3 className="text-lg font-semibold text-gray-900 mb-4">
        Account Types
      </h3>

      <div className="space-y-3">
        {/* All option */}
        <label className="flex items-center space-x-3 cursor-pointer">
          <input
            type="checkbox"
            checked={allSelected}
            onChange={handleToggleAll}
            className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500"
          />
          <span className="text-sm font-medium text-gray-900">All Types</span>
        </label>

        <div className="border-t border-gray-200 pt-3"></div>

        {/* Individual account types */}
        {accountTypes.types.map((type) => {
          const isSelected =
            selectedTypes.length === 0 || selectedTypes.includes(type.key)

          return (
            <label
              key={type.key}
              className="flex items-center justify-between cursor-pointer hover:bg-gray-50 -mx-2 px-2 py-1 rounded"
            >
              <div className="flex items-center space-x-3">
                <input
                  type="checkbox"
                  checked={isSelected}
                  onChange={() => handleToggleType(type.key)}
                  className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500"
                />
                <div>
                  <p className="text-sm font-medium text-gray-900">
                    {type.label}
                  </p>
                  <p className="text-xs text-gray-500">
                    {type.count} account{type.count !== 1 ? 's' : ''}
                  </p>
                </div>
              </div>
              <span className="text-sm font-semibold text-gray-700">
                {formatCurrency(type.total_balance)}
              </span>
            </label>
          )
        })}
      </div>
    </div>
  )
}
