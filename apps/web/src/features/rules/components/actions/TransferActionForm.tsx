/**
 * TransferActionForm Component
 * Issue #39: Rule Builder UI
 *
 * Form for configuring account transfer actions.
 * Creates transfer suggestions between accounts.
 */

import { useCallback } from 'react'
import { useGetAccounts } from '@boostedbudget/api-client/endpoints/accounts/accounts'
import type { TransferAction } from '../../types'

export interface TransferActionFormProps {
  /** Current action configuration */
  action: TransferAction
  /** Called when action changes */
  onChange: (action: TransferAction) => void
  /** Disable all controls */
  disabled?: boolean
  /** Additional CSS class */
  className?: string
}

type AmountType = 'fixed' | 'percentage' | 'excess'

const AMOUNT_TYPES: { value: AmountType; label: string; description: string }[] = [
  {
    value: 'fixed',
    label: 'Fixed',
    description: 'Transfer a specific dollar amount',
  },
  {
    value: 'percentage',
    label: 'Percentage',
    description: 'Transfer a percentage of the balance',
  },
  {
    value: 'excess',
    label: 'Excess',
    description: 'Transfer any amount above the threshold',
  },
]

/**
 * TransferActionForm component for configuring account transfer actions.
 */
export function TransferActionForm({
  action,
  onChange,
  disabled = false,
  className = '',
}: TransferActionFormProps) {
  const { data: accounts, isLoading } = useGetAccounts()

  // Current amount type description
  const currentDescription = AMOUNT_TYPES.find(
    (t) => t.value === action.amount_type
  )?.description

  // Handlers
  const handleFromAccountChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      onChange({
        ...action,
        from_account_id: e.target.value,
      })
    },
    [action, onChange]
  )

  const handleToAccountChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      onChange({
        ...action,
        to_account_id: e.target.value,
      })
    },
    [action, onChange]
  )

  const handleAmountTypeChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      onChange({
        ...action,
        amount_type: e.target.value as AmountType,
      })
    },
    [action, onChange]
  )

  const handleAmountChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const value = parseFloat(e.target.value) || 0
      onChange({
        ...action,
        amount: value,
      })
    },
    [action, onChange]
  )

  const handleThresholdChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const value = parseFloat(e.target.value) || 0
      onChange({
        ...action,
        threshold: value,
      })
    },
    [action, onChange]
  )

  const accountsList = accounts?.accounts ?? []

  return (
    <div className={`flex flex-col gap-5 ${className}`.trim()}>
      {/* Account Selection */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div className="flex flex-col gap-1.5">
          <label htmlFor="from-account" className="text-sm font-medium text-foreground">
            From Account
          </label>
          <select
            id="from-account"
            value={action.from_account_id}
            onChange={handleFromAccountChange}
            disabled={disabled || isLoading}
            className="cursor-pointer rounded-lg border border-border bg-card px-3.5 py-2.5 text-sm text-foreground focus:border-blue-500 focus:outline-none focus:ring-[3px] focus:ring-blue-500/10 disabled:cursor-not-allowed disabled:opacity-50"
          >
            <option value="">Select an account...</option>
            {accountsList.map((account: { id: string; name: string; institution_name?: string | null }) => (
              <option key={account.id} value={account.id}>
                {account.name}
                {account.institution_name && ` (${account.institution_name})`}
              </option>
            ))}
          </select>
        </div>

        <div className="flex flex-col gap-1.5">
          <label htmlFor="to-account" className="text-sm font-medium text-foreground">
            To Account
          </label>
          <select
            id="to-account"
            value={action.to_account_id}
            onChange={handleToAccountChange}
            disabled={disabled || isLoading}
            className="cursor-pointer rounded-lg border border-border bg-card px-3.5 py-2.5 text-sm text-foreground focus:border-blue-500 focus:outline-none focus:ring-[3px] focus:ring-blue-500/10 disabled:cursor-not-allowed disabled:opacity-50"
          >
            <option value="">Select an account...</option>
            {accountsList.map((account: { id: string; name: string; institution_name?: string | null }) => (
              <option key={account.id} value={account.id}>
                {account.name}
                {account.institution_name && ` (${account.institution_name})`}
              </option>
            ))}
          </select>
        </div>
      </div>

      {/* Amount Type */}
      <div className="flex flex-col gap-1.5">
        <label htmlFor="amount-type" className="text-sm font-medium text-foreground">
          Amount Type
        </label>
        <select
          id="amount-type"
          value={action.amount_type}
          onChange={handleAmountTypeChange}
          disabled={disabled}
          className="cursor-pointer rounded-lg border border-border bg-card px-3.5 py-2.5 text-sm text-foreground focus:border-blue-500 focus:outline-none focus:ring-[3px] focus:ring-blue-500/10 disabled:cursor-not-allowed disabled:opacity-50"
        >
          {AMOUNT_TYPES.map((type) => (
            <option key={type.value} value={type.value}>
              {type.label}
            </option>
          ))}
        </select>
        {currentDescription && (
          <p className="mt-1 text-xs text-muted-foreground">{currentDescription}</p>
        )}
      </div>

      {/* Amount Input (for fixed and percentage) */}
      {(action.amount_type === 'fixed' || action.amount_type === 'percentage') && (
        <div className="flex flex-col gap-1.5">
          <label htmlFor="amount" className="text-sm font-medium text-foreground">
            Amount
          </label>
          <div className="flex items-center overflow-hidden rounded-lg border border-border bg-card focus-within:border-blue-500 focus-within:ring-[3px] focus-within:ring-blue-500/10">
            {action.amount_type === 'fixed' && (
              <span className="bg-muted px-3 py-2.5 text-sm font-medium text-muted-foreground">$</span>
            )}
            <input
              id="amount"
              type="number"
              value={action.amount}
              onChange={handleAmountChange}
              disabled={disabled}
              min={0}
              max={action.amount_type === 'percentage' ? 100 : undefined}
              className="min-w-0 flex-1 border-none bg-transparent px-3.5 py-2.5 text-sm focus:outline-none focus:ring-0 disabled:cursor-not-allowed disabled:opacity-50"
            />
            {action.amount_type === 'percentage' && (
              <span className="bg-muted px-3 py-2.5 text-sm font-medium text-muted-foreground">%</span>
            )}
          </div>
        </div>
      )}

      {/* Threshold Input (for excess type) */}
      {action.amount_type === 'excess' && (
        <div className="flex flex-col gap-1.5">
          <label htmlFor="threshold" className="text-sm font-medium text-foreground">
            Threshold
          </label>
          <div className="flex items-center overflow-hidden rounded-lg border border-border bg-card focus-within:border-blue-500 focus-within:ring-[3px] focus-within:ring-blue-500/10">
            <span className="bg-muted px-3 py-2.5 text-sm font-medium text-muted-foreground">$</span>
            <input
              id="threshold"
              type="number"
              value={action.threshold ?? 0}
              onChange={handleThresholdChange}
              disabled={disabled}
              min={0}
              className="min-w-0 flex-1 border-none bg-transparent px-3.5 py-2.5 text-sm focus:outline-none focus:ring-0 disabled:cursor-not-allowed disabled:opacity-50"
            />
          </div>
          <p className="mt-1 text-xs text-muted-foreground">
            Transfer any amount above the threshold to the target account
          </p>
        </div>
      )}
    </div>
  )
}
