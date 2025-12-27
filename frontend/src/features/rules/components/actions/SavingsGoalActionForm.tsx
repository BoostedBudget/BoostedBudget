/**
 * SavingsGoalActionForm Component
 * Issue #39: Rule Builder UI
 *
 * Form for configuring savings goal contribution actions.
 * Creates transfer suggestions to contribute to savings goals.
 */

import { useCallback, useMemo } from 'react'
import { useGetAccounts } from '@/api/generated/endpoints/accounts/accounts'
import { useGetSavingsGoals } from '@/api/generated/endpoints/savings-goals/savings-goals'
import type { SavingsGoalAction } from '../../types'
import './SavingsGoalActionForm.css'

export interface SavingsGoalActionFormProps {
  /** Current action configuration */
  action: SavingsGoalAction
  /** Called when action changes */
  onChange: (action: SavingsGoalAction) => void
  /** Disable all controls */
  disabled?: boolean
  /** Additional CSS class */
  className?: string
}

type AmountType = 'fixed' | 'percentage' | 'remaining'

const AMOUNT_TYPES: { value: AmountType; label: string; description: string }[] = [
  {
    value: 'fixed',
    label: 'Fixed',
    description: 'Transfer a specific dollar amount to the savings goal',
  },
  {
    value: 'percentage',
    label: 'Percentage',
    description: 'Transfer a percentage of the balance to the savings goal',
  },
  {
    value: 'remaining',
    label: 'Remaining',
    description: 'Transfer all funds above the minimum balance to the savings goal',
  },
]

function formatCurrency(amount: number): string {
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
  }).format(amount)
}

/**
 * SavingsGoalActionForm component for configuring savings goal actions.
 */
export function SavingsGoalActionForm({
  action,
  onChange,
  disabled = false,
  className = '',
}: SavingsGoalActionFormProps) {
  const { data: accounts, isLoading: loadingAccounts } = useGetAccounts()
  const { data: savingsGoals, isLoading: loadingGoals } = useGetSavingsGoals()

  const accountsList = accounts?.accounts ?? []
  const goalsList = (savingsGoals ?? []) as {
    id: string
    name: string
    target_amount: number
    current_amount: number
  }[]

  // Get selected goal for progress display
  const selectedGoal = useMemo(() => {
    const goals = (savingsGoals ?? []) as { id: string; name: string; target_amount: number; current_amount: number }[]
    return goals.find((g) => g.id === action.savings_goal_id)
  }, [savingsGoals, action.savings_goal_id])

  // Current amount type description
  const currentDescription = AMOUNT_TYPES.find(
    (t) => t.value === action.amount_type
  )?.description

  // Handlers
  const handleGoalChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      onChange({
        ...action,
        savings_goal_id: e.target.value,
      })
    },
    [action, onChange]
  )

  const handleFromAccountChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      onChange({
        ...action,
        from_account_id: e.target.value,
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

  const handlePercentageChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const value = parseFloat(e.target.value) || 0
      onChange({
        ...action,
        percentage: value,
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

  const isLoading = loadingAccounts || loadingGoals

  return (
    <div className={`savings-goal-form ${className}`.trim()}>
      {/* Goal Selection */}
      <div className="savings-goal-form__field">
        <label htmlFor="savings-goal" className="savings-goal-form__label">
          Savings Goal
        </label>
        <select
          id="savings-goal"
          value={action.savings_goal_id}
          onChange={handleGoalChange}
          disabled={disabled || isLoading}
          className="savings-goal-form__select"
        >
          <option value="">Select a savings goal...</option>
          {goalsList.map((goal) => (
            <option key={goal.id} value={goal.id}>
              {goal.name}
            </option>
          ))}
        </select>
        {selectedGoal && (
          <p className="savings-goal-form__progress">
            {formatCurrency(selectedGoal.current_amount)} of{' '}
            {formatCurrency(selectedGoal.target_amount)} saved (
            {Math.round((selectedGoal.current_amount / selectedGoal.target_amount) * 100)}%)
          </p>
        )}
      </div>

      {/* From Account */}
      <div className="savings-goal-form__field">
        <label htmlFor="from-account" className="savings-goal-form__label">
          From Account
        </label>
        <select
          id="from-account"
          value={action.from_account_id}
          onChange={handleFromAccountChange}
          disabled={disabled || isLoading}
          className="savings-goal-form__select"
        >
          <option value="">Select an account...</option>
          {accountsList.map((account: { id: string; name: string }) => (
            <option key={account.id} value={account.id}>
              {account.name}
            </option>
          ))}
        </select>
      </div>

      {/* Amount Type */}
      <div className="savings-goal-form__field">
        <label htmlFor="amount-type" className="savings-goal-form__label">
          Amount Type
        </label>
        <select
          id="amount-type"
          value={action.amount_type}
          onChange={handleAmountTypeChange}
          disabled={disabled}
          className="savings-goal-form__select"
        >
          {AMOUNT_TYPES.map((type) => (
            <option key={type.value} value={type.value}>
              {type.label}
            </option>
          ))}
        </select>
        {currentDescription && (
          <p className="savings-goal-form__description">{currentDescription}</p>
        )}
      </div>

      {/* Amount Input (for fixed type) */}
      {action.amount_type === 'fixed' && (
        <div className="savings-goal-form__field">
          <label htmlFor="amount" className="savings-goal-form__label">
            Amount
          </label>
          <div className="savings-goal-form__input-group">
            <span className="savings-goal-form__prefix">$</span>
            <input
              id="amount"
              type="number"
              value={action.amount ?? 0}
              onChange={handleAmountChange}
              disabled={disabled}
              min={0}
              className="savings-goal-form__input"
            />
          </div>
        </div>
      )}

      {/* Percentage Input (for percentage type) */}
      {action.amount_type === 'percentage' && (
        <div className="savings-goal-form__field">
          <label htmlFor="percentage" className="savings-goal-form__label">
            Percentage
          </label>
          <div className="savings-goal-form__input-group">
            <input
              id="percentage"
              type="number"
              value={action.percentage ?? 0}
              onChange={handlePercentageChange}
              disabled={disabled}
              min={0}
              max={100}
              className="savings-goal-form__input"
            />
            <span className="savings-goal-form__suffix">%</span>
          </div>
        </div>
      )}

      {/* Threshold Input (for remaining type) */}
      {action.amount_type === 'remaining' && (
        <div className="savings-goal-form__field">
          <label htmlFor="minimum-balance" className="savings-goal-form__label">
            Minimum Balance
          </label>
          <div className="savings-goal-form__input-group">
            <span className="savings-goal-form__prefix">$</span>
            <input
              id="minimum-balance"
              type="number"
              value={action.threshold ?? 0}
              onChange={handleThresholdChange}
              disabled={disabled}
              min={0}
              className="savings-goal-form__input"
            />
          </div>
          <p className="savings-goal-form__description">
            Transfer all funds above this minimum balance to the savings goal
          </p>
        </div>
      )}
    </div>
  )
}
