/**
 * MixedCurrencyBadge — the visible half of #1631 CX1.
 *
 * When the backend serves `is_mixed_currency: true` it is refusing to name a currency: the total
 * is a currency-blind sum of positions in two or more currencies, and there is no FX rate source
 * in this system to convert them with. The figure beside this badge therefore renders with NO
 * currency symbol, which is honest but silent -- a reader would otherwise be left to guess why the
 * symbol vanished. This says it.
 *
 * One component rather than a repeated span, so every investment surface that has to declare the
 * mixed case says the same words in the same place.
 */

import { Badge } from '@/components/ui/badge'

export function MixedCurrencyBadge({ className }: { className?: string }) {
  return (
    <Badge variant="warning" size="sm" className={className}>
      Mixed currencies
    </Badge>
  )
}
