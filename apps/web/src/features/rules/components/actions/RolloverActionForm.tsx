/**
 * RolloverActionForm Component
 * Issue #39: Rule Builder UI
 *
 * Form for configuring category rollover actions.
 * Moves unspent envelope funds to another category.
 */

import { useCallback, useMemo } from 'react'
import { useBudgets } from '../../../budgets/hooks/useBudgets'
import type { RolloverAction, RolloverType } from '../../types'
import { formatBudgetCategoryLabel } from '../../../budgets/types'

export interface RolloverActionFormProps {
  /** Current action configuration */
  action: RolloverAction
  /** Called when action changes */
  onChange: (action: RolloverAction) => void
  /** Disable all controls */
  disabled?: boolean
  /** Additional CSS class */
  className?: string
}

const ROLLOVER_TYPES: { value: RolloverType; label: string; description: string }[] = [
  {
    value: 'full',
    label: 'Full',
    description: 'Transfer all remaining funds to the target category',
  },
  {
    value: 'capped',
    label: 'Capped',
    description: 'Transfer up to a maximum amount to the target category',
  },
  {
    value: 'percentage',
    label: 'Percentage',
    description: 'Transfer a percentage of remaining funds to the target category',
  },
]

/**
 * RolloverActionForm component for configuring category rollover actions.
 */
export function RolloverActionForm({
  action,
  onChange,
  disabled = false,
  className = '',
}: RolloverActionFormProps) {
  const { budgets, loading } = useBudgets()

  // Find which budget contains the source category
  const sourceBudget = useMemo(() => {
    if (!action.source_budget_category_id) return null
    return (
      budgets.find((b) =>
        b.categories?.some((c) => c.id === action.source_budget_category_id)
      ) ?? null
    )
  }, [budgets, action.source_budget_category_id])

  // Find which budget contains the target category
  const targetBudget = useMemo(() => {
    if (!action.target_budget_category_id) return null
    return (
      budgets.find((b) =>
        b.categories?.some((c) => c.id === action.target_budget_category_id)
      ) ?? null
    )
  }, [budgets, action.target_budget_category_id])

  // Current rollover type description
  const currentDescription = ROLLOVER_TYPES.find(
    (t) => t.value === action.rollover_type
  )?.description

  // Handlers
  const handleSourceBudgetChange = useCallback(
    (_e: React.ChangeEvent<HTMLSelectElement>) => {
      onChange({
        ...action,
        source_budget_category_id: '', // Clear source category when budget changes
      })
    },
    [action, onChange]
  )

  const handleSourceCategoryChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      onChange({
        ...action,
        source_budget_category_id: e.target.value,
      })
    },
    [action, onChange]
  )

  const handleTargetBudgetChange = useCallback(
    (_e: React.ChangeEvent<HTMLSelectElement>) => {
      onChange({
        ...action,
        target_budget_category_id: null, // Clear target category when budget changes
      })
    },
    [action, onChange]
  )

  const handleTargetCategoryChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      const value = e.target.value
      onChange({
        ...action,
        target_budget_category_id: value === '' ? null : value,
      })
    },
    [action, onChange]
  )

  const handleRolloverTypeChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      onChange({
        ...action,
        rollover_type: e.target.value as RolloverType,
      })
    },
    [action, onChange]
  )

  const handleCapAmountChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const value = parseFloat(e.target.value) || 0
      onChange({
        ...action,
        cap_amount: value,
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

  return (
    <div className={`flex flex-col gap-6 ${className}`.trim()}>
      {/* Source Section */}
      <div className="rounded-xl border border-border bg-muted p-4">
        <h4 className="mb-4 text-sm font-semibold uppercase tracking-wide text-foreground">Source</h4>

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div className="flex flex-col gap-1.5">
            <label htmlFor="source-budget" className="text-sm font-medium text-foreground">
              Source Budget
            </label>
            <select
              id="source-budget"
              value={sourceBudget?.id ?? ''}
              onChange={handleSourceBudgetChange}
              disabled={disabled || loading}
              className="cursor-pointer rounded-lg border border-border bg-card px-3.5 py-2.5 text-sm text-foreground focus:border-blue-500 focus:outline-none focus:ring-[3px] focus:ring-blue-500/10 disabled:cursor-not-allowed disabled:opacity-50"
            >
              <option value="">Select a budget...</option>
              {budgets.map((budget) => (
                <option key={budget.id} value={budget.id}>
                  {budget.name}
                </option>
              ))}
            </select>
          </div>

          <div className="flex flex-col gap-1.5">
            <label htmlFor="source-category" className="text-sm font-medium text-foreground">
              Source Category
            </label>
            <select
              id="source-category"
              value={action.source_budget_category_id}
              onChange={handleSourceCategoryChange}
              disabled={disabled || !sourceBudget}
              className="cursor-pointer rounded-lg border border-border bg-card px-3.5 py-2.5 text-sm text-foreground focus:border-blue-500 focus:outline-none focus:ring-[3px] focus:ring-blue-500/10 disabled:cursor-not-allowed disabled:opacity-50"
            >
              <option value="">Select a category...</option>
              {sourceBudget?.categories?.map((category) => (
                <option key={category.id} value={category.id}>
                  {formatBudgetCategoryLabel(category)}
                </option>
              ))}
            </select>
          </div>
        </div>
      </div>

      {/* Target Section */}
      <div className="rounded-xl border border-border bg-muted p-4">
        <h4 className="mb-4 text-sm font-semibold uppercase tracking-wide text-foreground">Target</h4>

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div className="flex flex-col gap-1.5">
            <label htmlFor="target-budget" className="text-sm font-medium text-foreground">
              Target Budget
            </label>
            <select
              id="target-budget"
              value={targetBudget?.id ?? ''}
              onChange={handleTargetBudgetChange}
              disabled={disabled || loading}
              className="cursor-pointer rounded-lg border border-border bg-card px-3.5 py-2.5 text-sm text-foreground focus:border-blue-500 focus:outline-none focus:ring-[3px] focus:ring-blue-500/10 disabled:cursor-not-allowed disabled:opacity-50"
            >
              <option value="">Select a budget...</option>
              {budgets.map((budget) => (
                <option key={budget.id} value={budget.id}>
                  {budget.name}
                </option>
              ))}
            </select>
          </div>

          <div className="flex flex-col gap-1.5">
            <label htmlFor="target-category" className="text-sm font-medium text-foreground">
              Target Category
            </label>
            <select
              id="target-category"
              value={action.target_budget_category_id ?? ''}
              onChange={handleTargetCategoryChange}
              disabled={disabled}
              className="cursor-pointer rounded-lg border border-border bg-card px-3.5 py-2.5 text-sm text-foreground focus:border-blue-500 focus:outline-none focus:ring-[3px] focus:ring-blue-500/10 disabled:cursor-not-allowed disabled:opacity-50"
            >
              <option value="">Unallocated Funds</option>
              {targetBudget?.categories?.map((category) => (
                <option key={category.id} value={category.id}>
                  {formatBudgetCategoryLabel(category)}
                </option>
              ))}
            </select>
          </div>
        </div>
      </div>

      {/* Rollover Type Section */}
      <div className="rounded-xl border border-border bg-muted p-4">
        <h4 className="mb-4 text-sm font-semibold uppercase tracking-wide text-foreground">Rollover Configuration</h4>

        <div className="flex flex-col gap-1.5">
          <label htmlFor="rollover-type" className="text-sm font-medium text-foreground">
            Rollover Type
          </label>
          <select
            id="rollover-type"
            value={action.rollover_type}
            onChange={handleRolloverTypeChange}
            disabled={disabled}
            className="cursor-pointer rounded-lg border border-border bg-card px-3.5 py-2.5 text-sm text-foreground focus:border-blue-500 focus:outline-none focus:ring-[3px] focus:ring-blue-500/10 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {ROLLOVER_TYPES.map((type) => (
              <option key={type.value} value={type.value}>
                {type.label}
              </option>
            ))}
          </select>
          {currentDescription && (
            <p className="mt-1 text-xs text-muted-foreground">{currentDescription}</p>
          )}
        </div>

        {/* Cap Amount (for capped type) */}
        {action.rollover_type === 'capped' && (
          <div className="mt-4 flex flex-col gap-1.5">
            <label htmlFor="cap-amount" className="text-sm font-medium text-foreground">
              Cap Amount
            </label>
            <div className="flex items-center overflow-hidden rounded-lg border border-border bg-card focus-within:border-blue-500 focus-within:ring-[3px] focus-within:ring-blue-500/10">
              <span className="bg-muted px-3 py-2.5 text-sm font-medium text-muted-foreground">$</span>
              <input
                id="cap-amount"
                type="number"
                value={action.cap_amount ?? 0}
                onChange={handleCapAmountChange}
                disabled={disabled}
                min={0}
                className="min-w-0 flex-1 border-none bg-transparent px-3.5 py-2.5 text-sm focus:outline-none focus:ring-0 disabled:cursor-not-allowed disabled:opacity-50"
              />
            </div>
          </div>
        )}

        {/* Percentage (for percentage type) */}
        {action.rollover_type === 'percentage' && (
          <div className="mt-4 flex flex-col gap-1.5">
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
      </div>
    </div>
  )
}
