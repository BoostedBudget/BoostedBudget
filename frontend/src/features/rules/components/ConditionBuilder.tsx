/**
 * ConditionBuilder Component
 * Issue #39: Rule Builder UI
 *
 * Component for building dynamic condition rows with field/operator/value.
 * Supports AND/OR logic toggle.
 */

import { useCallback } from 'react'
import { Plus, Trash2 } from 'lucide-react'
import {
  CONDITION_FIELD_OPTIONS,
  CONDITION_OPERATOR_LABELS,
  type ConditionFieldOption,
} from '../constants'
import type { RuleCondition, ConditionLogic, ConditionOperator } from '../types'
import './ConditionBuilder.css'

export interface ConditionBuilderProps {
  /** Current conditions */
  conditions: RuleCondition[]
  /** Current logic (AND/OR) */
  logic: ConditionLogic
  /** Called when conditions change */
  onConditionsChange: (conditions: RuleCondition[]) => void
  /** Called when logic changes */
  onLogicChange: (logic: ConditionLogic) => void
  /** Disable all controls */
  disabled?: boolean
  /** Additional CSS class */
  className?: string
}

// Get default condition
function createDefaultCondition(): RuleCondition {
  const defaultField = CONDITION_FIELD_OPTIONS[0]
  return {
    field: defaultField.value,
    operator: defaultField.operators[0],
    value: defaultField.valueType === 'number' ? 0 : '',
  }
}

// Get operators for a field
function getOperatorsForField(fieldValue: string): ConditionOperator[] {
  const field = CONDITION_FIELD_OPTIONS.find((f) => f.value === fieldValue)
  return field?.operators ?? ['eq']
}

// Get value type for a field
function getValueTypeForField(fieldValue: string): ConditionFieldOption['valueType'] {
  const field = CONDITION_FIELD_OPTIONS.find((f) => f.value === fieldValue)
  return field?.valueType ?? 'string'
}

interface ConditionRowProps {
  condition: RuleCondition
  index: number
  onChange: (index: number, condition: RuleCondition) => void
  onRemove: (index: number) => void
  disabled?: boolean
}

function ConditionRow({ condition, index, onChange, onRemove, disabled }: ConditionRowProps) {
  const operators = getOperatorsForField(condition.field)
  const valueType = getValueTypeForField(condition.field)

  const handleFieldChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      const newField = e.target.value
      const newOperators = getOperatorsForField(newField)
      const newValueType = getValueTypeForField(newField)

      onChange(index, {
        field: newField,
        operator: newOperators[0],
        value: newValueType === 'number' ? 0 : '',
      })
    },
    [index, onChange]
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

  // Group fields by category for optgroup
  const groupedFields = CONDITION_FIELD_OPTIONS.reduce(
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
    <div className="condition-builder__row">
      {/* Field selector */}
      <div className="condition-builder__field">
        <label htmlFor={`condition-field-${index}`} className="condition-builder__label">
          Field
        </label>
        <select
          id={`condition-field-${index}`}
          value={condition.field}
          onChange={handleFieldChange}
          disabled={disabled}
          className="condition-builder__select"
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
      <div className="condition-builder__operator">
        <label htmlFor={`condition-operator-${index}`} className="condition-builder__label">
          Operator
        </label>
        <select
          id={`condition-operator-${index}`}
          value={condition.operator}
          onChange={handleOperatorChange}
          disabled={disabled}
          className="condition-builder__select"
        >
          {operators.map((op) => (
            <option key={op} value={op}>
              {CONDITION_OPERATOR_LABELS[op]}
            </option>
          ))}
        </select>
      </div>

      {/* Value input */}
      <div className="condition-builder__value">
        <label htmlFor={`condition-value-${index}`} className="condition-builder__label">
          Value
        </label>
        <input
          id={`condition-value-${index}`}
          type={valueType === 'number' ? 'number' : 'text'}
          value={condition.value as string | number}
          onChange={handleValueChange}
          disabled={disabled}
          className="condition-builder__input"
          placeholder={valueType === 'number' ? '0' : 'Enter value...'}
        />
      </div>

      {/* Remove button */}
      <button
        type="button"
        onClick={() => onRemove(index)}
        disabled={disabled}
        className="condition-builder__remove"
        aria-label="Remove condition"
        title="Remove condition"
      >
        <Trash2 className="condition-builder__icon" aria-hidden="true" />
      </button>
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
  disabled = false,
  className = '',
}: ConditionBuilderProps) {
  const handleAdd = useCallback(() => {
    onConditionsChange([...conditions, createDefaultCondition()])
  }, [conditions, onConditionsChange])

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
    <div className={`condition-builder ${className}`.trim()}>
      {/* Logic toggle (only show when multiple conditions) */}
      {conditions.length > 1 && (
        <div className="condition-builder__logic">
          <span className="condition-builder__logic-label">Match</span>
          <div className="condition-builder__logic-toggle">
            <button
              type="button"
              onClick={() => handleLogicChange('AND')}
              aria-pressed={logic === 'AND'}
              disabled={disabled}
              className={`condition-builder__logic-btn ${
                logic === 'AND' ? 'condition-builder__logic-btn--active' : ''
              }`}
            >
              AND
            </button>
            <button
              type="button"
              onClick={() => handleLogicChange('OR')}
              aria-pressed={logic === 'OR'}
              disabled={disabled}
              className={`condition-builder__logic-btn ${
                logic === 'OR' ? 'condition-builder__logic-btn--active' : ''
              }`}
            >
              OR
            </button>
          </div>
          <span className="condition-builder__logic-label">conditions</span>
        </div>
      )}

      {/* Conditions list */}
      {conditions.length === 0 ? (
        <div className="condition-builder__empty">
          <p>No conditions added yet. This rule will run unconditionally.</p>
        </div>
      ) : (
        <div className="condition-builder__list">
          {conditions.map((condition, index) => (
            <ConditionRow
              key={index}
              condition={condition}
              index={index}
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
        className="condition-builder__add"
      >
        <Plus className="condition-builder__icon" aria-hidden="true" />
        Add condition
      </button>
    </div>
  )
}
