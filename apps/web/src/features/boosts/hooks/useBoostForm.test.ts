/**
 * useBoostForm Hook Tests
 * Issue #39: Rule Builder UI
 *
 * Tests the automation rule form state management hook.
 * TDD RED: Writing tests before implementation.
 */

import { describe, it, expect } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { useBoostForm } from './useBoostForm'
import type { BoostType, TriggerFormState, BoostConditionLocal, BoostAction } from '../types'

describe('useBoostForm', () => {
  describe('initialization', () => {
    it('should initialize with empty form state for new rule', () => {
      const { result } = renderHook(() => useBoostForm())

      expect(result.current.formState.name).toBe('')
      expect(result.current.formState.description).toBe('')
      expect(result.current.formState.priority).toBe(10)
      expect(result.current.formState.enabled).toBe(true)
      expect(result.current.formState.boostType).toBeNull()
      expect(result.current.formState.conditions).toEqual([])
      expect(result.current.formState.conditionLogic).toBe('AND')
      expect(result.current.formState.actions).toBeNull()
      expect(result.current.formState.isDirty).toBe(false)
    })

    it('should initialize with default trigger config', () => {
      const { result } = renderHook(() => useBoostForm())

      expect(result.current.formState.triggerConfig.type).toBe('schedule')
      expect(result.current.formState.triggerConfig.cron).toBe('0 9 * * *')
      expect(result.current.formState.triggerConfig.timezone).toBe('America/New_York')
    })

    it('should initialize with provided initial values', () => {
      const initialValues = {
        name: 'Test Rule',
        description: 'Test Description',
        priority: 5,
        boostType: 'budget_alert' as BoostType,
      }

      const { result } = renderHook(() => useBoostForm({ initialValues }))

      expect(result.current.formState.name).toBe('Test Rule')
      expect(result.current.formState.description).toBe('Test Description')
      expect(result.current.formState.priority).toBe(5)
      expect(result.current.formState.boostType).toBe('budget_alert')
    })
  })

  describe('setName', () => {
    it('should update name and mark form as dirty', () => {
      const { result } = renderHook(() => useBoostForm())

      act(() => {
        result.current.setName('New Rule Name')
      })

      expect(result.current.formState.name).toBe('New Rule Name')
      expect(result.current.formState.isDirty).toBe(true)
    })

    it('should clear name error when name is set', () => {
      const { result } = renderHook(() => useBoostForm())

      // Trigger validation to get error
      act(() => {
        result.current.validate()
      })
      expect(result.current.errors.name).toBeDefined()

      // Set name should clear error
      act(() => {
        result.current.setName('Valid Name')
      })
      expect(result.current.errors.name).toBeUndefined()
    })
  })

  describe('setDescription', () => {
    it('should update description and mark form as dirty', () => {
      const { result } = renderHook(() => useBoostForm())

      act(() => {
        result.current.setDescription('New Description')
      })

      expect(result.current.formState.description).toBe('New Description')
      expect(result.current.formState.isDirty).toBe(true)
    })
  })

  describe('setPriority', () => {
    it('should update priority', () => {
      const { result } = renderHook(() => useBoostForm())

      act(() => {
        result.current.setPriority(5)
      })

      expect(result.current.formState.priority).toBe(5)
    })
  })

  describe('setEnabled', () => {
    it('should update enabled state', () => {
      const { result } = renderHook(() => useBoostForm())

      act(() => {
        result.current.setEnabled(false)
      })

      expect(result.current.formState.enabled).toBe(false)
    })
  })

  describe('setBoostType', () => {
    it('should update rule type', () => {
      const { result } = renderHook(() => useBoostForm())

      act(() => {
        result.current.setBoostType('transfer_suggestion')
      })

      expect(result.current.formState.boostType).toBe('transfer_suggestion')
      expect(result.current.formState.isDirty).toBe(true)
    })

    it('should reset actions when rule type changes', () => {
      const { result } = renderHook(() => useBoostForm())

      // Set initial action
      act(() => {
        result.current.setBoostType('budget_alert')
        result.current.setActions({
          notification_channels: ['in_app'],
          threshold_type: 'percentage',
          threshold_value: 80,
        })
      })

      // Change rule type
      act(() => {
        result.current.setBoostType('transfer_suggestion')
      })

      expect(result.current.formState.actions).toBeNull()
    })
  })

  describe('setTriggerConfig', () => {
    it('should update trigger config', () => {
      const { result } = renderHook(() => useBoostForm())

      const newConfig: TriggerFormState = {
        type: 'event',
        event: 'budget_updated',
      }

      act(() => {
        result.current.setTriggerConfig(newConfig)
      })

      expect(result.current.formState.triggerConfig.type).toBe('event')
      expect(result.current.formState.triggerConfig.event).toBe('budget_updated')
    })

    it('should merge partial trigger config updates', () => {
      const { result } = renderHook(() => useBoostForm())

      act(() => {
        result.current.setTriggerConfig({ cron: '0 0 * * *' })
      })

      expect(result.current.formState.triggerConfig.type).toBe('schedule')
      expect(result.current.formState.triggerConfig.cron).toBe('0 0 * * *')
      expect(result.current.formState.triggerConfig.timezone).toBe('America/New_York')
    })
  })

  describe('conditions', () => {
    const testCondition: BoostConditionLocal = {
      field: 'budget.percent_used',
      operator: 'gte',
      value: 80,
    }

    it('should add a condition', () => {
      const { result } = renderHook(() => useBoostForm())

      act(() => {
        result.current.addCondition(testCondition)
      })

      expect(result.current.formState.conditions).toHaveLength(1)
      expect(result.current.formState.conditions[0]).toEqual(testCondition)
    })

    it('should update a condition at index', () => {
      const { result } = renderHook(() => useBoostForm())

      act(() => {
        result.current.addCondition(testCondition)
      })

      const updatedCondition: BoostConditionLocal = {
        field: 'budget.percent_used',
        operator: 'gt',
        value: 90,
      }

      act(() => {
        result.current.updateCondition(0, updatedCondition)
      })

      expect(result.current.formState.conditions[0].operator).toBe('gt')
      expect(result.current.formState.conditions[0].value).toBe(90)
    })

    it('should remove a condition at index', () => {
      const { result } = renderHook(() => useBoostForm())

      act(() => {
        result.current.addCondition(testCondition)
        result.current.addCondition({ ...testCondition, value: 90 })
      })

      expect(result.current.formState.conditions).toHaveLength(2)

      act(() => {
        result.current.removeCondition(0)
      })

      expect(result.current.formState.conditions).toHaveLength(1)
      expect(result.current.formState.conditions[0].value).toBe(90)
    })

    it('should set condition logic', () => {
      const { result } = renderHook(() => useBoostForm())

      act(() => {
        result.current.setConditionLogic('OR')
      })

      expect(result.current.formState.conditionLogic).toBe('OR')
    })
  })

  describe('setActions', () => {
    it('should set actions', () => {
      const { result } = renderHook(() => useBoostForm())

      const alertAction: BoostAction = {
        notification_channels: ['in_app', 'email'],
        threshold_type: 'percentage',
        threshold_value: 80,
      }

      act(() => {
        result.current.setBoostType('budget_alert')
        result.current.setActions(alertAction)
      })

      expect(result.current.formState.actions).toEqual(alertAction)
    })
  })

  describe('validation', () => {
    it('should validate name is required', () => {
      const { result } = renderHook(() => useBoostForm())

      let isValid: boolean = true
      act(() => {
        isValid = result.current.validate()
      })

      expect(isValid).toBe(false)
      expect(result.current.errors.name).toBe('Name is required')
    })

    it('should validate rule type is required', () => {
      const { result } = renderHook(() => useBoostForm())

      act(() => {
        result.current.setName('Test Rule')
      })

      let isValid: boolean = true
      act(() => {
        isValid = result.current.validate()
      })

      expect(isValid).toBe(false)
      expect(result.current.errors.boostType).toBe('Boost type is required')
    })

    it('should validate cron is required for schedule trigger', () => {
      const { result } = renderHook(() => useBoostForm())

      act(() => {
        result.current.setName('Test Rule')
        result.current.setBoostType('budget_alert')
        result.current.setTriggerConfig({ type: 'schedule', cron: '' })
      })

      let isValid: boolean = true
      act(() => {
        isValid = result.current.validate()
      })

      expect(isValid).toBe(false)
      expect(result.current.errors.trigger?.cron).toBe('Cron expression is required for scheduled triggers')
    })

    it('should validate event is required for event trigger', () => {
      const { result } = renderHook(() => useBoostForm())

      act(() => {
        result.current.setName('Test Rule')
        result.current.setBoostType('budget_alert')
        result.current.setTriggerConfig({ type: 'event', event: undefined })
      })

      let isValid: boolean = true
      act(() => {
        isValid = result.current.validate()
      })

      expect(isValid).toBe(false)
      expect(result.current.errors.trigger?.event).toBe('Event type is required for event triggers')
    })

    it('should pass validation with valid form data', () => {
      const { result } = renderHook(() => useBoostForm())

      act(() => {
        result.current.setName('Test Rule')
        result.current.setBoostType('budget_alert')
        result.current.setTriggerConfig({ type: 'schedule', cron: '0 9 * * *' })
        result.current.setActions({
          notification_channels: ['in_app'],
          threshold_type: 'percentage',
          threshold_value: 80,
        })
      })

      let isValid: boolean = false
      act(() => {
        isValid = result.current.validate()
      })

      expect(isValid).toBe(true)
      expect(result.current.errors).toEqual({})
    })
  })

  describe('buildApiRequest', () => {
    it('should build API request from form state', () => {
      const { result } = renderHook(() => useBoostForm())

      act(() => {
        result.current.setName('Budget Alert Rule')
        result.current.setDescription('Alert when budget exceeds 80%')
        result.current.setBoostType('budget_alert')
        result.current.setPriority(5)
        result.current.setTriggerConfig({
          type: 'schedule',
          cron: '0 9 * * *',
          timezone: 'America/New_York',
        })
        result.current.addCondition({
          field: 'budget.percent_used',
          operator: 'gte',
          value: 80,
        })
        result.current.setActions({
          notification_channels: ['in_app', 'email'],
          threshold_type: 'percentage',
          threshold_value: 80,
        })
      })

      const request = result.current.buildApiRequest()

      expect(request).not.toBeNull()
      expect(request!.name).toBe('Budget Alert Rule')
      expect(request!.description).toBe('Alert when budget exceeds 80%')
      expect(request!.boost_type).toBe('budget_alert')
      expect(request!.priority).toBe(5)
      expect(request!.trigger_config.type).toBe('schedule')
      expect(request!.trigger_config.cron).toBe('0 9 * * *')
      expect(request!.conditions).toHaveLength(1)
      // condition_logic is undefined when only 1 condition (AND/OR is meaningless)
      expect(request!.condition_logic).toBeUndefined()
      expect(request!.actions).toBeDefined()
    })

    it('should return null if form is invalid', () => {
      const { result } = renderHook(() => useBoostForm())

      const request = result.current.buildApiRequest()

      expect(request).toBeNull()
    })

    it('should preserve budget_id and category_id in condition payload', () => {
      const { result } = renderHook(() => useBoostForm())

      act(() => {
        result.current.setName('Budget Category Rule')
        result.current.setBoostType('budget_alert')
        result.current.addCondition({
          field: 'budget_category.percent_used',
          operator: 'gte',
          value: 80,
          budget_id: 'budget-1',
          category_id: 'cat-1',
        })
        result.current.setActions({
          notification_channels: ['in_app'],
          threshold_type: 'percentage',
          threshold_value: 80,
        })
      })

      const request = result.current.buildApiRequest()

      expect(request).not.toBeNull()
      expect(request!.conditions).toHaveLength(1)
      const condition = request!.conditions![0]
      expect(condition).toMatchObject({
        field: 'budget_category.percent_used',
        budget_id: 'budget-1',
        category_id: 'cat-1',
      })
    })
  })

  describe('reset', () => {
    it('should reset form to initial state', () => {
      const { result } = renderHook(() => useBoostForm())

      act(() => {
        result.current.setName('Test Rule')
        result.current.setBoostType('budget_alert')
        result.current.addCondition({
          field: 'budget.percent_used',
          operator: 'gte',
          value: 80,
        })
      })

      expect(result.current.formState.isDirty).toBe(true)

      act(() => {
        result.current.reset()
      })

      expect(result.current.formState.name).toBe('')
      expect(result.current.formState.boostType).toBeNull()
      expect(result.current.formState.conditions).toEqual([])
      expect(result.current.formState.isDirty).toBe(false)
    })

    it('should reset to provided initial values', () => {
      const initialValues = {
        name: 'Initial Name',
        boostType: 'budget_alert' as BoostType,
      }

      const { result } = renderHook(() => useBoostForm({ initialValues }))

      act(() => {
        result.current.setName('Changed Name')
      })

      expect(result.current.formState.name).toBe('Changed Name')

      act(() => {
        result.current.reset()
      })

      expect(result.current.formState.name).toBe('Initial Name')
      expect(result.current.formState.boostType).toBe('budget_alert')
    })
  })

  describe('clearError', () => {
    it('should clear a specific error', () => {
      const { result } = renderHook(() => useBoostForm())

      act(() => {
        result.current.validate()
      })

      expect(result.current.errors.name).toBeDefined()

      act(() => {
        result.current.clearError('name')
      })

      expect(result.current.errors.name).toBeUndefined()
    })
  })
})
