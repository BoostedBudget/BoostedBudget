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
import './TransferActionForm.css'

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
    <div className={`transfer-form ${className}`.trim()}>
      {/* Account Selection */}
      <div className="transfer-form__row">
        <div className="transfer-form__field">
          <label htmlFor="from-account" className="transfer-form__label">
            From Account
          </label>
          <select
            id="from-account"
            value={action.from_account_id}
            onChange={handleFromAccountChange}
            disabled={disabled || isLoading}
            className="transfer-form__select"
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

        <div className="transfer-form__field">
          <label htmlFor="to-account" className="transfer-form__label">
            To Account
          </label>
          <select
            id="to-account"
            value={action.to_account_id}
            onChange={handleToAccountChange}
            disabled={disabled || isLoading}
            className="transfer-form__select"
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
      <div className="transfer-form__field">
        <label htmlFor="amount-type" className="transfer-form__label">
          Amount Type
        </label>
        <select
          id="amount-type"
          value={action.amount_type}
          onChange={handleAmountTypeChange}
          disabled={disabled}
          className="transfer-form__select"
        >
          {AMOUNT_TYPES.map((type) => (
            <option key={type.value} value={type.value}>
              {type.label}
            </option>
          ))}
        </select>
        {currentDescription && (
          <p className="transfer-form__description">{currentDescription}</p>
        )}
      </div>

      {/* Amount Input (for fixed and percentage) */}
      {(action.amount_type === 'fixed' || action.amount_type === 'percentage') && (
        <div className="transfer-form__field">
          <label htmlFor="amount" className="transfer-form__label">
            Amount
          </label>
          <div className="transfer-form__input-group">
            {action.amount_type === 'fixed' && (
              <span className="transfer-form__prefix">$</span>
            )}
            <input
              id="amount"
              type="number"
              value={action.amount}
              onChange={handleAmountChange}
              disabled={disabled}
              min={0}
              max={action.amount_type === 'percentage' ? 100 : undefined}
              className="transfer-form__input"
            />
            {action.amount_type === 'percentage' && (
              <span className="transfer-form__suffix">%</span>
            )}
          </div>
        </div>
      )}

      {/* Threshold Input (for excess type) */}
      {action.amount_type === 'excess' && (
        <div className="transfer-form__field">
          <label htmlFor="threshold" className="transfer-form__label">
            Threshold
          </label>
          <div className="transfer-form__input-group">
            <span className="transfer-form__prefix">$</span>
            <input
              id="threshold"
              type="number"
              value={action.threshold ?? 0}
              onChange={handleThresholdChange}
              disabled={disabled}
              min={0}
              className="transfer-form__input"
            />
          </div>
          <p className="transfer-form__description">
            Transfer any amount above the threshold to the target account
          </p>
        </div>
      )}
    </div>
  )
}
