/**
 * Rule formatting utilities
 * Issue #1267 Gap W5: Human-readable condition + action summaries for RuleCard/RuleTable
 */

import type { AutomationRuleCondition } from '@boostedbudget/api-client/models'
import { CONDITION_FIELD_OPTIONS } from './constants'

/** Short operator symbols for condition summaries */
const OP_SYMBOL: Record<string, string> = {
  eq: '=',
  neq: '≠',
  gt: '>',
  gte: '≥',
  lt: '<',
  lte: '≤',
  contains: 'contains',
}

/**
 * Returns a human-readable one-line summary of the rule's conditions.
 * e.g. "When Budget % Used ≥ 80"
 * Returns empty string when there are no conditions.
 */
export function formatRuleConditionsSummary(
  conditions: AutomationRuleCondition[] | null | undefined,
  logic = 'AND',
): string {
  if (!conditions || conditions.length === 0) return ''

  const parts = conditions.map((c) => {
    const fieldLabel = CONDITION_FIELD_OPTIONS.find((f) => f.value === c.field)?.label ?? c.field
    const opSymbol = OP_SYMBOL[c.operator] ?? c.operator
    const val = c.value != null ? String(c.value) : ''
    return `${fieldLabel} ${opSymbol} ${val}`.trim()
  })

  return `When ${parts.join(` ${logic} `)}`
}

/**
 * Returns a human-readable one-line summary of the rule's action.
 * e.g. "Suggest transfer $200", "Send budget alert notification"
 */
export function formatRuleActionSummary(ruleType: string, actions: unknown): string {
  const a = actions as Record<string, unknown> | null | undefined
  switch (ruleType) {
    case 'transfer_suggestion': {
      const amount = typeof a?.amount === 'number'
        ? ` $${a.amount.toLocaleString()}`
        : ''
      return `Suggest transfer${amount}`
    }
    case 'budget_alert':
      return 'Send budget alert notification'
    case 'category_rollover':
      return 'Roll over category funds'
    case 'savings_goal':
      return 'Auto-contribute to savings goal'
    default:
      return 'Execute rule action'
  }
}
