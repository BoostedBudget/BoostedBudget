/**
 * AutoCategorizationRuleEditor Component
 *
 * Form for creating and editing auto-categorization rules.
 * Issue #346: Custom Transaction Categorization Rules
 */

import { useState, useCallback, useMemo } from 'react'
import type {
  CategorizationRule,
  CreateRuleRequest,
  UpdateRuleRequest,
  RuleCondition,
  ConditionType,
  ConditionLogic,
  TagBehavior,
  AmountRangeValue,
  DateRangeValue,
  RuleTargetKind,
} from '../types'
import { CONDITION_TYPE_LABELS } from '../types'
import { SearchableSelect } from '../../../shared/components/SearchableSelect'
import type { Account } from '../../accounts/types'
import type { Primary } from '@boostedbudget/api-client/models/primary'
import type { Detailed } from '@boostedbudget/api-client/models/detailed'
import { useGetPlaidDetailedCategories } from '@boostedbudget/api-client/endpoints/categories/categories'

// Account types available for selection
const ACCOUNT_TYPES = [
  { value: 'depository', label: 'Depository (Checking/Savings)' },
  { value: 'credit', label: 'Credit Card' },
  { value: 'loan', label: 'Loan' },
  { value: 'investment', label: 'Investment' },
  { value: 'other', label: 'Other' },
]

// Payment channels available for selection
const PAYMENT_CHANNELS = [
  { value: 'online', label: 'Online' },
  { value: 'in store', label: 'In Store' },
  { value: 'other', label: 'Other' },
]

/**
 * Determines the input type for a given condition type
 */
function getInputTypeForCondition(conditionType: ConditionType): string {
  switch (conditionType) {
    case 'amount_greater_than':
    case 'amount_less_than':
    case 'amount_equals':
      return 'number'
    case 'amount_range':
      return 'amount_range'
    case 'date_range':
      return 'date_range'
    case 'existing_pfc_primary':
      return 'pfc_primary_select'
    case 'existing_pfc_detailed':
      return 'pfc_detailed_select'
    case 'account_id':
      return 'account_select'
    case 'account_type':
      return 'account_type_select'
    case 'payment_channel':
      return 'payment_channel_select'
    default:
      return 'text'
  }
}

/**
 * Validates a single condition and returns an error message if invalid
 */
function validateCondition(condition: RuleCondition): string | null {
  const { type, value } = condition

  // Required value check (except for some range types that allow partial values)
  if (type !== 'date_range' && type !== 'amount_range') {
    if (value === undefined || value === null || value === '') {
      return 'Value is required'
    }
  }

  // Amount validation
  if (['amount_greater_than', 'amount_less_than', 'amount_equals'].includes(type)) {
    const numValue = typeof value === 'string' ? parseFloat(value) : value
    if (typeof numValue !== 'number' || isNaN(numValue)) {
      return 'Must be a valid number'
    }
  }

  // Amount range validation
  if (type === 'amount_range') {
    const rangeValue = value as AmountRangeValue
    if (!rangeValue || (rangeValue.min === undefined && rangeValue.max === undefined)) {
      return 'At least one value is required'
    }
    if (rangeValue.min !== undefined && rangeValue.max !== undefined) {
      if (isNaN(rangeValue.min) || isNaN(rangeValue.max)) {
        return 'Min and max must be valid numbers'
      }
      if (rangeValue.min > rangeValue.max) {
        return 'Min must be less than or equal to max'
      }
    }
  }

  // Date range validation
  if (type === 'date_range') {
    const rangeValue = value as DateRangeValue
    if (!rangeValue || (!rangeValue.start && !rangeValue.end)) {
      return 'At least one date is required'
    }
    if (rangeValue.start && rangeValue.end) {
      const startDate = new Date(rangeValue.start)
      const endDate = new Date(rangeValue.end)
      if (startDate > endDate) {
        return 'Start date must be before or equal to end date'
      }
    }
  }

  // Account type validation (must be array)
  if (type === 'account_type') {
    if (!Array.isArray(value) || value.length === 0) {
      return 'At least one account type must be selected'
    }
  }

  return null
}

export interface AutoCategorizationRuleEditorProps {
  mode: 'create' | 'edit'
  rule?: CategorizationRule
  onSave: (data: CreateRuleRequest | UpdateRuleRequest) => Promise<void>
  onCancel: () => void
  /** Legacy prop kept for parent compatibility; PFC v2 picker uses `plaidPrimaries` instead. */
  categories?: string[][]
  plaidPrimaries?: Primary[]
  accounts?: Account[] // Optional accounts list for account_id dropdown
  tags?: { id: string; name: string }[] // User tags for target tags autocomplete
}

interface FormErrors {
  name?: string
  conditions?: string
  target?: string
}

interface ConditionErrors {
  [index: number]: string | null
}

const DEFAULT_CONDITION: RuleCondition = {
  type: 'merchant_contains',
  value: '',
}

export function AutoCategorizationRuleEditor({ mode, rule, onSave, onCancel, plaidPrimaries = [], accounts = [], tags = [] }: AutoCategorizationRuleEditorProps) {
  // Form state - basic info
  const [name, setName] = useState(rule?.name || '')
  const [description, setDescription] = useState(rule?.description || '')
  const [priority, setPriority] = useState(rule?.priority || 10)

  // Form state - conditions
  const [conditions, setConditions] = useState<RuleCondition[]>(rule?.conditions || [])
  const [conditionLogic, setConditionLogic] = useState<ConditionLogic>(rule?.condition_logic || 'AND')

  // Form state - target (PFC v2 discriminated)
  // Note: targetCustomCategoryId is hydrated but read-only pending deferred custom-target picker
  const [targetKind, setTargetKind] = useState<RuleTargetKind | null>(rule?.target_kind ?? null)
  const [targetPfcPrimary, setTargetPfcPrimary] = useState<string | null>(rule?.target_pfc_primary ?? null)
  const [targetPfcDetailed, setTargetPfcDetailed] = useState<string | null>(rule?.target_pfc_detailed ?? null)
  const [targetCustomCategoryId] = useState<string | null>(rule?.target_custom_category_id ?? null)

  // Form state - tags
  const [targetTags, setTargetTags] = useState<string[]>(rule?.target_tags || [])
  const [tagBehavior, setTagBehavior] = useState<TagBehavior>(rule?.tag_behavior || 'add')
  const [newTag, setNewTag] = useState('')

  // UI state
  const [saving, setSaving] = useState(false)
  const [errors, setErrors] = useState<FormErrors>({})
  const [conditionErrors, setConditionErrors] = useState<ConditionErrors>({})
  const [pfcDetailedPrimarySelections, setPfcDetailedPrimarySelections] = useState<Record<number, string>>({})

  // Lazy-load detailed categories for pfc_detailed conditions or pfc-kind targets
  const needsPlaidDetailed = useMemo(
    () => conditions.some(c => c.type === 'existing_pfc_detailed') || targetKind === 'pfc',
    [conditions, targetKind]
  )
  const { data: plaidDetailedData } = useGetPlaidDetailedCategories({ query: { enabled: needsPlaidDetailed } })
  const plaidDetailed = useMemo(
    () => (plaidDetailedData as unknown as Detailed[]) ?? [],
    [plaidDetailedData]
  )

  // PFC v2 (#1073): target picker option set.
  // - PFC primaries (display label, value = enum)
  // - Synthetic "Uncategorized" entry → targetKind='clear'
  // Custom-category target picker is deferred (requires fetching CustomCategory[] with ids).
  const CLEAR_VALUE = '__CLEAR__'
  const CLEAR_LABEL = 'Uncategorized (clear override)'

  const targetPrimaryOptions = useMemo(() => {
    const pfcOptions = plaidPrimaries
      .map(p => ({ value: p.enum, label: p.display }))
      .sort((a, b) => a.label.localeCompare(b.label))
    return [{ value: CLEAR_VALUE, label: CLEAR_LABEL }, ...pfcOptions]
  }, [plaidPrimaries])

  // Detailed children of the currently selected primary (cascading sub-picker)
  const detailedChildrenOptions = useMemo(() => {
    if (targetKind !== 'pfc' || !targetPfcPrimary) return []
    return plaidDetailed
      .filter(d => d.primary_enum === targetPfcPrimary)
      .map(d => ({ value: d.enum, label: d.display }))
      .sort((a, b) => a.label.localeCompare(b.label))
  }, [targetKind, targetPfcPrimary, plaidDetailed])

  // Display value for the primary combobox: maps targetKind/targetPfcPrimary back to option.value.
  const targetPrimaryComboboxValue = useMemo(() => {
    if (targetKind === 'clear') return CLEAR_VALUE
    if (targetKind === 'pfc' && targetPfcPrimary) return targetPfcPrimary
    return null
  }, [targetKind, targetPfcPrimary])

  // Validate a single condition and update error state
  const validateSingleCondition = useCallback((index: number, condition: RuleCondition): string | null => {
    const error = validateCondition(condition)
    setConditionErrors(prev => ({ ...prev, [index]: error }))
    return error
  }, [])

  // Validation
  const validate = useCallback((): boolean => {
    const newErrors: FormErrors = {}
    const newConditionErrors: ConditionErrors = {}

    if (!name.trim()) {
      newErrors.name = 'Rule name is required'
    }

    if (conditions.length === 0) {
      newErrors.conditions = 'At least one condition is required'
    } else {
      // Validate each condition
      let hasConditionError = false
      conditions.forEach((condition, index) => {
        const error = validateCondition(condition)
        newConditionErrors[index] = error
        if (error) {
          hasConditionError = true
        }
      })
      if (hasConditionError) {
        newErrors.conditions = 'Please fix condition errors'
      }
    }

    // PFC v2: targetKind discriminator — null means no category action.
    // 'pfc' requires a primary; 'custom' requires a custom_category_id (currently
    // only hydrated on edit; picker support is deferred); 'clear' requires no further fields.
    if (targetKind === null && targetTags.length === 0) {
      newErrors.target = 'Target category or tags required'
    }
    if (targetKind === 'pfc' && !targetPfcPrimary) {
      newErrors.target = 'PFC primary is required for a PFC target'
    }
    if (targetKind === 'custom' && !targetCustomCategoryId) {
      newErrors.target = 'Custom category id is required for a custom target'
    }

    setErrors(newErrors)
    setConditionErrors(newConditionErrors)
    return Object.keys(newErrors).length === 0
  }, [name, conditions, targetKind, targetPfcPrimary, targetCustomCategoryId, targetTags])

  // Handlers
  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()

    if (!validate()) {
      return
    }

    setSaving(true)
    try {
      const data: CreateRuleRequest | UpdateRuleRequest = {
        name: name.trim(),
        description: description.trim() || undefined,
        priority,
        conditions,
        condition_logic: conditionLogic,
        // PFC v2: discriminated target. undefined = no category action.
        // Custom-target rules round-trip the hydrated custom_category_id even though the
        // picker UI for it is not yet exposed (#1073 Phase 2 scope deferral).
        target_kind: targetKind ?? undefined,
        target_pfc_primary: targetKind === 'pfc' ? (targetPfcPrimary ?? undefined) : undefined,
        target_pfc_detailed: targetKind === 'pfc' ? (targetPfcDetailed ?? undefined) : undefined,
        target_custom_category_id: targetKind === 'custom' ? (targetCustomCategoryId ?? undefined) : undefined,
        target_tags: targetTags.length > 0 ? targetTags : undefined,
        tag_behavior: tagBehavior,
      }
      await onSave(data)
    } finally {
      setSaving(false)
    }
  }

  // Get default value for a condition type
  const getDefaultValueForType = (type: ConditionType): RuleCondition['value'] => {
    switch (type) {
      case 'amount_range':
        return { min: 0, max: 100 } as AmountRangeValue
      case 'date_range':
        return { start: '', end: '' } as DateRangeValue
      case 'account_type':
        return [] as string[]
      case 'amount_greater_than':
      case 'amount_less_than':
      case 'amount_equals':
        return 0
      default:
        return ''
    }
  }

  const addCondition = () => {
    setConditions([...conditions, { ...DEFAULT_CONDITION }])
  }

  const removeCondition = (index: number) => {
    setConditions(conditions.filter((_, i) => i !== index))
    // Remove error for this condition and shift errors for subsequent conditions
    setConditionErrors(prev => {
      const newErrors: ConditionErrors = {}
      Object.keys(prev).forEach(key => {
        const keyNum = parseInt(key, 10)
        if (keyNum < index) {
          newErrors[keyNum] = prev[keyNum]
        } else if (keyNum > index) {
          newErrors[keyNum - 1] = prev[keyNum]
        }
      })
      return newErrors
    })
  }

  const updateCondition = (index: number, updates: Partial<RuleCondition>) => {
    setConditions(conditions.map((c, i) => {
      if (i !== index) return c

      // If type changed, reset value to appropriate default
      if (updates.type && updates.type !== c.type) {
        return {
          ...c,
          ...updates,
          value: getDefaultValueForType(updates.type)
        }
      }

      return { ...c, ...updates }
    }))
    // Clear error for this condition on change
    setConditionErrors(prev => ({ ...prev, [index]: null }))
  }

  // Update amount range value
  const updateAmountRangeValue = (index: number, field: 'min' | 'max', value: string) => {
    const numValue = value === '' ? undefined : parseFloat(value)
    const condition = conditions[index]
    const currentValue = (condition.value as AmountRangeValue) || { min: undefined, max: undefined }
    updateCondition(index, {
      value: { ...currentValue, [field]: numValue } as AmountRangeValue
    })
  }

  // Update date range value
  const updateDateRangeValue = (index: number, field: 'start' | 'end', value: string) => {
    const condition = conditions[index]
    const currentValue = (condition.value as DateRangeValue) || { start: '', end: '' }
    updateCondition(index, {
      value: { ...currentValue, [field]: value || undefined } as DateRangeValue
    })
  }

  // Toggle account type selection
  const toggleAccountType = (index: number, accountType: string) => {
    const condition = conditions[index]
    const currentTypes = (Array.isArray(condition.value) ? condition.value : []) as string[]
    const newTypes = currentTypes.includes(accountType)
      ? currentTypes.filter(t => t !== accountType)
      : [...currentTypes, accountType]
    updateCondition(index, { value: newTypes })
  }

  // PFC v2 (#1073): map combobox selection back to discriminated state.
  const handleTargetPrimaryChange = (value: string) => {
    if (value === '' || value === null) {
      // No selection — clear all target fields (rule has no category action).
      setTargetKind(null)
      setTargetPfcPrimary(null)
      setTargetPfcDetailed(null)
    } else if (value === CLEAR_VALUE) {
      // User explicitly wants the rule to clear any existing override.
      setTargetKind('clear')
      setTargetPfcPrimary(null)
      setTargetPfcDetailed(null)
    } else {
      // PFC primary enum selected — switch detailed back to null (forces user to re-pick).
      setTargetKind('pfc')
      setTargetPfcPrimary(value)
      setTargetPfcDetailed(null)
    }
  }

  const addTag = () => {
    const tag = newTag.trim()
    if (tag && !targetTags.includes(tag)) {
      setTargetTags([...targetTags, tag])
      setNewTag('')
    }
  }

  const removeTag = (tag: string) => {
    setTargetTags(targetTags.filter((t) => t !== tag))
  }

  // Render the appropriate input for a condition based on its type
  const renderConditionValueInput = (condition: RuleCondition, index: number) => {
    const inputType = getInputTypeForCondition(condition.type)
    const inputCls = 'flex-1 min-w-[100px] px-3 py-2 text-sm border border-input rounded bg-background text-foreground transition-colors focus:outline-none focus:border-primary disabled:bg-muted disabled:opacity-70'
    const numberInputCls = 'max-w-[120px] px-3 py-2 text-sm border border-input rounded bg-background text-foreground transition-colors focus:outline-none focus:border-primary disabled:bg-muted disabled:opacity-70'
    const dateInputCls = 'max-w-[150px] px-3 py-2 text-sm border border-input rounded bg-background text-foreground transition-colors focus:outline-none focus:border-primary disabled:bg-muted disabled:opacity-70'
    const selectCls = 'flex-shrink-0 px-3 py-2 text-sm border border-input rounded bg-background text-foreground cursor-pointer min-w-[150px] focus:outline-none focus:border-primary disabled:bg-muted disabled:opacity-70 disabled:cursor-not-allowed'

    switch (inputType) {
      case 'number':
        return (
          <input
            type="number"
            step="0.01"
            className={numberInputCls}
            value={condition.value === '' ? '' : String(condition.value)}
            onChange={(e) => {
              const val = e.target.value === '' ? '' : parseFloat(e.target.value)
              updateCondition(index, { value: val })
            }}
            onBlur={() => validateSingleCondition(index, conditions[index])}
            disabled={saving}
            placeholder="$0.00 (supports decimals)"
            aria-label="Condition value"
          />
        )

      case 'amount_range': {
        const rangeValue = (condition.value as AmountRangeValue) || { min: undefined, max: undefined }
        return (
          <div className="flex items-center gap-2 flex-1">
            <input
              type="number"
              step="0.01"
              className={numberInputCls}
              value={rangeValue.min ?? ''}
              onChange={(e) => updateAmountRangeValue(index, 'min', e.target.value)}
              onBlur={() => validateSingleCondition(index, conditions[index])}
              disabled={saving}
              placeholder="$0.00"
              aria-label="Minimum amount"
            />
            <span className="text-sm text-muted-foreground">to</span>
            <input
              type="number"
              step="0.01"
              className={numberInputCls}
              value={rangeValue.max ?? ''}
              onChange={(e) => updateAmountRangeValue(index, 'max', e.target.value)}
              onBlur={() => validateSingleCondition(index, conditions[index])}
              disabled={saving}
              placeholder="$0.00"
              aria-label="Maximum amount"
            />
          </div>
        )
      }

      case 'date_range': {
        const dateValue = (condition.value as DateRangeValue) || { start: '', end: '' }
        return (
          <div className="flex items-center gap-2 flex-1">
            <input
              type="date"
              className={dateInputCls}
              value={dateValue.start || ''}
              onChange={(e) => updateDateRangeValue(index, 'start', e.target.value)}
              onBlur={() => validateSingleCondition(index, conditions[index])}
              disabled={saving}
              aria-label="Start date"
            />
            <span className="text-sm text-muted-foreground">to</span>
            <input
              type="date"
              className={dateInputCls}
              value={dateValue.end || ''}
              onChange={(e) => updateDateRangeValue(index, 'end', e.target.value)}
              onBlur={() => validateSingleCondition(index, conditions[index])}
              disabled={saving}
              aria-label="End date"
            />
          </div>
        )
      }

      case 'pfc_primary_select':
        // existing_pfc_primary: display shown, raw PFC enum stored
        return (
          <SearchableSelect
            options={plaidPrimaries.map(p => ({ value: p.enum, label: p.display }))}
            value={String(condition.value || '') || null}
            onChange={(value) => updateCondition(index, { value: value || '' })}
            placeholder="Search PFC primaries..."
            disabled={saving}
            aria-label="Select PFC primary"
            allowClear
            clearLabel="Select a primary"
          />
        )

      case 'pfc_detailed_select': {
        // existing_pfc_detailed: two-stage cascading dropdown (primary → detailed children)
        const detailedValue = String(condition.value || '')
        // Derive selected primary: from explicit per-condition selection, OR from cached row on edit
        const selectedPrimary = pfcDetailedPrimarySelections[index] ??
          plaidDetailed.find(d => d.enum === detailedValue)?.primary_enum ?? ''

        // Build stage-1: distinct primaries that have at least one detailed child
        const primarysWithChildren = Array.from(new Map(
          plaidDetailed.map(d => [d.primary_enum, { value: d.primary_enum, label: d.primary_display }])
        ).values()).sort((a, b) => a.label.localeCompare(b.label))

        // Build stage-2: detailed children of the selected primary
        const detailedChildren = plaidDetailed
          .filter(d => d.primary_enum === selectedPrimary)
          .map(d => ({ value: d.enum, label: d.display }))
          .sort((a, b) => a.label.localeCompare(b.label))

        return (
          <div className="flex flex-col gap-2 flex-1">
            <SearchableSelect
              options={primarysWithChildren}
              value={selectedPrimary || null}
              onChange={(value) => {
                setPfcDetailedPrimarySelections(prev => ({ ...prev, [index]: value || '' }))
                // Clear the detailed value when primary changes
                updateCondition(index, { value: '' })
              }}
              placeholder="Select primary category..."
              disabled={saving || plaidDetailed.length === 0}
              aria-label="Select PFC primary for detailed"
              allowClear
              clearLabel="Select a primary"
            />
            {selectedPrimary && (
              <SearchableSelect
                options={detailedChildren}
                value={detailedValue || null}
                onChange={(value) => updateCondition(index, { value: value || '' })}
                placeholder="Select detailed category..."
                disabled={saving}
                aria-label="Select PFC detailed"
                allowClear
                clearLabel="Select a detailed category"
              />
            )}
          </div>
        )
      }

      case 'account_select': {
        const accountOptions = accounts.map(acc => ({
          value: acc.user_account_id ?? acc.id,
          label: acc.name,
          sublabel: `${acc.type}${acc.mask ? ` ••••${acc.mask}` : ''}`
        }))
        const rawValue = String(condition.value || '') || null
        // Edit-mode hydration: if a legacy rule stored a raw account.id (not user_account_id),
        // resolve it to the stable user_account_id so the dropdown renders correctly.
        // Migration 000072 backfills existing rules, but this is a belt-and-suspenders guard.
        const resolvedValue = rawValue !== null && !accountOptions.some(o => o.value === rawValue)
          ? (accounts.find(acc => acc.id === rawValue)?.user_account_id ?? rawValue)
          : rawValue
        return (
          <SearchableSelect
            options={accountOptions}
            value={resolvedValue}
            onChange={(value) => updateCondition(index, { value: value || '' })}
            placeholder="Search accounts..."
            disabled={saving}
            aria-label="Select account"
            allowClear
            clearLabel="Select an account"
          />
        )
      }

      case 'account_type_select': {
        const selectedTypes = (Array.isArray(condition.value) ? condition.value : []) as string[]
        return (
          <div className="flex flex-wrap gap-3 p-2 bg-muted rounded flex-1 min-w-[200px]">
            <span className="w-full text-xs font-semibold text-muted-foreground uppercase tracking-wide mb-1">Match ANY of:</span>
            {ACCOUNT_TYPES.map((type) => (
              <label key={type.value} className="flex items-center gap-1.5 text-[0.8125rem] text-foreground cursor-pointer whitespace-nowrap">
                <input
                  type="checkbox"
                  checked={selectedTypes.includes(type.value)}
                  onChange={() => toggleAccountType(index, type.value)}
                  disabled={saving}
                  className="cursor-pointer"
                />
                {type.label}
              </label>
            ))}
          </div>
        )
      }

      case 'payment_channel_select':
        return (
          <select
            className={selectCls}
            value={String(condition.value || '')}
            onChange={(e) => updateCondition(index, { value: e.target.value })}
            disabled={saving}
            aria-label="Select payment channel"
          >
            <option value="">Select payment channel</option>
            {PAYMENT_CHANNELS.map((channel) => (
              <option key={channel.value} value={channel.value}>
                {channel.label}
              </option>
            ))}
          </select>
        )

      default:
        return (
          <input
            type="text"
            className={inputCls}
            value={String(condition.value)}
            onChange={(e) => updateCondition(index, { value: e.target.value })}
            onBlur={() => validateSingleCondition(index, conditions[index])}
            disabled={saving}
            placeholder="Enter value"
            aria-label="Condition value"
          />
        )
    }
  }

  const inputClass = 'w-full px-3 py-2 text-sm border border-input rounded bg-background text-foreground transition-colors focus:outline-none focus:border-primary disabled:bg-muted disabled:opacity-70'
  const selectClass = 'px-3 py-2 text-sm border border-input rounded bg-background text-foreground cursor-pointer min-w-[150px] focus:outline-none focus:border-primary disabled:bg-muted disabled:opacity-70 disabled:cursor-not-allowed'
  const labelClass = 'block mb-1.5 text-[0.8125rem] font-medium text-muted-foreground'
  const errorClass = 'block mt-1 text-xs text-destructive'
  const btnBaseClass = 'px-5 py-2 text-sm font-medium border border-transparent rounded cursor-pointer transition-colors disabled:opacity-60 disabled:cursor-not-allowed'

  return (
    <form className="bg-card border border-border rounded-lg p-6 max-w-[600px]" onSubmit={handleSubmit}>
      <h2 className="m-0 mb-6 text-xl font-semibold text-foreground">
        {mode === 'create' ? 'Create Rule' : 'Edit Rule'}
      </h2>

      {/* Basic Info */}
      <div className="mb-6 pb-6 border-b border-border">
        <div className="mb-4">
          <label htmlFor="rule-name" className={labelClass}>
            Rule Name
          </label>
          <input
            id="rule-name"
            type="text"
            className={inputClass}
            value={name}
            onChange={(e) => setName(e.target.value)}
            disabled={saving}
            placeholder="e.g., Coffee Shops"
          />
          {errors.name && <span className={errorClass}>{errors.name}</span>}
        </div>

        <div className="mb-4">
          <label htmlFor="rule-description" className={labelClass}>
            Description
          </label>
          <input
            id="rule-description"
            type="text"
            className={inputClass}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            disabled={saving}
            placeholder="Optional description"
          />
        </div>

        <div className="mb-4 max-w-[120px]">
          <label htmlFor="rule-priority" className={labelClass}>
            Priority
          </label>
          <input
            id="rule-priority"
            type="number"
            className={inputClass}
            value={priority}
            onChange={(e) => setPriority(parseInt(e.target.value, 10) || 0)}
            disabled={saving}
            min={1}
            max={100}
          />
        </div>
      </div>

      {/* Conditions */}
      <div className="mb-6 pb-6 border-b border-border">
        <div className="flex items-center justify-between mb-4">
          <h3 className="m-0 text-[0.9375rem] font-semibold text-foreground">Conditions</h3>
          {conditions.length > 1 && (
            <div className="flex border border-border rounded overflow-hidden">
              <button
                type="button"
                className={`px-3 py-1 text-xs font-semibold border-none cursor-pointer transition-colors disabled:opacity-70 disabled:cursor-not-allowed ${conditionLogic === 'AND' ? 'active bg-primary text-primary-foreground' : 'bg-background text-muted-foreground hover:bg-muted'}`}
                onClick={() => setConditionLogic('AND')}
                disabled={saving}
              >
                AND
              </button>
              <button
                type="button"
                className={`px-3 py-1 text-xs font-semibold border-none cursor-pointer transition-colors disabled:opacity-70 disabled:cursor-not-allowed ${conditionLogic === 'OR' ? 'active bg-primary text-primary-foreground' : 'bg-background text-muted-foreground hover:bg-muted'}`}
                onClick={() => setConditionLogic('OR')}
                disabled={saving}
              >
                OR
              </button>
            </div>
          )}
        </div>

        {errors.conditions && (
          <span className={errorClass}>{errors.conditions}</span>
        )}

        <div className="flex flex-col gap-3 mb-3">
          {conditions.map((condition, index) => (
            <div key={index} className="flex flex-col gap-1">
              <div className={`flex items-start gap-2 flex-wrap ${conditionErrors[index] ? 'border-l-2 border-destructive pl-2 -ml-2' : ''}`}>
                <label htmlFor={`condition-type-${index}`} className="sr-only">
                  Condition type
                </label>
                <select
                  id={`condition-type-${index}`}
                  aria-label="Condition type"
                  className={selectClass}
                  value={condition.type}
                  onChange={(e) => updateCondition(index, { type: e.target.value as ConditionType })}
                  disabled={saving}
                >
                  {Object.entries(CONDITION_TYPE_LABELS).map(([type, label]) => (
                    <option key={type} value={type}>
                      {label}
                    </option>
                  ))}
                </select>
                {renderConditionValueInput(condition, index)}
                <button
                  type="button"
                  className="px-2 py-1 text-xl leading-none border-none bg-transparent text-destructive cursor-pointer opacity-70 hover:opacity-100 transition-opacity disabled:cursor-not-allowed"
                  onClick={() => removeCondition(index)}
                  disabled={saving}
                  aria-label="Remove condition"
                >
                  &times;
                </button>
              </div>
              {conditionErrors[index] && (
                <span className="text-xs text-destructive pl-2">
                  {conditionErrors[index]}
                </span>
              )}
            </div>
          ))}
        </div>

        <button
          type="button"
          className="px-3 py-1.5 text-[0.8125rem] font-medium border border-dashed border-border rounded bg-transparent text-muted-foreground cursor-pointer hover:border-primary hover:text-primary transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
          onClick={addCondition}
          disabled={saving}
        >
          Add Condition
        </button>
      </div>

      {/* Target Category */}
      <div className="mb-6 pb-6 border-b border-border last:border-b-0 last:pb-0">
        <h3 className="m-0 mb-4 text-[0.9375rem] font-semibold text-foreground">Actions</h3>
        {errors.target && <span className={errorClass}>{errors.target}</span>}

        <div className="mb-4">
          <label htmlFor="target-category" className={labelClass}>
            Target Category
          </label>
          {targetKind === 'custom' ? (
            // PFC v2 (#1073): the custom-target picker UI is deferred. Existing rules with a
            // custom target are shown read-only so users can edit other fields without
            // accidentally clearing the target. Editing the custom target itself is unsupported
            // in this version — track in a follow-up.
            <div
              className="px-3 py-2 text-sm border border-input rounded bg-muted text-muted-foreground"
              data-testid="target-custom-readonly"
              aria-label="Custom category target (read-only)"
            >
              Custom category: <code className="font-mono text-xs">{rule?.target_custom_category_display ?? targetCustomCategoryId}</code>
              <span className="block text-xs mt-1 text-muted-foreground/80">
                Editing the custom-category target is not yet supported in this UI.
              </span>
            </div>
          ) : (
            <>
              {/* PFC v2 (#1073): primary picker + optional detailed cascade. */}
              <SearchableSelect
                options={targetPrimaryOptions}
                value={targetPrimaryComboboxValue}
                onChange={(value) => handleTargetPrimaryChange(value || '')}
                placeholder="Search PFC primaries..."
                disabled={saving}
                aria-label="Target category"
                allowClear
                clearLabel="No category change"
              />
            </>
          )}
          {targetKind === 'pfc' && targetPfcPrimary && (
            <div className="mt-2">
              <label htmlFor="target-detailed" className={labelClass}>
                Detailed (optional)
              </label>
              <SearchableSelect
                options={detailedChildrenOptions}
                value={targetPfcDetailed}
                onChange={(value) => setTargetPfcDetailed(value || null)}
                placeholder="Search detailed categories..."
                disabled={saving || detailedChildrenOptions.length === 0}
                aria-label="Target detailed category"
                allowClear
                clearLabel="No detailed refinement"
              />
            </div>
          )}
        </div>

        {/* Tags */}
        <div className="mb-4">
          <label className={labelClass}>Target Tags</label>
          <div className="flex flex-wrap gap-1.5 mb-2">
            {targetTags.map((tag) => (
              <span key={tag} className="inline-flex items-center gap-1 px-2 py-1 text-[0.8125rem] bg-info/10 text-info rounded">
                {tag}
                <button
                  type="button"
                  className="p-0 text-base leading-none border-none bg-transparent text-current cursor-pointer opacity-70 hover:opacity-100"
                  onClick={() => removeTag(tag)}
                  disabled={saving}
                  aria-label={`Remove tag ${tag}`}
                >
                  &times;
                </button>
              </span>
            ))}
          </div>
          <div className="flex gap-2">
            {tags.length > 0 ? (
              <SearchableSelect
                options={tags
                  .filter(tag => !targetTags.includes(tag.name))
                  .map(tag => ({ value: tag.name, label: tag.name }))}
                value={null}
                onChange={(tagName) => {
                  if (tagName && !targetTags.includes(tagName)) {
                    setTargetTags([...targetTags, tagName])
                  }
                }}
                placeholder="Search tags..."
                disabled={saving}
                aria-label="Select tag"
              />
            ) : (
              <>
                <input
                  type="text"
                  className={`${inputClass} flex-1`}
                  value={newTag}
                  onChange={(e) => setNewTag(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      e.preventDefault()
                      addTag()
                    }
                  }}
                  disabled={saving}
                  placeholder="Add tag"
                />
                <button
                  type="button"
                  className="px-3 py-1.5 text-[0.8125rem] font-medium border border-dashed border-border rounded bg-transparent text-muted-foreground cursor-pointer hover:border-primary hover:text-primary transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                  onClick={addTag}
                  disabled={saving || !newTag.trim()}
                >
                  Add Tag
                </button>
              </>
            )}
          </div>
        </div>

        {/* Tag behavior */}
        {targetTags.length > 0 && (
          <div className="mb-4">
            <label className={labelClass}>Tag Behavior</label>
            <div className="flex flex-col gap-2">
              <label className="flex items-center gap-2 text-sm text-foreground cursor-pointer">
                <input
                  type="radio"
                  name="tag-behavior"
                  value="add"
                  checked={tagBehavior === 'add'}
                  onChange={() => setTagBehavior('add')}
                  disabled={saving}
                  className="cursor-pointer"
                />
                Add to existing tags
              </label>
              <label className="flex items-center gap-2 text-sm text-foreground cursor-pointer">
                <input
                  type="radio"
                  name="tag-behavior"
                  value="replace"
                  checked={tagBehavior === 'replace'}
                  onChange={() => setTagBehavior('replace')}
                  disabled={saving}
                  className="cursor-pointer"
                />
                Replace existing tags
              </label>
            </div>
          </div>
        )}
      </div>

      {/* Footer Actions */}
      <div className="flex justify-end gap-3 mt-6 pt-6 border-t border-border">
        <button
          type="button"
          className={`${btnBaseClass} bg-background border-border text-foreground hover:bg-muted`}
          onClick={onCancel}
          disabled={saving}
        >
          Cancel
        </button>
        <button
          type="submit"
          className={`${btnBaseClass} bg-primary text-primary-foreground hover:bg-primary/90`}
          disabled={saving}
        >
          {saving ? 'Saving...' : 'Save'}
        </button>
      </div>
    </form>
  )
}
