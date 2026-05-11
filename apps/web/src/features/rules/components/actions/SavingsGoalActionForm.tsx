/**
 * SavingsGoalActionForm Component
 * Issue #39: Rule Builder UI
 *
 * Form for configuring savings goal contribution actions.
 * Creates transfer suggestions to contribute to savings goals.
 */

import { useCallback, useMemo } from 'react'
import { useActiveAccounts } from '@/features/accounts/hooks'
import { useGetSavingsGoals } from '@boostedbudget/api-client/endpoints/savings-goals/savings-goals'
import type { SavingsGoalAction } from '../../types'

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
  const { accounts: accountsList, isLoading: loadingAccounts } = useActiveAccounts()
  const { data: savingsGoals, isLoading: loadingGoals } = useGetSavingsGoals()

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
      const selectedId = e.target.value
      const account = (accountsList ?? []).find((a) => a.id === selectedId)
      onChange({
        ...action,
        from_account_id: selectedId,
        from_user_account_id: account?.user_account_id,
      })
    },
    [action, onChange, accountsList]
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
    <div className={`flex flex-col gap-5 ${className}`.trim()}>
      {/* Goal Selection */}
      <div className="flex flex-col gap-1.5">
        <label htmlFor="savings-goal" className="text-sm font-medium text-foreground">
          Savings Goal
        </label>
        <select
          id="savings-goal"
          value={action.savings_goal_id}
          onChange={handleGoalChange}
          disabled={disabled || isLoading}
          className="cursor-pointer rounded-lg border border-border bg-card px-3.5 py-2.5 text-sm text-foreground focus:border-blue-500 focus:outline-none focus:ring-[3px] focus:ring-blue-500/10 disabled:cursor-not-allowed disabled:opacity-50"
        >
          <option value="">Select a savings goal...</option>
          {goalsList.map((goal) => (
            <option key={goal.id} value={goal.id}>
              {goal.name}
            </option>
          ))}
        </select>
        {selectedGoal && (
          <p className="mt-1 text-xs font-medium text-green-600 dark:text-green-400">
            {formatCurrency(selectedGoal.current_amount)} of{' '}
            {formatCurrency(selectedGoal.target_amount)} saved (
            {Math.round((selectedGoal.current_amount / selectedGoal.target_amount) * 100)}%)
          </p>
        )}
      </div>

      {/* From Account */}
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
          {(accountsList ?? []).map((account: { id: string; name: string }) => (
            <option key={account.id} value={account.id}>
              {account.name}
            </option>
          ))}
        </select>
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

      {/* Amount Input (for fixed type) */}
      {action.amount_type === 'fixed' && (
        <div className="flex flex-col gap-1.5">
          <label htmlFor="amount" className="text-sm font-medium text-foreground">
            Amount
          </label>
          <div className="flex items-center overflow-hidden rounded-lg border border-border bg-card focus-within:border-blue-500 focus-within:ring-[3px] focus-within:ring-blue-500/10">
            <span className="bg-muted px-3 py-2.5 text-sm font-medium text-muted-foreground">$</span>
            <input
              id="amount"
              type="number"
              value={action.amount ?? 0}
              onChange={handleAmountChange}
              disabled={disabled}
              min={0}
              className="min-w-0 flex-1 border-none bg-transparent px-3.5 py-2.5 text-sm focus:outline-none focus:ring-0 disabled:cursor-not-allowed disabled:opacity-50"
            />
          </div>
        </div>
      )}

      {/* Percentage Input (for percentage type) */}
      {action.amount_type === 'percentage' && (
        <div className="flex flex-col gap-1.5">
          <label htmlFor="percentage" className="text-sm font-medium text-foreground">
            Percentage
          </label>
          <div className="flex items-center overflow-hidden rounded-lg border border-border bg-card focus-within:border-blue-500 focus-within:ring-[3px] focus-within:ring-blue-500/10">
            <input
              id="percentage"
              type="number"
              value={action.percentage ?? 0}
              onChange={handlePercentageChange}
              disabled={disabled}
              min={0}
              max={100}
              className="min-w-0 flex-1 border-none bg-transparent px-3.5 py-2.5 text-sm focus:outline-none focus:ring-0 disabled:cursor-not-allowed disabled:opacity-50"
            />
            <span className="bg-muted px-3 py-2.5 text-sm font-medium text-muted-foreground">%</span>
          </div>
        </div>
      )}

      {/* Threshold Input (for remaining type) */}
      {action.amount_type === 'remaining' && (
        <div className="flex flex-col gap-1.5">
          <label htmlFor="minimum-balance" className="text-sm font-medium text-foreground">
            Minimum Balance
          </label>
          <div className="flex items-center overflow-hidden rounded-lg border border-border bg-card focus-within:border-blue-500 focus-within:ring-[3px] focus-within:ring-blue-500/10">
            <span className="bg-muted px-3 py-2.5 text-sm font-medium text-muted-foreground">$</span>
            <input
              id="minimum-balance"
              type="number"
              value={action.threshold ?? 0}
              onChange={handleThresholdChange}
              disabled={disabled}
              min={0}
              className="min-w-0 flex-1 border-none bg-transparent px-3.5 py-2.5 text-sm focus:outline-none focus:ring-0 disabled:cursor-not-allowed disabled:opacity-50"
            />
          </div>
          <p className="mt-1 text-xs text-muted-foreground">
            Transfer all funds above this minimum balance to the savings goal
          </p>
        </div>
      )}
    </div>
  )
}
