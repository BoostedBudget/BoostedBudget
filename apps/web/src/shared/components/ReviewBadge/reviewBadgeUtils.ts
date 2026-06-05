import type { ReviewBadgeVariant } from './ReviewBadge'

export function reviewBadgeVariantFromTransaction(tx: {
  reviewed?: boolean
  category_source?: 'plaid' | 'rule' | 'manual'
}): ReviewBadgeVariant {
  if (!tx.reviewed) return 'unreviewed'
  const src = tx.category_source ?? 'manual'
  if (src === 'plaid') return 'plaid'
  if (src === 'rule') return 'rule'
  return 'manual'
}
