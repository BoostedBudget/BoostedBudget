/**
 * BoostBuilder Component
 * Issue #1283: Rename rules → boosts (Phase 5)
 *
 * Main form for creating and editing boosts.
 */

import { useState, useCallback, useEffect } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useTrackEvent } from '../../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../../shared/analytics'
import { ArrowLeft, Save } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { ReadOnlyGuard } from '@/auth/ReadOnlyGuard'
import { useReadOnly } from '@/auth/ReadOnlyContext'
import {
  useCreateBoost,
  useUpdateBoost,
  useGetBoostByID,
} from '@boostedbudget/api-client/endpoints/boosts/boosts'
import {
  BOOST_TYPE_LABELS,
  BOOST_TYPE_DESCRIPTIONS,
  DEFAULT_PRIORITY,
  DEFAULT_TRIGGER_CONFIG,
} from '../boostConstants'
import type {
  BoostType,
  TriggerFormState,
  BoostConditionLocal,
  ConditionLogic,
  AlertAction,
  TransferAction,
  RolloverAction,
  SavingsGoalAction,
  BoostAction,
} from '../types'
import { TriggerSelector } from './TriggerSelector'
import { ConditionBuilder } from './ConditionBuilder'
import {
  BudgetAlertActionForm,
  TransferActionForm,
  RolloverActionForm,
  SavingsGoalActionForm,
} from './actions'
import { useBudgets } from '../../budgets/hooks/useBudgets'

interface FormErrors {
  name?: string
  boostType?: string
  trigger?: string
  action?: string
}

const BOOST_TYPES: BoostType[] = [
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
 * BoostBuilder - main form for creating/editing boosts.
 */
export function BoostBuilder() {
  const trackEvent = useTrackEvent()
  const navigate = useNavigate()
  const { id } = useParams<{ id: string }>()
  const [searchParams] = useSearchParams()
  const isEditMode = !!id
  const { isReadOnly } = useReadOnly()

  // Pre-fill from query params
  const prefilledType = searchParams.get('type') as BoostType | null
  const prefilledBudgetId = searchParams.get('budget_id')
  const prefilledCategoryId = searchParams.get('category_id')

  // Fetch existing boost for edit mode
  const { data: existingBoost, isLoading: loadingBoost } = useGetBoostByID(
    id || '',
    { query: { enabled: isEditMode } }
  )

  // Mutations
  const createMutation = useCreateBoost()
  const updateMutation = useUpdateBoost()

  // Budget data for condition category picker
  const { budgets } = useBudgets()

  // Form state
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [boostType, setBoostType] = useState<BoostType | ''>('')
  const [priority, setPriority] = useState(DEFAULT_PRIORITY)
  const [enabled, setEnabled] = useState(true)

  // Trigger state
  const [triggerConfig, setTriggerConfig] = useState<TriggerFormState>({
    type: 'schedule',
    cron: DEFAULT_TRIGGER_CONFIG.cron,
    timezone: DEFAULT_TRIGGER_CONFIG.timezone,
  })

  // Conditions state
  const [conditions, setConditions] = useState<BoostConditionLocal[]>([])
  const [conditionLogic, setConditionLogic] = useState<ConditionLogic>('AND')

  // Action state
  const [alertAction, setAlertAction] = useState<AlertAction>(DEFAULT_ALERT_ACTION)
  const [transferAction, setTransferAction] = useState<TransferAction>(DEFAULT_TRANSFER_ACTION)
  const [rolloverAction, setRolloverAction] = useState<RolloverAction>(DEFAULT_ROLLOVER_ACTION)
  const [savingsAction, setSavingsAction] = useState<SavingsGoalAction>(DEFAULT_SAVINGS_ACTION)

  // Validation errors
  const [errors, setErrors] = useState<FormErrors>({})
  const [submitted, setSubmitted] = useState(false)

  // Initialize form from existing boost or pre-fill params
  // Note: Multiple setState calls in a single effect is intentional for form initialization
  // from external data. React 18+ batches these automatically.
  useEffect(() => {
    if (existingBoost) {
      setName(existingBoost.name)
      setDescription(existingBoost.description || '')
      setBoostType(existingBoost.boost_type as BoostType)
      setPriority(existingBoost.priority)
      setEnabled(existingBoost.enabled)

      // Trigger config
      const tc = existingBoost.trigger_config
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
      if (existingBoost.conditions && Array.isArray(existingBoost.conditions)) {
        setConditions(existingBoost.conditions as BoostConditionLocal[])
      }
      setConditionLogic((existingBoost.condition_logic as ConditionLogic) || 'AND')

      // Actions - parse based on boost type
      const actions = existingBoost.actions
      if (actions) {
        switch (existingBoost.boost_type) {
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
      setBoostType(prefilledType)

      // Pre-fill budget/category for alert/rollover boosts
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
  }, [existingBoost, prefilledType, prefilledBudgetId, prefilledCategoryId])

  // Validate form
  const validate = useCallback((): FormErrors => {
    const newErrors: FormErrors = {}

    if (!name.trim()) {
      newErrors.name = 'Name is required'
    }

    if (!boostType) {
      newErrors.boostType = 'Boost type is required'
    }

    return newErrors
  }, [name, boostType])

  // Get current action based on boost type
  const getCurrentAction = useCallback((): BoostAction | null => {
    switch (boostType) {
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
  }, [boostType, alertAction, transferAction, rolloverAction, savingsAction])

  // Handle save
  const handleSave = useCallback(async () => {
    // Defense-in-depth: backend 403s for read-only users, but also block client-side
    // to prevent form submissions triggered via keyboard (Enter key on inputs).
    if (isReadOnly) return
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
        trackEvent(ANALYTICS_EVENTS.BOOST_EDITED)
      } else {
        // Create payload (no enabled field)
        await createMutation.mutateAsync({
          data: {
            name: name.trim(),
            description: description.trim() || undefined,
            boost_type: boostType as BoostType,
            priority,
            trigger_config: triggerConfigPayload,
            conditions: conditionsPayload ?? null,
            condition_logic: conditionLogic,
            actions: action as unknown,
          },
        })
        trackEvent(ANALYTICS_EVENTS.BOOST_CREATED)
      }
      navigate('/boosts')
    } catch {
      // Error handling is done by React Query
    }
  }, [
    isReadOnly,
    validate,
    getCurrentAction,
    name,
    description,
    boostType,
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
    navigate('/boosts')
  }, [navigate])

  // Handle boost type change
  const handleBoostTypeChange = useCallback((e: React.ChangeEvent<HTMLSelectElement>) => {
    setBoostType(e.target.value as BoostType)
    if (submitted) {
      setErrors((prev) => ({ ...prev, boostType: undefined }))
    }
  }, [submitted])


  // Loading state
  if (isEditMode && loadingBoost) {
    return (
      <div className="flex flex-col items-center justify-center px-6 py-16 text-center text-muted-foreground">
        <p>Loading boost...</p>
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
          {isEditMode ? 'Edit Boost' : 'Create Boost'}
        </h1>
      </header>

      <form
        className="flex flex-col gap-6"
        aria-label="Boost builder form"
        onSubmit={(e) => {
          e.preventDefault()
          handleSave()
        }}
      >
        {/* Basic Info Section */}
        <Card>
          <h2 className="mb-4 text-base font-semibold text-foreground">Basic Information</h2>

          <div className="mb-4">
            <label htmlFor="boost-name" className="mb-1.5 block text-sm font-medium text-foreground">
              Boost Name *
            </label>
            <input
              id="boost-name"
              type="text"
              value={name}
              onChange={(e) => {
                setName(e.target.value)
                if (submitted) setErrors((prev) => ({ ...prev, name: undefined }))
              }}
              placeholder="Enter boost name"
              className={cn(inputClass, errors.name && inputErrorClass)}
            />
            {errors.name && (
              <p className="mt-1.5 text-xs text-destructive">{errors.name}</p>
            )}
          </div>

          <div className="mb-4">
            <label htmlFor="boost-description" className="mb-1.5 block text-sm font-medium text-foreground">
              Description
            </label>
            <textarea
              id="boost-description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Describe what this boost does"
              rows={3}
              className={cn(inputClass, 'min-h-16 resize-y font-[inherit]')}
            />
          </div>

          <div className="mb-4">
            <label htmlFor="boost-type" className="mb-1.5 block text-sm font-medium text-foreground">
              Boost Type *
            </label>
            <select
              id="boost-type"
              value={boostType}
              onChange={handleBoostTypeChange}
              className={cn(inputClass, 'cursor-pointer', errors.boostType && inputErrorClass)}
            >
              <option value="">Select a boost type...</option>
              {BOOST_TYPES.map((type) => (
                <option key={type} value={type}>
                  {BOOST_TYPE_LABELS[type]}
                </option>
              ))}
            </select>
            {boostType && (
              <p className="mt-1.5 text-xs text-muted-foreground">
                {BOOST_TYPE_DESCRIPTIONS[boostType as BoostType]}
              </p>
            )}
            {errors.boostType && (
              <p className="mt-1.5 text-xs text-destructive">{errors.boostType}</p>
            )}
          </div>

          <div>
            <label htmlFor="boost-priority" className="mb-1.5 block text-sm font-medium text-foreground">
              Priority
            </label>
            <input
              id="boost-priority"
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
        </Card>

        {/* Trigger Section */}
        <Card>
          <h2 className="mb-4 text-base font-semibold text-foreground">Trigger</h2>
          <TriggerSelector
            config={triggerConfig}
            onChange={setTriggerConfig}
          />
        </Card>

        {/* Conditions Section */}
        <Card>
          <h2 className="mb-4 text-base font-semibold text-foreground">Conditions (Optional)</h2>
          <ConditionBuilder
            conditions={conditions}
            logic={conditionLogic}
            onConditionsChange={setConditions}
            onLogicChange={setConditionLogic}
            triggerEvent={triggerConfig.type === 'event' ? triggerConfig.event : undefined}
            budgets={budgets}
          />
        </Card>

        {/* Action Section */}
        {boostType && (
          <Card>
            <h2 className="mb-4 text-base font-semibold text-foreground">Action Configuration</h2>
            {boostType === 'budget_alert' && (
              <BudgetAlertActionForm
                action={alertAction}
                onChange={setAlertAction}
              />
            )}
            {boostType === 'transfer_suggestion' && (
              <TransferActionForm
                action={transferAction}
                onChange={setTransferAction}
              />
            )}
            {boostType === 'category_rollover' && (
              <RolloverActionForm
                action={rolloverAction}
                onChange={setRolloverAction}
              />
            )}
            {boostType === 'savings_goal' && (
              <SavingsGoalActionForm
                action={savingsAction}
                onChange={setSavingsAction}
              />
            )}
          </Card>
        )}

        {/* Form Actions — wrapped in ReadOnlyGuard: backend 403s anyway, but guard
            the UI controls as defense-in-depth (trial/read-only accounts see no submit) */}
        <ReadOnlyGuard>
          <div className="flex justify-end gap-3 pt-2">
            <Button
              type="button"
              variant="secondary"
              onClick={handleCancel}
              aria-label="Cancel"
            >
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={isPending}
              aria-label="Save boost"
            >
              <Save aria-hidden="true" />
              {isPending ? 'Saving...' : 'Save Boost'}
            </Button>
          </div>
        </ReadOnlyGuard>
      </form>
    </div>
  )
}
