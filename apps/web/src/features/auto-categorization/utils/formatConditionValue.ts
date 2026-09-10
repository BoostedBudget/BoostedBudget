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
 * ⚠ THE RETURN IS ALWAYS USER-IDENTIFYING TEXT: a merchant substring, an amount, a date, and
 * since #1626 an account NAME. Every caller must render it inside `data-ph-mask`, and must never
 * put it in an `aria-label`, `title` or any other attribute — session replay records attribute
 * values verbatim and `data-ph-mask` does not cover them. The LABEL
 * (`CONDITION_TYPE_LABELS[type]`) is a fixed vocabulary and deliberately stays outside the mask,
 * so session replay keeps the shape of the rule without keeping its contents.
 *
 * ⚠ Issue #1626 (Task K): `account_id` is resolved to a name HERE, not on the backend. The
 * backend route was rejected because the frozen `definition_snapshot` is stored and returned as
 * an opaque `json.RawMessage` (`rule_execution_history_repository.go`) that is never re-enriched
 * — a read-time `value_display` would reach the live rule and never the snapshot, which is
 * exactly the drift this shared renderer exists to prevent. Resolving in the one renderer both
 * callers share makes the live rule and the frozen copy agree by construction.
 */

import { formatPFCPrimary, formatPFCDetailed } from '@/shared/categories/formatPfc'
import type { Account } from '@boostedbudget/api-client/models'
import type { ConditionType } from '../types'
import { UNKNOWN_ACCOUNT_LABEL } from '../constants'

export interface FormatConditionValueOptions {
  /**
   * id → display-name index for `account_id` conditions, from `buildAccountLabelIndex`.
   * Absent (or missing the id) means the account cannot be named, and the neutral
   * `UNKNOWN_ACCOUNT_LABEL` is rendered — never the raw id.
   */
  accountLabels?: Readonly<Record<string, string>>
}

/**
 * Builds the id → name index `formatConditionValue` looks an `account_id` condition up in.
 *
 * Each account is indexed under BOTH keys a rule can hold. The editor writes
 * `user_account_id` (`AutoCategorizationRuleEditor`'s `account_select` arm), but rules predating
 * migration 000072's backfill can still hold the raw `accounts.id`, which is why the editor also
 * carries a hydration fallback for that shape.
 *
 * The label prefers the user's own `nickname` over the provider `name`, matching how
 * `InvestmentDetailDrawer` resolves an account for display.
 */
export function buildAccountLabelIndex(accounts: readonly Account[]): Record<string, string> {
  const index: Record<string, string> = {}
  for (const account of accounts) {
    const label = account.nickname?.trim() || account.name
    if (!label) continue
    if (account.user_account_id) index[account.user_account_id] = label
    if (account.id) index[account.id] = label
  }
  return index
}

export function formatConditionValue(
  condition: {
    type: ConditionType
    value: unknown
    value_display?: string
  },
  options?: FormatConditionValueOptions
): string {
  // An account id is opaque and must never reach the screen. Checked before `value_display`
  // because nothing populates that field for account conditions today, so trusting it would be
  // trusting a channel that only an attacker-shaped or future payload could fill.
  if (condition.type === 'account_id') {
    const id = typeof condition.value === 'string' ? condition.value : ''
    return (id && options?.accountLabels?.[id]) || UNKNOWN_ACCOUNT_LABEL
  }

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
