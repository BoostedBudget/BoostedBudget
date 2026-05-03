import type { Transaction } from '@/features/transactions/types'
import { formatPFCEnum } from './formatPfc'

/**
 * The resolved effective category for a transaction.
 * Discriminated union: either a PFC primary (+ optional detailed) or a custom category UUID.
 */
export type EffectiveCategory =
  | { kind: 'pfc'; pfc_primary: string; pfc_detailed?: string; pfc_primary_display?: string; pfc_detailed_display?: string }
  | { kind: 'custom'; custom_category_id: string }

/**
 * Helper to build a PFC-kind EffectiveCategory from enum and display fields.
 */
function toPFCCategory(
  pfc_primary: string,
  pfc_detailed?: string | null,
  pfc_primary_display?: string | null,
  pfc_detailed_display?: string | null
): EffectiveCategory {
  return {
    kind: 'pfc',
    pfc_primary,
    pfc_detailed: pfc_detailed ?? undefined,
    pfc_primary_display: pfc_primary_display ?? undefined,
    pfc_detailed_display: pfc_detailed_display ?? undefined,
  }
}

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
    return toPFCCategory(
      transaction.pfc_primary_override,
      transaction.pfc_detailed_override,
      transaction.pfc_primary_override_display,
      transaction.pfc_detailed_override_display
    )
  }

  if (transaction.pfc_primary) {
    return toPFCCategory(
      transaction.pfc_primary,
      transaction.pfc_detailed,
      transaction.pfc_primary_display,
      transaction.pfc_detailed_display
    )
  }

  return null
}

/**
 * Returns the display label for an effective category.
 * Prefers server-supplied display fields, falls back to formatPFCEnum.
 */
export function getEffectiveCategoryLabel(effective: EffectiveCategory | null): string {
  if (!effective) return 'Uncategorized'
  if (effective.kind === 'custom') return effective.custom_category_id
  if (effective.pfc_detailed_display) return effective.pfc_detailed_display
  if (effective.pfc_primary_display) return effective.pfc_primary_display
  return formatPFCEnum(effective.pfc_detailed ?? effective.pfc_primary)
}
