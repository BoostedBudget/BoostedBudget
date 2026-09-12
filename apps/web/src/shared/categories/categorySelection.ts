import type { Transaction } from '@/features/transactions/types'
import { getEffectiveCategory } from './getEffectiveCategory'

/** The editor value is also the mutation payload. Labels never identify categories. */
export type CategorySelection =
  | { kind: 'pfc'; pfc_primary: string; pfc_detailed?: string }
  | { kind: 'custom'; custom_category_id: string }
  | { kind: 'clear' }

export function seedCategorySelection(transaction: Transaction): CategorySelection {
  const effective = getEffectiveCategory(transaction)
  if (!effective) return { kind: 'clear' }
  if (effective.kind === 'custom') return { kind: 'custom', custom_category_id: effective.custom_category_id }
  return { kind: 'pfc', pfc_primary: effective.pfc_primary, pfc_detailed: effective.pfc_detailed }
}

export function categorySelectionEqual(a: CategorySelection, b: CategorySelection): boolean {
  if (a.kind === 'clear') return b.kind === 'clear'
  if (a.kind === 'custom') return b.kind === 'custom' && a.custom_category_id === b.custom_category_id
  return b.kind === 'pfc' && a.pfc_primary === b.pfc_primary && a.pfc_detailed === b.pfc_detailed
}
