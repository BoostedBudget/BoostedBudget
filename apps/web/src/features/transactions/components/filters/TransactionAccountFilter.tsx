/**
 * Transaction Account Filter Component
 * Multi-select dropdown for filtering by accounts
 * Issue #529: Transaction page filtering
 */

import { useState, useRef, useEffect, useCallback } from 'react'
import { ChevronDownIcon, CheckIcon } from '@heroicons/react/24/outline'

interface Account {
  id: string
  name: string
  official_name?: string | null
  institution_name?: string | null
  type: string
}

interface TransactionAccountFilterProps {
  accounts: Account[]
  selectedIds: string[]
  onChange: (ids: string[]) => void
  loading?: boolean
  disabled?: boolean
}

export function TransactionAccountFilter({
  accounts,
  selectedIds,
  onChange,
  loading,
  disabled,
}: TransactionAccountFilterProps) {
  const [isOpen, setIsOpen] = useState(false)
  const dropdownRef = useRef<HTMLDivElement>(null)

  // Close dropdown when clicking outside
  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setIsOpen(false)
      }
    }

    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [])

  const handleToggle = useCallback(
    (accountId: string) => {
      if (selectedIds.includes(accountId)) {
        onChange(selectedIds.filter((id) => id !== accountId))
      } else {
        onChange([...selectedIds, accountId])
      }
    },
    [selectedIds, onChange]
  )

  const handleSelectAll = useCallback(() => {
    onChange(accounts.map((a) => a.id))
  }, [accounts, onChange])

  const handleClearAll = useCallback(() => {
    onChange([])
  }, [onChange])

  const getDisplayText = () => {
    if (selectedIds.length === 0) return 'All accounts'
    if (selectedIds.length === 1) {
      const account = accounts.find((a) => a.id === selectedIds[0])
      return account?.name || 'Selected account'
    }
    return `${selectedIds.length} accounts`
  }

  return (
    <div className="flex flex-col gap-1 relative" ref={dropdownRef}>
      <label className="text-sm font-medium text-gray-700">Accounts</label>

      {/* Dropdown trigger */}
      <button
        type="button"
        onClick={() => setIsOpen(!isOpen)}
        disabled={disabled || loading}
        className={`
          flex items-center justify-between w-full px-3 py-2 text-sm
          border border-gray-300 rounded-lg bg-white
          focus:ring-2 focus:ring-blue-500 focus:border-blue-500
          ${disabled || loading ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer hover:bg-gray-50'}
        `}
        aria-expanded={isOpen}
        aria-haspopup="listbox"
      >
        <span className={selectedIds.length === 0 ? 'text-gray-500' : 'text-gray-900'}>
          {loading ? 'Loading accounts...' : getDisplayText()}
        </span>
        <ChevronDownIcon
          className={`w-4 h-4 text-gray-400 transition-transform ${isOpen ? 'rotate-180' : ''}`}
        />
      </button>

      {/* Dropdown menu */}
      {isOpen && !loading && (
        <div className="absolute left-0 z-10 mt-1 min-w-full max-w-xs max-h-60 overflow-auto bg-white border border-gray-200 rounded-lg shadow-lg">
          {/* Select All / Clear All */}
          <div className="flex justify-between p-2 border-b border-gray-100">
            <button
              type="button"
              onClick={handleSelectAll}
              className="text-xs text-blue-600 hover:text-blue-800"
            >
              Select All
            </button>
            <button
              type="button"
              onClick={handleClearAll}
              className="text-xs text-gray-500 hover:text-gray-700"
            >
              Clear All
            </button>
          </div>

          {/* Account list */}
          {accounts.length === 0 ? (
            <div className="p-3 text-sm text-gray-500 text-center">No accounts available</div>
          ) : (
            <ul role="listbox" className="py-1">
              {accounts.map((account) => {
                const isSelected = selectedIds.includes(account.id)
                return (
                  <li
                    key={account.id}
                    role="option"
                    aria-selected={isSelected}
                    onClick={() => handleToggle(account.id)}
                    className={`
                      flex items-center gap-2 px-3 py-2 cursor-pointer
                      ${isSelected ? 'bg-blue-50' : 'hover:bg-gray-50'}
                    `}
                  >
                    <div
                      className={`
                        w-4 h-4 flex items-center justify-center rounded border
                        ${isSelected ? 'bg-blue-600 border-blue-600' : 'border-gray-300'}
                      `}
                    >
                      {isSelected && <CheckIcon className="w-3 h-3 text-white" />}
                    </div>
                    <div className="flex-1 min-w-0">
                      <div className="text-sm font-medium text-gray-900 truncate">
                        {account.name}
                      </div>
                      {account.institution_name && (
                        <div className="text-xs text-gray-500 truncate">
                          {account.institution_name}
                        </div>
                      )}
                    </div>
                  </li>
                )
              })}
            </ul>
          )}
        </div>
      )}
    </div>
  )
}
