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
import { formatBudgetCategoryLabel } from '../../../budgets/types'

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
    <div className={`flex flex-col gap-5 ${className}`.trim()}>
      {/* Budget Selection */}
      <div className="flex flex-col gap-1.5">
        <label htmlFor="budget-select" className="text-sm font-medium text-foreground">
          Budget
        </label>
        <select
          id="budget-select"
          value={action.budget_id ?? ''}
          onChange={handleBudgetChange}
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

      {/* Category Selection (only show when budget selected) */}
      {action.budget_id && (
        <div className="flex flex-col gap-1.5">
          <label htmlFor="category-select" className="text-sm font-medium text-foreground">
            Category
          </label>
          <select
            id="category-select"
            value={action.category_id ?? ''}
            onChange={handleCategoryChange}
            disabled={disabled}
            className="cursor-pointer rounded-lg border border-border bg-card px-3.5 py-2.5 text-sm text-foreground focus:border-blue-500 focus:outline-none focus:ring-[3px] focus:ring-blue-500/10 disabled:cursor-not-allowed disabled:opacity-50"
          >
            <option value="">All Categories</option>
            {categories.map((category) => (
              <option key={category.id} value={category.id}>
                {formatBudgetCategoryLabel(category)}
              </option>
            ))}
          </select>
        </div>
      )}

      {/* Threshold Configuration */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div className="flex flex-col gap-1.5">
          <label htmlFor="threshold-type" className="text-sm font-medium text-foreground">
            Threshold Type
          </label>
          <select
            id="threshold-type"
            value={action.threshold_type}
            onChange={handleThresholdTypeChange}
            disabled={disabled}
            className="cursor-pointer rounded-lg border border-border bg-card px-3.5 py-2.5 text-sm text-foreground focus:border-blue-500 focus:outline-none focus:ring-[3px] focus:ring-blue-500/10 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {THRESHOLD_TYPES.map((type) => (
              <option key={type.value} value={type.value}>
                {type.label}
              </option>
            ))}
          </select>
        </div>

        <div className="flex flex-col gap-1.5">
          <label htmlFor="threshold-value" className="text-sm font-medium text-foreground">
            Threshold Value
          </label>
          <div className="flex items-center overflow-hidden rounded-lg border border-border bg-card focus-within:border-blue-500 focus-within:ring-[3px] focus-within:ring-blue-500/10">
            {action.threshold_type === 'amount' && (
              <span className="bg-muted px-3 py-2.5 text-sm font-medium text-muted-foreground">$</span>
            )}
            <input
              id="threshold-value"
              type="number"
              value={action.threshold_value}
              onChange={handleThresholdValueChange}
              disabled={disabled}
              min={0}
              max={action.threshold_type === 'percentage' ? 100 : undefined}
              className="min-w-0 flex-1 border-none bg-transparent px-3.5 py-2.5 text-sm focus:outline-none focus:ring-0 disabled:cursor-not-allowed disabled:opacity-50"
            />
            {action.threshold_type === 'percentage' && (
              <span className="bg-muted px-3 py-2.5 text-sm font-medium text-muted-foreground">%</span>
            )}
          </div>
        </div>
      </div>

      {/* Notification Channels */}
      <fieldset className="m-0 border-none p-0" aria-label="Notification Channels">
        <legend className="mb-2 text-sm font-medium text-foreground">Notification Channels</legend>
        <div className="flex flex-wrap gap-4">
          {NOTIFICATION_CHANNELS.map((channel) => (
            <label key={channel.value} className="flex cursor-pointer items-center gap-2">
              <input
                type="checkbox"
                checked={action.notification_channels.includes(channel.value)}
                onChange={() => handleChannelToggle(channel.value)}
                disabled={disabled}
                className="h-[1.125rem] w-[1.125rem] cursor-pointer accent-blue-600 disabled:cursor-not-allowed"
                aria-label={channel.label}
              />
              <span className="text-sm text-foreground">{channel.label}</span>
            </label>
          ))}
        </div>
      </fieldset>

      {/* Title Template */}
      <div className="flex flex-col gap-1.5">
        <label htmlFor="title-template" className="text-sm font-medium text-foreground">
          Title Template
        </label>
        <input
          id="title-template"
          type="text"
          value={action.title_template ?? DEFAULT_TITLE_TEMPLATE}
          onChange={handleTitleChange}
          disabled={disabled}
          placeholder={DEFAULT_TITLE_TEMPLATE}
          className="rounded-lg border border-border bg-card px-3.5 py-2.5 text-sm text-foreground focus:border-blue-500 focus:outline-none focus:ring-[3px] focus:ring-blue-500/10 disabled:cursor-not-allowed disabled:opacity-50"
        />
      </div>

      {/* Message Template */}
      <div className="flex flex-col gap-1.5">
        <label htmlFor="message-template" className="text-sm font-medium text-foreground">
          Message Template
        </label>
        <textarea
          id="message-template"
          value={action.message_template ?? DEFAULT_MESSAGE_TEMPLATE}
          onChange={handleMessageChange}
          disabled={disabled}
          placeholder={DEFAULT_MESSAGE_TEMPLATE}
          rows={3}
          className="min-h-[80px] resize-y rounded-lg border border-border bg-card px-3.5 py-2.5 font-[inherit] text-sm text-foreground focus:border-blue-500 focus:outline-none focus:ring-[3px] focus:ring-blue-500/10 disabled:cursor-not-allowed disabled:opacity-50"
        />
      </div>

      {/* Available Placeholders */}
      <div className="rounded-lg border border-border bg-muted p-3">
        <span className="mb-2 block text-xs font-semibold text-muted-foreground">Available Placeholders:</span>
        <div className="flex flex-wrap gap-1.5">
          {AVAILABLE_PLACEHOLDERS.map((placeholder) => (
            <span
              key={placeholder.name}
              className="cursor-help rounded bg-blue-100 px-2 py-1 font-mono text-xs text-blue-800 dark:bg-blue-900 dark:text-blue-300"
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
