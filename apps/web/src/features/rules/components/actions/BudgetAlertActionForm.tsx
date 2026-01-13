/**
 * BudgetAlertActionForm Component
 * Issue #39: Rule Builder UI
 *
 * Form for configuring budget alert actions.
 * Sends notifications when budget thresholds are exceeded.
 */

import { useCallback, useMemo } from 'react'
import { useBudgets } from '../../../budgets/hooks/useBudgets'
import type { AlertAction, NotificationChannel, ThresholdType } from '../../types'
import { formatCategoryPath } from '../../../budgets/types'
import './BudgetAlertActionForm.css'

export interface BudgetAlertActionFormProps {
  /** Current action configuration */
  action: AlertAction
  /** Called when action changes */
  onChange: (action: AlertAction) => void
  /** Disable all controls */
  disabled?: boolean
  /** Additional CSS class */
  className?: string
}

const DEFAULT_MESSAGE_TEMPLATE =
  'Budget "{{budget_name}}" has reached {{percent_used}}% of its limit. Remaining: ${{remaining_amount}}'
const DEFAULT_TITLE_TEMPLATE = 'Budget Alert: {{budget_name}}'

const AVAILABLE_PLACEHOLDERS = [
  { name: '{{budget_name}}', description: 'Budget name' },
  { name: '{{percent_used}}', description: 'Percentage used' },
  { name: '{{remaining_amount}}', description: 'Remaining amount' },
  { name: '{{total_spent}}', description: 'Total amount spent' },
  { name: '{{budget_amount}}', description: 'Budget limit amount' },
  { name: '{{category_name}}', description: 'Category name (if selected)' },
]

const NOTIFICATION_CHANNELS: { value: NotificationChannel; label: string }[] = [
  { value: 'email', label: 'Email' },
  { value: 'push', label: 'Push Notification' },
  { value: 'in_app', label: 'In-App Notification' },
]

const THRESHOLD_TYPES: { value: ThresholdType; label: string }[] = [
  { value: 'percentage', label: 'Percentage' },
  { value: 'amount', label: 'Amount' },
]

/**
 * BudgetAlertActionForm component for configuring budget alert actions.
 */
export function BudgetAlertActionForm({
  action,
  onChange,
  disabled = false,
  className = '',
}: BudgetAlertActionFormProps) {
  const { budgets, loading } = useBudgets()

  // Get categories for selected budget
  const selectedBudget = useMemo(() => {
    if (!action.budget_id) return null
    return budgets.find((b) => b.id === action.budget_id) ?? null
  }, [budgets, action.budget_id])

  const categories = selectedBudget?.categories ?? []

  // Handlers
  const handleBudgetChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      const newBudgetId = e.target.value || undefined
      onChange({
        ...action,
        budget_id: newBudgetId,
        category_id: undefined, // Clear category when budget changes
      })
    },
    [action, onChange]
  )

  const handleCategoryChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      const newCategoryId = e.target.value || undefined
      onChange({
        ...action,
        category_id: newCategoryId,
      })
    },
    [action, onChange]
  )

  const handleThresholdTypeChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      onChange({
        ...action,
        threshold_type: e.target.value as ThresholdType,
      })
    },
    [action, onChange]
  )

  const handleThresholdValueChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const value = parseFloat(e.target.value) || 0
      onChange({
        ...action,
        threshold_value: value,
      })
    },
    [action, onChange]
  )

  const handleChannelToggle = useCallback(
    (channel: NotificationChannel) => {
      const currentChannels = action.notification_channels
      const newChannels = currentChannels.includes(channel)
        ? currentChannels.filter((c) => c !== channel)
        : [...currentChannels, channel]
      onChange({
        ...action,
        notification_channels: newChannels,
      })
    },
    [action, onChange]
  )

  const handleTitleChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      onChange({
        ...action,
        title_template: e.target.value,
      })
    },
    [action, onChange]
  )

  const handleMessageChange = useCallback(
    (e: React.ChangeEvent<HTMLTextAreaElement>) => {
      onChange({
        ...action,
        message_template: e.target.value,
      })
    },
    [action, onChange]
  )

  return (
    <div className={`budget-alert-form ${className}`.trim()}>
      {/* Budget Selection */}
      <div className="budget-alert-form__field">
        <label htmlFor="budget-select" className="budget-alert-form__label">
          Budget
        </label>
        <select
          id="budget-select"
          value={action.budget_id ?? ''}
          onChange={handleBudgetChange}
          disabled={disabled || loading}
          className="budget-alert-form__select"
        >
          <option value="">Select a budget...</option>
          {budgets.map((budget) => (
            <option key={budget.id} value={budget.id}>
              {budget.name}
            </option>
          ))}
        </select>
      </div>

      {/* Category Selection (only show when budget selected) */}
      {action.budget_id && (
        <div className="budget-alert-form__field">
          <label htmlFor="category-select" className="budget-alert-form__label">
            Category
          </label>
          <select
            id="category-select"
            value={action.category_id ?? ''}
            onChange={handleCategoryChange}
            disabled={disabled}
            className="budget-alert-form__select"
          >
            <option value="">All Categories</option>
            {categories.map((category) => (
              <option key={category.id} value={category.id}>
                {formatCategoryPath(category.category)}
              </option>
            ))}
          </select>
        </div>
      )}

      {/* Threshold Configuration */}
      <div className="budget-alert-form__row">
        <div className="budget-alert-form__field">
          <label htmlFor="threshold-type" className="budget-alert-form__label">
            Threshold Type
          </label>
          <select
            id="threshold-type"
            value={action.threshold_type}
            onChange={handleThresholdTypeChange}
            disabled={disabled}
            className="budget-alert-form__select"
          >
            {THRESHOLD_TYPES.map((type) => (
              <option key={type.value} value={type.value}>
                {type.label}
              </option>
            ))}
          </select>
        </div>

        <div className="budget-alert-form__field">
          <label htmlFor="threshold-value" className="budget-alert-form__label">
            Threshold Value
          </label>
          <div className="budget-alert-form__input-group">
            {action.threshold_type === 'amount' && (
              <span className="budget-alert-form__prefix">$</span>
            )}
            <input
              id="threshold-value"
              type="number"
              value={action.threshold_value}
              onChange={handleThresholdValueChange}
              disabled={disabled}
              min={0}
              max={action.threshold_type === 'percentage' ? 100 : undefined}
              className="budget-alert-form__input"
            />
            {action.threshold_type === 'percentage' && (
              <span className="budget-alert-form__suffix">%</span>
            )}
          </div>
        </div>
      </div>

      {/* Notification Channels */}
      <fieldset className="budget-alert-form__fieldset" aria-label="Notification Channels">
        <legend className="budget-alert-form__legend">Notification Channels</legend>
        <div className="budget-alert-form__channels">
          {NOTIFICATION_CHANNELS.map((channel) => (
            <label key={channel.value} className="budget-alert-form__checkbox-label">
              <input
                type="checkbox"
                checked={action.notification_channels.includes(channel.value)}
                onChange={() => handleChannelToggle(channel.value)}
                disabled={disabled}
                className="budget-alert-form__checkbox"
                aria-label={channel.label}
              />
              <span className="budget-alert-form__checkbox-text">{channel.label}</span>
            </label>
          ))}
        </div>
      </fieldset>

      {/* Title Template */}
      <div className="budget-alert-form__field">
        <label htmlFor="title-template" className="budget-alert-form__label">
          Title Template
        </label>
        <input
          id="title-template"
          type="text"
          value={action.title_template ?? DEFAULT_TITLE_TEMPLATE}
          onChange={handleTitleChange}
          disabled={disabled}
          placeholder={DEFAULT_TITLE_TEMPLATE}
          className="budget-alert-form__input"
        />
      </div>

      {/* Message Template */}
      <div className="budget-alert-form__field">
        <label htmlFor="message-template" className="budget-alert-form__label">
          Message Template
        </label>
        <textarea
          id="message-template"
          value={action.message_template ?? DEFAULT_MESSAGE_TEMPLATE}
          onChange={handleMessageChange}
          disabled={disabled}
          placeholder={DEFAULT_MESSAGE_TEMPLATE}
          rows={3}
          className="budget-alert-form__textarea"
        />
      </div>

      {/* Available Placeholders */}
      <div className="budget-alert-form__placeholders">
        <span className="budget-alert-form__placeholders-label">Available Placeholders:</span>
        <div className="budget-alert-form__placeholder-list">
          {AVAILABLE_PLACEHOLDERS.map((placeholder) => (
            <span
              key={placeholder.name}
              className="budget-alert-form__placeholder"
              title={placeholder.description}
            >
              {placeholder.name}
            </span>
          ))}
        </div>
      </div>
    </div>
  )
}
