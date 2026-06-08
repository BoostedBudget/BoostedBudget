/**
 * ConditionBuilder Component
 * Issue #39: Rule Builder UI
 *
 * Component for building dynamic condition rows with field/operator/value.
 * Supports AND/OR logic toggle.
 */

import { useCallback, useEffect } from 'react'
import { Plus, Trash2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import {
  CONDITION_FIELD_OPTIONS,
  CONDITION_FIELDS_BY_EVENT,
  CONDITION_OPERATOR_LABELS,
  type ConditionFieldOption,
} from '../constants'
import type { RuleCondition, ConditionLogic, ConditionOperator } from '../types'
import type { BudgetCategory } from '../../budgets/types'
import { formatBudgetCategoryLabel } from '../../budgets/types'

export interface ConditionBuilderProps {
  /** Current conditions */
  conditions: RuleCondition[]
  /** Current logic (AND/OR) */
  logic: ConditionLogic
  /** Called when conditions change */
  onConditionsChange: (conditions: RuleCondition[]) => void
  /** Called when logic changes */
  onLogicChange: (logic: ConditionLogic) => void
  /** The trigger event name — used to filter which fields are available */
  triggerEvent?: string
  /** Budget categories available for budget_category.* condition fields */
  categories?: BudgetCategory[]
  /** Disable all controls */
  disabled?: boolean
  /** Additional CSS class */
  className?: string
}

// Get default condition for a given field list
function createDefaultCondition(availableFields: ConditionFieldOption[]): RuleCondition {
  const defaultField = availableFields[0] ?? CONDITION_FIELD_OPTIONS[0]
  return {
    field: defaultField.value,
    operator: defaultField.operators[0],
    value: defaultField.valueType === 'number' ? 0 : '',
  }
}

// Get operators for a field from the given field list
function getOperatorsForField(fieldValue: string, fields: ConditionFieldOption[]): ConditionOperator[] {
  const field = fields.find((f) => f.value === fieldValue)
  return field?.operators ?? ['eq']
}

// Get value type for a field from the given field list
function getValueTypeForField(fieldValue: string, fields: ConditionFieldOption[]): ConditionFieldOption['valueType'] {
  const field = fields.find((f) => f.value === fieldValue)
  return field?.valueType ?? 'string'
}

interface ConditionRowProps {
  condition: RuleCondition
  index: number
  availableFields: ConditionFieldOption[]
  categories?: BudgetCategory[]
  onChange: (index: number, condition: RuleCondition) => void
  onRemove: (index: number) => void
  disabled?: boolean
}

const selectClass = 'rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground focus:border-ring focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50'
const inputClass = 'rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground focus:border-ring focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50'

function ConditionRow({ condition, index, availableFields, categories, onChange, onRemove, disabled }: ConditionRowProps) {
  const operators = getOperatorsForField(condition.field, availableFields)
  const valueType = getValueTypeForField(condition.field, availableFields)
  const isCategoryField = condition.field.startsWith('budget_category.')

  const handleFieldChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      const newField = e.target.value
      const newOperators = getOperatorsForField(newField, availableFields)
      const newValueType = getValueTypeForField(newField, availableFields)
      const isCat = newField.startsWith('budget_category.')

      onChange(index, {
        field: newField,
        operator: newOperators[0],
        value: newValueType === 'number' ? 0 : '',
        category_id: isCat ? condition.category_id : undefined,
      })
    },
    [index, availableFields, condition.category_id, onChange]
  )

  const handleOperatorChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      onChange(index, {
        ...condition,
        operator: e.target.value as ConditionOperator,
      })
    },
    [index, condition, onChange]
  )

  const handleValueChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const value = valueType === 'number' ? parseFloat(e.target.value) || 0 : e.target.value
      onChange(index, {
        ...condition,
        value,
      })
    },
    [index, condition, valueType, onChange]
  )

  const handleCategoryChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      onChange(index, {
        ...condition,
        category_id: e.target.value || undefined,
      })
    },
    [index, condition, onChange]
  )

  // Group fields by category for optgroup
  const groupedFields = availableFields.reduce(
    (acc, field) => {
      if (!acc[field.category]) {
        acc[field.category] = []
      }
      acc[field.category].push(field)
      return acc
    },
    {} as Record<string, ConditionFieldOption[]>
  )

  return (
    <div className="flex flex-col gap-3 rounded-lg border border-border bg-card p-4">
      <div className="grid grid-cols-[1fr_1fr_1fr_auto] gap-3 max-md:grid-cols-[1fr_1fr] [&_.condition-value]:max-md:col-span-2">
        {/* Field selector */}
        <div className="flex flex-col gap-1">
          <label htmlFor={`condition-field-${index}`} className="text-xs font-medium text-muted-foreground">
            Field
          </label>
          <select
            id={`condition-field-${index}`}
            value={condition.field}
            onChange={handleFieldChange}
            disabled={disabled}
            className={selectClass}
          >
            {Object.entries(groupedFields).map(([category, fields]) => (
              <optgroup key={category} label={category}>
                {fields.map((field) => (
                  <option key={field.value} value={field.value}>
                    {field.label}
                  </option>
                ))}
              </optgroup>
            ))}
          </select>
        </div>

        {/* Operator selector */}
        <div className="flex flex-col gap-1">
          <label htmlFor={`condition-operator-${index}`} className="text-xs font-medium text-muted-foreground">
            Operator
          </label>
          <select
            id={`condition-operator-${index}`}
            value={condition.operator}
            onChange={handleOperatorChange}
            disabled={disabled}
            className={selectClass}
          >
            {operators.map((op) => (
              <option key={op} value={op}>
                {CONDITION_OPERATOR_LABELS[op]}
              </option>
            ))}
          </select>
        </div>

        {/* Value input */}
        <div className="condition-value flex flex-col gap-1">
          <label htmlFor={`condition-value-${index}`} className="text-xs font-medium text-muted-foreground">
            Value
          </label>
          <input
            id={`condition-value-${index}`}
            type={valueType === 'number' ? 'number' : 'text'}
            value={condition.value as string | number}
            onChange={handleValueChange}
            disabled={disabled}
            className={inputClass}
            placeholder={valueType === 'number' ? '0' : 'Enter value...'}
          />
        </div>

        {/* Remove button */}
        <button
          type="button"
          onClick={() => onRemove(index)}
          disabled={disabled}
          className="flex h-9 w-9 items-center justify-center self-end rounded-md border-none bg-transparent text-muted-foreground transition-all hover:bg-red-50 hover:text-destructive disabled:cursor-not-allowed disabled:opacity-50 dark:hover:bg-red-900 dark:hover:text-red-400"
          aria-label="Remove condition"
          title="Remove condition"
        >
          <Trash2 className="h-4 w-4" aria-hidden="true" />
        </button>
      </div>

      {/* Category picker — only shown for budget_category.* fields */}
      {isCategoryField && (
        <div className="flex flex-col gap-1">
          <label htmlFor={`condition-category-${index}`} className="text-xs font-medium text-muted-foreground">
            Category
          </label>
          <select
            id={`condition-category-${index}`}
            value={condition.category_id ?? ''}
            onChange={handleCategoryChange}
            disabled={disabled}
            className={selectClass}
          >
            <option value="">All Categories</option>
            {(categories ?? []).map((category) => (
              <option key={category.id} value={category.id}>
                {formatBudgetCategoryLabel(category)}
              </option>
            ))}
          </select>
        </div>
      )}
    </div>
  )
}

/**
 * ConditionBuilder component for building rule conditions.
 *
 * @example
 * ```tsx
 * function RuleForm() {
 *   const [conditions, setConditions] = useState<RuleCondition[]>([])
 *   const [logic, setLogic] = useState<ConditionLogic>('AND')
 *
 *   return (
 *     <ConditionBuilder
 *       conditions={conditions}
 *       logic={logic}
 *       onConditionsChange={setConditions}
 *       onLogicChange={setLogic}
 *     />
 *   )
 * }
 * ```
 */
export function ConditionBuilder({
  conditions,
  logic,
  onConditionsChange,
  onLogicChange,
  triggerEvent,
  categories,
  disabled = false,
  className = '',
}: ConditionBuilderProps) {
  // Determine the fields to show based on the trigger event
  const availableFields =
    triggerEvent && CONDITION_FIELDS_BY_EVENT[triggerEvent]
      ? CONDITION_FIELDS_BY_EVENT[triggerEvent]
      : CONDITION_FIELD_OPTIONS

  // Reset conditions whose field is no longer valid when availableFields changes
  useEffect(() => {
    const validFields = new Set(availableFields.map((f) => f.value))
    const updated = conditions.map((c) => {
      if (!validFields.has(c.field)) {
        return { ...c, field: availableFields[0]?.value ?? c.field, category_id: undefined }
      }
      return c
    })
    const changed = updated.some((c, i) => c.field !== conditions[i].field)
    if (changed) {
      onConditionsChange(updated)
    }
  }, [availableFields]) // eslint-disable-line react-hooks/exhaustive-deps

  const handleAdd = useCallback(() => {
    onConditionsChange([...conditions, createDefaultCondition(availableFields)])
  }, [conditions, availableFields, onConditionsChange])

  const handleChange = useCallback(
    (index: number, condition: RuleCondition) => {
      const newConditions = [...conditions]
      newConditions[index] = condition
      onConditionsChange(newConditions)
    },
    [conditions, onConditionsChange]
  )

  const handleRemove = useCallback(
    (index: number) => {
      onConditionsChange(conditions.filter((_, i) => i !== index))
    },
    [conditions, onConditionsChange]
  )

  const handleLogicChange = useCallback(
    (newLogic: ConditionLogic) => {
      if (newLogic !== logic) {
        onLogicChange(newLogic)
      }
    },
    [logic, onLogicChange]
  )

  return (
    <div className={cn('flex flex-col gap-4', className)}>
      {/* Logic toggle (only show when multiple conditions) */}
      {conditions.length > 1 && (
        <div className="flex items-center gap-2 rounded-lg bg-muted px-4 py-3 dark:bg-muted/50">
          <span className="text-sm text-muted-foreground">Match</span>
          <div className="flex overflow-hidden rounded-md border border-border">
            <button
              type="button"
              onClick={() => handleLogicChange('AND')}
              aria-pressed={logic === 'AND'}
              disabled={disabled}
              className={cn(
                'border-none px-3 py-1.5 text-xs font-semibold transition-all disabled:cursor-not-allowed disabled:opacity-50',
                logic === 'AND'
                  ? 'bg-primary text-primary-foreground hover:bg-primary/90'
                  : 'bg-card text-muted-foreground hover:bg-muted dark:bg-muted/30 dark:hover:bg-muted/50'
              )}
            >
              AND
            </button>
            <button
              type="button"
              onClick={() => handleLogicChange('OR')}
              aria-pressed={logic === 'OR'}
              disabled={disabled}
              className={cn(
                'border-none px-3 py-1.5 text-xs font-semibold transition-all disabled:cursor-not-allowed disabled:opacity-50',
                logic === 'OR'
                  ? 'bg-primary text-primary-foreground hover:bg-primary/90'
                  : 'bg-card text-muted-foreground hover:bg-muted dark:bg-muted/30 dark:hover:bg-muted/50'
              )}
            >
              OR
            </button>
          </div>
          <span className="text-sm text-muted-foreground">conditions</span>
        </div>
      )}

      {/* Conditions list */}
      {conditions.length === 0 ? (
        <div className="rounded-lg border-2 border-dashed border-border p-6 text-center">
          <p className="text-sm text-muted-foreground">No conditions added yet. This rule will run unconditionally.</p>
        </div>
      ) : (
        <div className="flex flex-col gap-3">
          {conditions.map((condition, index) => (
            <ConditionRow
              key={index}
              condition={condition}
              index={index}
              availableFields={availableFields}
              categories={categories}
              onChange={handleChange}
              onRemove={handleRemove}
              disabled={disabled}
            />
          ))}
        </div>
      )}

      {/* Add button */}
      <button
        type="button"
        onClick={handleAdd}
        disabled={disabled}
        className="inline-flex items-center justify-center gap-2 rounded-lg border-2 border-dashed border-border bg-transparent px-4 py-2.5 text-sm font-medium text-muted-foreground transition-all hover:border-primary hover:bg-blue-50 hover:text-primary disabled:cursor-not-allowed disabled:opacity-50 dark:hover:bg-blue-900 dark:hover:text-blue-400"
      >
        <Plus className="h-4 w-4" aria-hidden="true" />
        Add condition
      </button>
    </div>
  )
}
