/**
 * conditionLabel — the single renderer for a rule condition's LEFT-hand side.
 *
 * Issue #1625. A condition can arrive in either of two shapes and both must render identically:
 *   - the {field, operator} triple, which is what the server emits for every live rule; and
 *   - a legacy fused `type` alone, which is what every frozen
 *     `rule_execution_history.definition_snapshot` row contains, permanently.
 *
 * Snapshots are immutable historical records and are deliberately NOT normalized on read, so the
 * legacy branch here is not a transitional convenience — it is load-bearing forever.
 *
 * ⚠ The return is a FIXED vocabulary (field and operator labels), never user content, so it
 * belongs OUTSIDE `data-ph-mask`. The right-hand side — see formatConditionValue — is user-typed
 * and must always be masked.
 */

import {
  CONDITION_FIELD_LABELS,
  CONDITION_OPERATOR_LABELS,
  CONDITION_TYPE_LABELS,
  type ConditionField,
  type ConditionOperator,
  type ConditionType,
} from '../types'

export function conditionLabel(condition: {
  type?: ConditionType
  field?: ConditionField
  operator?: ConditionOperator
}): string {
  const { field, operator } = condition
  if (field && operator) {
    const fieldLabel = CONDITION_FIELD_LABELS[field] ?? field
    const opLabel = CONDITION_OPERATOR_LABELS[operator] ?? operator
    return `${fieldLabel} ${opLabel}`
  }

  // Legacy snapshot, or a server that predates the triple.
  if (condition.type && condition.type !== 'expression') {
    return CONDITION_TYPE_LABELS[condition.type] ?? condition.type
  }

  // 'expression' with no field/operator should be unreachable — the server always sends the pair
  // alongside it — but a generic label beats rendering the raw sentinel at the user.
  return CONDITION_TYPE_LABELS.expression
}
