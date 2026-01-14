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
import { formatCategoryPath } from '../../../budgets/types'
import './RolloverActionForm.css'

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
    <div className={`rollover-form ${className}`.trim()}>
      {/* Source Section */}
      <div className="rollover-form__section">
        <h4 className="rollover-form__section-title">Source</h4>

        <div className="rollover-form__row">
          <div className="rollover-form__field">
            <label htmlFor="source-budget" className="rollover-form__label">
              Source Budget
            </label>
            <select
              id="source-budget"
              value={sourceBudget?.id ?? ''}
              onChange={handleSourceBudgetChange}
              disabled={disabled || loading}
              className="rollover-form__select"
            >
              <option value="">Select a budget...</option>
              {budgets.map((budget) => (
                <option key={budget.id} value={budget.id}>
                  {budget.name}
                </option>
              ))}
            </select>
          </div>

          <div className="rollover-form__field">
            <label htmlFor="source-category" className="rollover-form__label">
              Source Category
            </label>
            <select
              id="source-category"
              value={action.source_budget_category_id}
              onChange={handleSourceCategoryChange}
              disabled={disabled || !sourceBudget}
              className="rollover-form__select"
            >
              <option value="">Select a category...</option>
              {sourceBudget?.categories?.map((category) => (
                <option key={category.id} value={category.id}>
                  {formatCategoryPath(category.category)}
                </option>
              ))}
            </select>
          </div>
        </div>
      </div>

      {/* Target Section */}
      <div className="rollover-form__section">
        <h4 className="rollover-form__section-title">Target</h4>

        <div className="rollover-form__row">
          <div className="rollover-form__field">
            <label htmlFor="target-budget" className="rollover-form__label">
              Target Budget
            </label>
            <select
              id="target-budget"
              value={targetBudget?.id ?? ''}
              onChange={handleTargetBudgetChange}
              disabled={disabled || loading}
              className="rollover-form__select"
            >
              <option value="">Select a budget...</option>
              {budgets.map((budget) => (
                <option key={budget.id} value={budget.id}>
                  {budget.name}
                </option>
              ))}
            </select>
          </div>

          <div className="rollover-form__field">
            <label htmlFor="target-category" className="rollover-form__label">
              Target Category
            </label>
            <select
              id="target-category"
              value={action.target_budget_category_id ?? ''}
              onChange={handleTargetCategoryChange}
              disabled={disabled}
              className="rollover-form__select"
            >
              <option value="">Unallocated Funds</option>
              {targetBudget?.categories?.map((category) => (
                <option key={category.id} value={category.id}>
                  {formatCategoryPath(category.category)}
                </option>
              ))}
            </select>
          </div>
        </div>
      </div>

      {/* Rollover Type Section */}
      <div className="rollover-form__section">
        <h4 className="rollover-form__section-title">Rollover Configuration</h4>

        <div className="rollover-form__field">
          <label htmlFor="rollover-type" className="rollover-form__label">
            Rollover Type
          </label>
          <select
            id="rollover-type"
            value={action.rollover_type}
            onChange={handleRolloverTypeChange}
            disabled={disabled}
            className="rollover-form__select"
          >
            {ROLLOVER_TYPES.map((type) => (
              <option key={type.value} value={type.value}>
                {type.label}
              </option>
            ))}
          </select>
          {currentDescription && (
            <p className="rollover-form__description">{currentDescription}</p>
          )}
        </div>

        {/* Cap Amount (for capped type) */}
        {action.rollover_type === 'capped' && (
          <div className="rollover-form__field">
            <label htmlFor="cap-amount" className="rollover-form__label">
              Cap Amount
            </label>
            <div className="rollover-form__input-group">
              <span className="rollover-form__prefix">$</span>
              <input
                id="cap-amount"
                type="number"
                value={action.cap_amount ?? 0}
                onChange={handleCapAmountChange}
                disabled={disabled}
                min={0}
                className="rollover-form__input"
              />
            </div>
          </div>
        )}

        {/* Percentage (for percentage type) */}
        {action.rollover_type === 'percentage' && (
          <div className="rollover-form__field">
            <label htmlFor="percentage" className="rollover-form__label">
              Percentage
            </label>
            <div className="rollover-form__input-group">
              <input
                id="percentage"
                type="number"
                value={action.percentage ?? 0}
                onChange={handlePercentageChange}
                disabled={disabled}
                min={0}
                max={100}
                className="rollover-form__input"
              />
              <span className="rollover-form__suffix">%</span>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
