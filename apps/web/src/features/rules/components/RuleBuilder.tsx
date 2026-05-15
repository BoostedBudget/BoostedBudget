/**
 * RuleBuilder Component
 * Issue #39: Rule Builder UI
 *
 * Main form for creating and editing automation rules.
 */

import { useState, useCallback, useEffect } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useTrackEvent } from '../../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../../shared/analytics'
import { ArrowLeft, Save } from 'lucide-react'
import { cn } from '@/lib/utils'
import {
  useCreateAutomationRule,
  useUpdateAutomationRule,
  useGetAutomationRuleByID,
} from '@boostedbudget/api-client/endpoints/automation-rules/automation-rules'
import {
  RULE_TYPE_LABELS,
  RULE_TYPE_DESCRIPTIONS,
  DEFAULT_PRIORITY,
  DEFAULT_TRIGGER_CONFIG,
} from '../constants'
import type {
  RuleType,
  TriggerFormState,
  RuleCondition,
  ConditionLogic,
  AlertAction,
  TransferAction,
  RolloverAction,
  SavingsGoalAction,
  RuleAction,
} from '../types'
import { TriggerSelector } from './TriggerSelector'
import { ConditionBuilder } from './ConditionBuilder'
import {
  BudgetAlertActionForm,
  TransferActionForm,
  RolloverActionForm,
  SavingsGoalActionForm,
} from './actions'

interface FormErrors {
  name?: string
  ruleType?: string
  trigger?: string
  action?: string
}

const RULE_TYPES: RuleType[] = [
  'budget_alert',
  'transfer_suggestion',
  'category_rollover',
  'savings_goal',
]

const DEFAULT_ALERT_ACTION: AlertAction = {
  notification_channels: ['in_app'],
  threshold_type: 'percentage',
  threshold_value: 80,
}

const DEFAULT_TRANSFER_ACTION: TransferAction = {
  from_account_id: '',
  to_account_id: '',
  amount_type: 'fixed',
  amount: 100,
}

const DEFAULT_ROLLOVER_ACTION: RolloverAction = {
  source_budget_category_id: '',
  target_budget_category_id: null,
  rollover_type: 'full',
}

const DEFAULT_SAVINGS_ACTION: SavingsGoalAction = {
  savings_goal_id: '',
  from_account_id: '',
  amount_type: 'fixed',
  amount: 100,
}

/**
 * RuleBuilder - main form for creating/editing automation rules.
 */
export function RuleBuilder() {
  const trackEvent = useTrackEvent()
  const navigate = useNavigate()
  const { id } = useParams<{ id: string }>()
  const [searchParams] = useSearchParams()
  const isEditMode = !!id

  // Pre-fill from query params
  const prefilledType = searchParams.get('type') as RuleType | null
  const prefilledBudgetId = searchParams.get('budget_id')
  const prefilledCategoryId = searchParams.get('category_id')

  // Fetch existing rule for edit mode
  const { data: existingRule, isLoading: loadingRule } = useGetAutomationRuleByID(
    id || '',
    { query: { enabled: isEditMode } }
  )

  // Mutations
  const createMutation = useCreateAutomationRule()
  const updateMutation = useUpdateAutomationRule()

  // Form state
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [ruleType, setRuleType] = useState<RuleType | ''>('')
  const [priority, setPriority] = useState(DEFAULT_PRIORITY)
  const [enabled, setEnabled] = useState(true)

  // Trigger state
  const [triggerConfig, setTriggerConfig] = useState<TriggerFormState>({
    type: 'schedule',
    cron: DEFAULT_TRIGGER_CONFIG.cron,
    timezone: DEFAULT_TRIGGER_CONFIG.timezone,
  })

  // Conditions state
  const [conditions, setConditions] = useState<RuleCondition[]>([])
  const [conditionLogic, setConditionLogic] = useState<ConditionLogic>('AND')

  // Action state
  const [alertAction, setAlertAction] = useState<AlertAction>(DEFAULT_ALERT_ACTION)
  const [transferAction, setTransferAction] = useState<TransferAction>(DEFAULT_TRANSFER_ACTION)
  const [rolloverAction, setRolloverAction] = useState<RolloverAction>(DEFAULT_ROLLOVER_ACTION)
  const [savingsAction, setSavingsAction] = useState<SavingsGoalAction>(DEFAULT_SAVINGS_ACTION)

  // Validation errors
  const [errors, setErrors] = useState<FormErrors>({})
  const [submitted, setSubmitted] = useState(false)

  // Initialize form from existing rule or pre-fill params
  // Note: Multiple setState calls in a single effect is intentional for form initialization
  // from external data. React 18+ batches these automatically.
  /* eslint-disable react-hooks/set-state-in-effect */
  useEffect(() => {
    if (existingRule) {
      setName(existingRule.name)
      setDescription(existingRule.description || '')
      setRuleType(existingRule.rule_type as RuleType)
      setPriority(existingRule.priority)
      setEnabled(existingRule.enabled)

      // Trigger config
      const tc = existingRule.trigger_config
      if (tc) {
        if (tc.type === 'schedule') {
          setTriggerConfig({
            type: 'schedule',
            cron: tc.cron || DEFAULT_TRIGGER_CONFIG.cron,
            timezone: tc.timezone || DEFAULT_TRIGGER_CONFIG.timezone,
          })
        } else if (tc.type === 'event') {
          setTriggerConfig({
            type: 'event',
            event: (tc.event as TriggerFormState['event']) || 'budget_updated',
          })
        }
      }

      // Conditions
      if (existingRule.conditions && Array.isArray(existingRule.conditions)) {
        setConditions(existingRule.conditions as RuleCondition[])
      }
      setConditionLogic((existingRule.condition_logic as ConditionLogic) || 'AND')

      // Actions - parse based on rule type
      const actions = existingRule.actions
      if (actions) {
        switch (existingRule.rule_type) {
          case 'budget_alert':
            setAlertAction(actions as unknown as AlertAction)
            break
          case 'transfer_suggestion':
            setTransferAction(actions as unknown as TransferAction)
            break
          case 'category_rollover':
            setRolloverAction(actions as unknown as RolloverAction)
            break
          case 'savings_goal':
            setSavingsAction(actions as unknown as SavingsGoalAction)
            break
        }
      }
    } else if (prefilledType) {
      setRuleType(prefilledType)

      // Pre-fill budget/category for alert/rollover rules
      if (prefilledBudgetId) {
        if (prefilledType === 'budget_alert') {
          setAlertAction((prev) => ({
            ...prev,
            budget_id: prefilledBudgetId,
            category_id: prefilledCategoryId || undefined,
          }))
        } else if (prefilledType === 'category_rollover' && prefilledCategoryId) {
          setRolloverAction((prev) => ({
            ...prev,
            source_budget_category_id: prefilledCategoryId,
          }))
        }
      }
    }
  }, [existingRule, prefilledType, prefilledBudgetId, prefilledCategoryId])
  /* eslint-enable react-hooks/set-state-in-effect */

  // Validate form
  const validate = useCallback((): FormErrors => {
    const newErrors: FormErrors = {}

    if (!name.trim()) {
      newErrors.name = 'Name is required'
    }

    if (!ruleType) {
      newErrors.ruleType = 'Rule type is required'
    }

    return newErrors
  }, [name, ruleType])

  // Get current action based on rule type
  const getCurrentAction = useCallback((): RuleAction | null => {
    switch (ruleType) {
      case 'budget_alert':
        return alertAction
      case 'transfer_suggestion':
        return transferAction
      case 'category_rollover':
        return rolloverAction
      case 'savings_goal':
        return savingsAction
      default:
        return null
    }
  }, [ruleType, alertAction, transferAction, rolloverAction, savingsAction])

  // Handle save
  const handleSave = useCallback(async () => {
    setSubmitted(true)
    const validationErrors = validate()
    setErrors(validationErrors)

    if (Object.keys(validationErrors).length > 0) {
      return
    }

    const action = getCurrentAction()
    if (!action) {
      setErrors({ action: 'Action configuration is required' })
      return
    }

    const triggerConfigPayload = {
      type: triggerConfig.type,
      ...(triggerConfig.type === 'schedule'
        ? { cron: triggerConfig.cron!, timezone: triggerConfig.timezone! }
        : { event: triggerConfig.event! }),
    }

    const conditionsPayload = conditions.length > 0 ? conditions : undefined

    try {
      if (isEditMode && id) {
        // Update payload includes enabled
        await updateMutation.mutateAsync({
          id,
          data: {
            name: name.trim(),
            description: description.trim() || undefined,
            priority,
            enabled,
            trigger_config: triggerConfigPayload,
            conditions: conditionsPayload,
            condition_logic: conditionLogic,
            actions: action as unknown,
          },
        })
        trackEvent(ANALYTICS_EVENTS.RULE_EDITED)
      } else {
        // Create payload (no enabled field)
        await createMutation.mutateAsync({
          data: {
            name: name.trim(),
            description: description.trim() || undefined,
            rule_type: ruleType as RuleType,
            priority,
            trigger_config: triggerConfigPayload,
            conditions: conditionsPayload ?? null,
            condition_logic: conditionLogic,
            actions: action as unknown,
          },
        })
        trackEvent(ANALYTICS_EVENTS.RULE_CREATED)
      }
      navigate('/rules')
    } catch {
      // Error handling is done by React Query
    }
  }, [
    validate,
    getCurrentAction,
    name,
    description,
    ruleType,
    priority,
    enabled,
    triggerConfig,
    conditions,
    conditionLogic,
    isEditMode,
    id,
    updateMutation,
    createMutation,
    trackEvent,
    navigate,
  ])

  // Handle cancel
  const handleCancel = useCallback(() => {
    navigate('/rules')
  }, [navigate])

  // Handle rule type change
  const handleRuleTypeChange = useCallback((e: React.ChangeEvent<HTMLSelectElement>) => {
    setRuleType(e.target.value as RuleType)
    if (submitted) {
      setErrors((prev) => ({ ...prev, ruleType: undefined }))
    }
  }, [submitted])


  // Loading state
  if (isEditMode && loadingRule) {
    return (
      <div className="flex flex-col items-center justify-center px-6 py-16 text-center text-muted-foreground">
        <p>Loading rule...</p>
      </div>
    )
  }

  const isPending = createMutation.isPending || updateMutation.isPending

  // Shared input class
  const inputClass = 'w-full rounded-lg border border-input bg-background px-3.5 py-2.5 text-sm text-foreground transition-colors focus:border-ring focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2'
  const inputErrorClass = 'border-destructive focus:border-destructive focus:ring-destructive/30'

  return (
    <div className="mx-auto max-w-[800px] p-6">
      <header className="mb-6 flex items-center gap-4">
        <button
          type="button"
          onClick={handleCancel}
          className="flex h-10 w-10 items-center justify-center rounded-lg border border-border bg-transparent text-muted-foreground transition-all hover:bg-muted hover:text-foreground [&>svg]:h-5 [&>svg]:w-5"
          aria-label="Go back"
        >
          <ArrowLeft aria-hidden="true" />
        </button>
        <h1 className="text-2xl font-semibold text-foreground">
          {isEditMode ? 'Edit Rule' : 'Create Automation Rule'}
        </h1>
      </header>

      <form
        className="flex flex-col gap-6"
        aria-label="Rule builder form"
        onSubmit={(e) => {
          e.preventDefault()
          handleSave()
        }}
      >
        {/* Basic Info Section */}
        <section className="rounded-xl border border-border bg-card p-5">
          <h2 className="mb-4 text-base font-semibold text-foreground">Basic Information</h2>

          <div className="mb-4">
            <label htmlFor="rule-name" className="mb-1.5 block text-sm font-medium text-foreground">
              Rule Name *
            </label>
            <input
              id="rule-name"
              type="text"
              value={name}
              onChange={(e) => {
                setName(e.target.value)
                if (submitted) setErrors((prev) => ({ ...prev, name: undefined }))
              }}
              placeholder="Enter rule name"
              className={cn(inputClass, errors.name && inputErrorClass)}
            />
            {errors.name && (
              <p className="mt-1.5 text-xs text-destructive">{errors.name}</p>
            )}
          </div>

          <div className="mb-4">
            <label htmlFor="rule-description" className="mb-1.5 block text-sm font-medium text-foreground">
              Description
            </label>
            <textarea
              id="rule-description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Describe what this rule does"
              rows={3}
              className={cn(inputClass, 'min-h-16 resize-y font-[inherit]')}
            />
          </div>

          <div className="mb-4">
            <label htmlFor="rule-type" className="mb-1.5 block text-sm font-medium text-foreground">
              Rule Type *
            </label>
            <select
              id="rule-type"
              value={ruleType}
              onChange={handleRuleTypeChange}
              className={cn(inputClass, 'cursor-pointer', errors.ruleType && inputErrorClass)}
            >
              <option value="">Select a rule type...</option>
              {RULE_TYPES.map((type) => (
                <option key={type} value={type}>
                  {RULE_TYPE_LABELS[type]}
                </option>
              ))}
            </select>
            {ruleType && (
              <p className="mt-1.5 text-xs text-muted-foreground">
                {RULE_TYPE_DESCRIPTIONS[ruleType as RuleType]}
              </p>
            )}
            {errors.ruleType && (
              <p className="mt-1.5 text-xs text-destructive">{errors.ruleType}</p>
            )}
          </div>

          <div>
            <label htmlFor="rule-priority" className="mb-1.5 block text-sm font-medium text-foreground">
              Priority
            </label>
            <input
              id="rule-priority"
              type="number"
              value={priority}
              onChange={(e) => setPriority(parseInt(e.target.value, 10) || 0)}
              min={1}
              max={100}
              className={cn(inputClass, 'w-32')}
            />
            <p className="mt-1.5 text-xs text-muted-foreground">
              Lower numbers run first. Default is 10.
            </p>
          </div>
        </section>

        {/* Trigger Section */}
        <section className="rounded-xl border border-border bg-card p-5">
          <h2 className="mb-4 text-base font-semibold text-foreground">Trigger</h2>
          <TriggerSelector
            config={triggerConfig}
            onChange={setTriggerConfig}
          />
        </section>

        {/* Conditions Section */}
        <section className="rounded-xl border border-border bg-card p-5">
          <h2 className="mb-4 text-base font-semibold text-foreground">Conditions (Optional)</h2>
          <ConditionBuilder
            conditions={conditions}
            logic={conditionLogic}
            onConditionsChange={setConditions}
            onLogicChange={setConditionLogic}
          />
        </section>

        {/* Action Section */}
        {ruleType && (
          <section className="rounded-xl border border-border bg-card p-5">
            <h2 className="mb-4 text-base font-semibold text-foreground">Action Configuration</h2>
            {ruleType === 'budget_alert' && (
              <BudgetAlertActionForm
                action={alertAction}
                onChange={setAlertAction}
              />
            )}
            {ruleType === 'transfer_suggestion' && (
              <TransferActionForm
                action={transferAction}
                onChange={setTransferAction}
              />
            )}
            {ruleType === 'category_rollover' && (
              <RolloverActionForm
                action={rolloverAction}
                onChange={setRolloverAction}
              />
            )}
            {ruleType === 'savings_goal' && (
              <SavingsGoalActionForm
                action={savingsAction}
                onChange={setSavingsAction}
              />
            )}
          </section>
        )}

        {/* Form Actions */}
        <div className="flex justify-end gap-3 pt-2">
          <button
            type="button"
            onClick={handleCancel}
            className="inline-flex items-center gap-2 rounded-lg bg-muted px-5 py-2.5 text-sm font-medium text-muted-foreground transition-colors hover:bg-muted/80 dark:bg-muted/50 dark:hover:bg-muted/40"
            aria-label="Cancel"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={isPending}
            className="inline-flex items-center gap-2 rounded-lg bg-primary px-5 py-2.5 text-sm font-medium text-primary-foreground transition-all hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-50 [&>svg]:h-4 [&>svg]:w-4"
            aria-label="Save rule"
          >
            <Save aria-hidden="true" />
            {isPending ? 'Saving...' : 'Save Rule'}
          </button>
        </div>
      </form>
    </div>
  )
}
