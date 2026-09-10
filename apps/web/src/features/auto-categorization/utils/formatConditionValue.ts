/**
 * formatConditionValue — the single renderer for a rule condition's right-hand side.
 *
 * Issue #1569 T9.4. Extracted VERBATIM from AutoCategorizationRuleCard, which had it as a private
 * function, so the retained-history panel does not become a second, drifting formatter for the
 * same shape. Two callers now exist and they render the same condition from two sources: the LIVE
 * rule (`CategorizationRule.conditions`) and the FROZEN copy inside a rule execution's
 * `definition_snapshot`. Those must read identically — a user comparing "what the rule does now"
 * with "what it did then" is the whole point of keeping the snapshot.
 *
 * ⚠ THE RETURN IS ALWAYS USER-TYPED TEXT: a merchant substring, an amount, a date. Every caller
 * must render it inside `data-ph-mask`. The LABEL (`CONDITION_TYPE_LABELS[type]`) is a fixed
 * vocabulary and deliberately stays outside the mask, so session replay keeps the shape of the
 * rule without keeping its contents.
 */

import { formatPFCPrimary, formatPFCDetailed } from '@/shared/categories/formatPfc'
import type { ConditionType } from '../types'

export function formatConditionValue(condition: {
  type: ConditionType
  value: unknown
  value_display?: string
}): string {
  if (
    condition.value_display &&
    (condition.type === 'existing_pfc_primary' || condition.type === 'existing_pfc_detailed')
  ) {
    return condition.value_display
  }

  const { type, value } = condition
  if (typeof value === 'string') {
    if (type === 'existing_pfc_primary') return formatPFCPrimary(value)
    if (type === 'existing_pfc_detailed') return formatPFCDetailed(value)
    return value
  }

  if (Array.isArray(value)) return value.join(', ')

  if (typeof value === 'object' && value !== null) {
    const obj = value as Record<string, unknown>
    // #1625: a numeric range requires BOTH bounds server-side, but a snapshot written before
    // that check could hold only one — render what is there rather than falling through to the
    // date branch and then to '[object Object]'.
    if ('min' in obj || 'max' in obj) {
      if ('min' in obj && 'max' in obj) return `${obj.min} - ${obj.max}`
      if ('min' in obj) return `at least ${obj.min}`
      return `at most ${obj.max}`
    }
    if ('start' in obj || 'end' in obj) {
      const parts: string[] = []
      if (obj.start) parts.push(`from ${obj.start}`)
      if (obj.end) parts.push(`to ${obj.end}`)
      return parts.join(' ')
    }
  }

  return String(value)
}
