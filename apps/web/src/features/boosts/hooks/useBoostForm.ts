/**
 * useBoostForm Hook
 * Issue #39: Boost Builder UI
 *
 * React hook for managing automation boost form state.
 * Handles create/edit form for all boost types with validation.
 */

import { useState, useCallback } from 'react'
import type {
  BoostType,
  TriggerFormState,
  BoostConditionLocal,
  ConditionLogic,
  BoostAction,
  BoostFormState,
  BoostFormErrors,
} from '../types'
import type { CreateBoostRequest, TriggerConfig } from '@boostedbudget/api-client/models'
import { DEFAULT_PRIORITY, DEFAULT_TRIGGER_CONFIG } from '../boostConstants'

export interface UseBoostFormOptions {
  initialValues?: Partial<BoostFormState>
}

export interface UseBoostFormReturn {
  formState: BoostFormState
  errors: BoostFormErrors
  // Basic field setters
  setName: (name: string) => void
  setDescription: (description: string) => void
  setPriority: (priority: number) => void
  setEnabled: (enabled: boolean) => void
  // Rule configuration setters
  setBoostType: (boostType: BoostType | null) => void
  setTriggerConfig: (config: Partial<TriggerFormState>) => void
  // Condition management
  addCondition: (condition: BoostConditionLocal) => void
  updateCondition: (index: number, condition: BoostConditionLocal) => void
  removeCondition: (index: number) => void
  setConditionLogic: (logic: ConditionLogic) => void
  // Action management
  setActions: (actions: BoostAction | null) => void
  // Validation and submission
  validate: () => boolean
  buildApiRequest: () => CreateBoostRequest | null
  // Utilities
  reset: () => void
  clearError: (field: keyof BoostFormErrors) => void
}

function createInitialState(initialValues?: Partial<BoostFormState>): BoostFormState {
  return {
    name: initialValues?.name ?? '',
    description: initialValues?.description ?? '',
    priority: initialValues?.priority ?? DEFAULT_PRIORITY,
    enabled: initialValues?.enabled ?? true,
    boostType: initialValues?.boostType ?? null,
    triggerConfig: {
      ...DEFAULT_TRIGGER_CONFIG,
      ...initialValues?.triggerConfig,
    },
    conditions: initialValues?.conditions ?? [],
    conditionLogic: initialValues?.conditionLogic ?? 'AND',
    actions: initialValues?.actions ?? null,
    isDirty: false,
    isValid: false,
  }
}

/**
 * Hook for managing automation rule form state
 *
 * @param options - Optional initial values for the form
 * @returns Form state, errors, and handlers
 *
 * @example
 * ```tsx
 * function RuleBuilder() {
 *   const {
 *     formState,
 *     errors,
 *     setName,
 *     setRuleType,
 *     validate,
 *     buildApiRequest,
 *   } = useRuleForm()
 *
 *   const handleSubmit = async () => {
 *     if (!validate()) return
 *     const request = buildApiRequest()
 *     if (request) {
 *       await createRule(request)
 *     }
 *   }
 *
 *   return (
 *     <form onSubmit={handleSubmit}>
 *       <input
 *         value={formState.name}
 *         onChange={(e) => setName(e.target.value)}
 *       />
 *       {errors.name && <span>{errors.name}</span>}
 *     </form>
 *   )
 * }
 * ```
 */
export function useBoostForm(options?: UseBoostFormOptions): UseBoostFormReturn {
  const initialState = createInitialState(options?.initialValues)

  const [formState, setFormState] = useState<BoostFormState>(initialState)
  const [errors, setErrors] = useState<BoostFormErrors>({})

  // Clear specific error
  const clearError = useCallback((field: keyof BoostFormErrors) => {
    setErrors((prev) => {
      const next = { ...prev }
      delete next[field]
      return next
    })
  }, [])

  // Basic field setters
  const setName = useCallback((name: string) => {
    setFormState((prev) => ({ ...prev, name, isDirty: true }))
    clearError('name')
  }, [clearError])

  const setDescription = useCallback((description: string) => {
    setFormState((prev) => ({ ...prev, description, isDirty: true }))
  }, [])

  const setPriority = useCallback((priority: number) => {
    setFormState((prev) => ({ ...prev, priority, isDirty: true }))
  }, [])

  const setEnabled = useCallback((enabled: boolean) => {
    setFormState((prev) => ({ ...prev, enabled, isDirty: true }))
  }, [])

  // Rule type setter - resets actions when type changes
  const setBoostType = useCallback((boostType: BoostType | null) => {
    setFormState((prev) => {
      // Reset actions when boost type changes
      const shouldResetActions = prev.boostType !== null && prev.boostType !== boostType
      return {
        ...prev,
        boostType,
        actions: shouldResetActions ? null : prev.actions,
        isDirty: true,
      }
    })
    clearError('boostType')
  }, [clearError])

  // Trigger config setter - merges with existing config
  const setTriggerConfig = useCallback((config: Partial<TriggerFormState>) => {
    setFormState((prev) => ({
      ...prev,
      triggerConfig: { ...prev.triggerConfig, ...config },
      isDirty: true,
    }))
    // Clear trigger errors
    setErrors((prev) => {
      const next = { ...prev }
      delete next.trigger
      return next
    })
  }, [])

  // Condition management
  const addCondition = useCallback((condition: BoostConditionLocal) => {
    setFormState((prev) => ({
      ...prev,
      conditions: [...prev.conditions, condition],
      isDirty: true,
    }))
  }, [])

  const updateCondition = useCallback((index: number, condition: BoostConditionLocal) => {
    setFormState((prev) => ({
      ...prev,
      conditions: prev.conditions.map((c, i) => (i === index ? condition : c)),
      isDirty: true,
    }))
  }, [])

  const removeCondition = useCallback((index: number) => {
    setFormState((prev) => ({
      ...prev,
      conditions: prev.conditions.filter((_, i) => i !== index),
      isDirty: true,
    }))
  }, [])

  const setConditionLogic = useCallback((logic: ConditionLogic) => {
    setFormState((prev) => ({ ...prev, conditionLogic: logic, isDirty: true }))
  }, [])

  // Action setter
  const setActions = useCallback((actions: BoostAction | null) => {
    setFormState((prev) => ({ ...prev, actions, isDirty: true }))
  }, [])

  // Validation
  const validate = useCallback((): boolean => {
    const newErrors: BoostFormErrors = {}

    // Name validation
    if (!formState.name.trim()) {
      newErrors.name = 'Name is required'
    }

    // Rule type validation
    if (!formState.boostType) {
      newErrors.boostType = 'Boost type is required'
    }

    // Trigger validation
    if (formState.triggerConfig.type === 'schedule') {
      if (!formState.triggerConfig.cron?.trim()) {
        newErrors.trigger = {
          ...newErrors.trigger,
          cron: 'Cron expression is required for scheduled triggers',
        }
      }
    } else if (formState.triggerConfig.type === 'event') {
      if (!formState.triggerConfig.event) {
        newErrors.trigger = {
          ...newErrors.trigger,
          event: 'Event type is required for event triggers',
        }
      }
    }

    setErrors(newErrors)
    const isValid = Object.keys(newErrors).length === 0
    setFormState((prev) => ({ ...prev, isValid }))

    return isValid
  }, [formState.name, formState.boostType, formState.triggerConfig])

  // Build API request
  const buildApiRequest = useCallback((): CreateBoostRequest | null => {
    // Validate first
    if (!validate()) {
      return null
    }

    if (!formState.boostType) {
      return null
    }

    // Build trigger config for API
    const triggerConfig: TriggerConfig = {
      type: formState.triggerConfig.type,
    }

    if (formState.triggerConfig.type === 'schedule') {
      triggerConfig.cron = formState.triggerConfig.cron
      triggerConfig.timezone = formState.triggerConfig.timezone
    } else {
      triggerConfig.event = formState.triggerConfig.event
      if (formState.triggerConfig.filters) {
        triggerConfig.filters = formState.triggerConfig.filters
      }
    }

    // Build request
    const request: CreateBoostRequest = {
      name: formState.name.trim(),
      description: formState.description.trim() || undefined,
      boost_type: formState.boostType,
      priority: formState.priority,
      trigger_config: triggerConfig,
      conditions: formState.conditions.length > 0
        ? formState.conditions.map((c) => ({
            field: c.field,
            operator: c.operator as string,
            value: c.value,
            ...(c.category_id ? { category_id: c.category_id } : {}),
            ...(c.budget_id ? { budget_id: c.budget_id } : {}),
          }))
        : undefined,
      condition_logic: formState.conditions.length > 1 ? formState.conditionLogic : undefined,
      actions: formState.actions ?? undefined,
    }

    return request
  }, [formState, validate])

  // Reset to initial state
  const reset = useCallback(() => {
    setFormState(createInitialState(options?.initialValues))
    setErrors({})
  }, [options?.initialValues])

  return {
    formState,
    errors,
    setName,
    setDescription,
    setPriority,
    setEnabled,
    setBoostType,
    setTriggerConfig,
    addCondition,
    updateCondition,
    removeCondition,
    setConditionLogic,
    setActions,
    validate,
    buildApiRequest,
    reset,
    clearError,
  }
}
