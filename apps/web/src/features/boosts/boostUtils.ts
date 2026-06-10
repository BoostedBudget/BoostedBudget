/**
 * Boost formatting utilities
 * Issue #1283: Rename rules → boosts (Phase 5)
 */

import type { BoostCondition } from '@boostedbudget/api-client/models'
import { CONDITION_FIELD_OPTIONS } from './boostConstants'

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
 * Returns a human-readable one-line summary of the boost's conditions.
 * e.g. "When Budget % Used ≥ 80"
 * Returns empty string when there are no conditions.
 */
export function formatBoostConditionsSummary(
  conditions: BoostCondition[] | null | undefined,
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
 * Returns a human-readable one-line summary of the boost's action.
 * e.g. "Suggest transfer $200", "Send budget alert notification"
 */
export function formatBoostActionSummary(boostType: string, actions: unknown): string {
  const a = actions as Record<string, unknown> | null | undefined
  switch (boostType) {
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
      return 'Execute boost action'
  }
}
