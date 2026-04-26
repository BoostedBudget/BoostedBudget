import type { Transaction } from '@/features/transactions/types'

/**
 * Parse a JSON string to a category array.
 * Returns empty array if invalid.
 */
function parseCategoryJson(json: string | null | undefined): string[] {
  if (!json) return []
  try {
    const parsed = JSON.parse(json)
    return Array.isArray(parsed) && parsed.length > 0 ? (parsed as string[]) : []
  } catch {
    return []
  }
}

/**
 * Returns the effective display category path for a transaction as a string array.
 *
 * Fallback chain:
 *   category_override → legacy category → personal_finance_category_data.primary_display → []
 *
 * The explicit-uncategorized guard: when category_source is 'manual' and there is no
 * category_override, the user deliberately cleared the category — return [] rather than
 * falling back to the legacy category field.
 */
export function getEffectiveCategory(transaction: Transaction): string[] {
  const override = parseCategoryJson(transaction.category_override)

  // Explicit-uncategorized guard: manual source with no non-empty override means the user
  // deliberately cleared the category (category_override is absent or '[]') — return [] instead
  // of falling through to legacy category or PFC.
  if (transaction.category_source === 'manual' && override.length === 0) {
    return []
  }

  if (override.length > 0) return override

  const legacy = parseCategoryJson(transaction.category)
  if (legacy.length > 0) return legacy

  return transaction.personal_finance_category_data?.primary_display
    ? [transaction.personal_finance_category_data.primary_display]
    : []
}
