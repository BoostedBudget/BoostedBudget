import type { Transaction } from '@/features/transactions/types'
import { formatPFCEnum } from './formatPfc'

/**
 * The resolved effective category for a transaction.
 * Discriminated union: either a PFC primary (+ optional detailed) or a custom category UUID.
 */
export type EffectiveCategory =
  | { kind: 'pfc'; pfc_primary: string; pfc_detailed?: string; pfc_primary_display?: string; pfc_detailed_display?: string }
  | { kind: 'custom'; custom_category_id: string; custom_category_display?: string }

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
    return {
      kind: 'custom',
      custom_category_id: transaction.custom_category_override_id,
      custom_category_display: transaction.custom_category_override_display ?? undefined,
    }
  }

  if (transaction.category_override_kind === 'pfc' && transaction.pfc_primary_override) {
    return {
      kind: 'pfc',
      pfc_primary: transaction.pfc_primary_override,
      pfc_detailed: transaction.pfc_detailed_override ?? undefined,
      pfc_primary_display: transaction.pfc_primary_override_display ?? undefined,
      pfc_detailed_display: transaction.pfc_detailed_override_display ?? undefined,
    }
  }

  if (transaction.pfc_primary) {
    return {
      kind: 'pfc',
      pfc_primary: transaction.pfc_primary,
      pfc_detailed: transaction.pfc_detailed ?? undefined,
      pfc_primary_display: transaction.pfc_primary_display ?? undefined,
      pfc_detailed_display: transaction.pfc_detailed_display ?? undefined,
    }
  }

  return null
}

/**
 * Returns the effective detailed PFC sub-category enum for a transaction, or null.
 *
 * Priority (mirrors getEffectiveCategory):
 *   1. pfc override (kind='pfc') → pfc_detailed_override
 *   2. Plaid-assigned → pfc_detailed
 *   3. null (custom category, uncategorized, or no detailed sub-category)
 */
export function getEffectiveDetailedCategory(transaction: Transaction): string | null {
  // Mirror getEffectiveCategory: only consume the pfc override when pfc_primary_override is present.
  // Without pfc_primary_override the override is considered incomplete and we fall through to the
  // Plaid-assigned pfc_detailed (same priority logic as getEffectiveCategory).
  if (transaction.category_override_kind === 'pfc' && transaction.pfc_primary_override) {
    return transaction.pfc_detailed_override ?? null
  }
  if (transaction.category_override_kind !== 'custom' && transaction.pfc_primary) {
    return transaction.pfc_detailed ?? null
  }
  return null
}

/**
 * Returns the display label for an effective category.
 * Prefers server-supplied display fields, falls back to formatPFCEnum.
 */
export function getEffectiveCategoryLabel(effective: EffectiveCategory | null): string {
  if (!effective) return 'Uncategorized'
  if (effective.kind === 'custom') return effective.custom_category_display ?? effective.custom_category_id
  if (effective.pfc_detailed_display) return effective.pfc_detailed_display
  if (effective.pfc_primary_display) return effective.pfc_primary_display
  return formatPFCEnum(effective.pfc_detailed ?? effective.pfc_primary)
}

/**
 * Returns a transaction's effective category in the legacy `string[]` shape:
 * `[custom_category_id]`, `[pfc_primary]`, or
 * `[pfc_primary, pfc_detailed]`. Empty array means genuinely uncategorized.
 *
 * Issue #1625: extracted from TransactionEditor so the triage recategorize panel seeds from
 * the same source. TriageDetailView previously opened with `[]`, which rendered a
 * well-categorized transaction as "Uncategorized" AND — because an empty selection is read as
 * `{ kind: 'clear' }` on save — destroyed the category when Apply was pressed without touching it.
 *
 * These are raw enums / UUIDs, NOT display names. CategoryEditor and its consumers now use
 * seedCategorySelection instead, retaining the category kind alongside its identity (#1638).
 */
export function effectiveCategoryToStringArray(transaction: Transaction): string[] {
  const eff = getEffectiveCategory(transaction)
  if (!eff) return []
  if (eff.kind === 'custom') return [eff.custom_category_id]
  return eff.pfc_detailed ? [eff.pfc_primary, eff.pfc_detailed] : [eff.pfc_primary]
}
