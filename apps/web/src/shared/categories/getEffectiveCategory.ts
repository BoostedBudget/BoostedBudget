import type { Transaction } from '@/features/transactions/types'

/**
 * The resolved effective category for a transaction.
 * Discriminated union: either a PFC primary (+ optional detailed) or a custom category UUID.
 */
export type EffectiveCategory =
  | { kind: 'pfc'; pfc_primary: string; pfc_detailed?: string }
  | { kind: 'custom'; custom_category_id: string }

/**
 * Returns the effective category for a transaction as a discriminated union.
 *
 * Priority:
 *   1. category_override_kind = 'custom' → custom category
 *   2. category_override_kind = 'pfc'    → pfc override
 *   3. pfc_primary (Plaid-assigned)      → pfc source
 *   4. null                              → uncategorized
 */
export function getEffectiveCategory(transaction: Transaction): EffectiveCategory | null {
  if (transaction.category_override_kind === 'custom' && transaction.custom_category_override_id) {
    return { kind: 'custom', custom_category_id: transaction.custom_category_override_id }
  }

  if (transaction.category_override_kind === 'pfc' && transaction.pfc_primary_override) {
    return {
      kind: 'pfc',
      pfc_primary: transaction.pfc_primary_override,
      pfc_detailed: transaction.pfc_detailed_override ?? undefined,
    }
  }

  if (transaction.pfc_primary) {
    return {
      kind: 'pfc',
      pfc_primary: transaction.pfc_primary,
      pfc_detailed: transaction.pfc_detailed ?? undefined,
    }
  }

  return null
}

/**
 * Returns the display label for an effective category.
 */
export function getEffectiveCategoryLabel(effective: EffectiveCategory | null): string {
  if (!effective) return 'Uncategorized'
  if (effective.kind === 'custom') return effective.custom_category_id
  return effective.pfc_detailed ?? effective.pfc_primary
}
