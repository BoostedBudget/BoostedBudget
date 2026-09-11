/**
 * TransactionBudgetBadge Component
 * Issue #499: Transaction-Budget Linking UI
 *
 * Displays a clickable badge on TransactionCard showing which budget category
 * the transaction matches. Clicking navigates to the budget detail page.
 */

import { Link } from 'react-router'
import { FolderIcon, TagIcon, SparklesIcon } from 'lucide-react'
import { getBudgetDetailPath } from '../../constants'
import type { BudgetCategoryMatch, MatchReason } from '../../types'
import { formatBudgetCategoryLabel } from '../../types'

export interface TransactionBudgetBadgeProps {
  /** Match result (BudgetCategoryMatch, budgets/types.ts), or null/undefined if no match */
  match: BudgetCategoryMatch | null | undefined
}

/**
 * Match reason metadata: icon and tooltip text
 */
const MATCH_REASON_CONFIG: Record<MatchReason, { Icon: typeof FolderIcon; tooltip: string }> = {
  category: { Icon: FolderIcon, tooltip: 'Matched by category' },
  tag: { Icon: TagIcon, tooltip: 'Matched by tag' },
  both: { Icon: SparklesIcon, tooltip: 'Matched by category and tag' },
}

/**
 * TransactionBudgetBadge - Badge showing budget category match
 *
 * Renders a small badge that:
 * - Shows the primary category name
 * - Has an icon indicating match type (category/tag/both)
 * - Links to the budget detail page
 * - Is accessible via keyboard
 */
export function TransactionBudgetBadge({ match }: TransactionBudgetBadgeProps) {
  // Don't render if no match
  if (!match) return null

  const { category, matchReason } = match
  const { Icon, tooltip } = MATCH_REASON_CONFIG[matchReason]
  const displayName = formatBudgetCategoryLabel(category)

  return (
    <Link
      to={getBudgetDetailPath(category.budget_id)}
      className="transaction-budget-badge inline-flex items-center gap-1 px-2 py-0.5 text-[0.6875rem] font-medium text-primary bg-primary/10 border border-primary/20 rounded no-underline whitespace-nowrap max-w-[120px] overflow-hidden text-ellipsis shrink-0 transition-colors duration-150 hover:bg-primary/20 hover:border-primary/40 focus:outline-none focus:ring-2 focus:ring-ring focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
      title={tooltip}
      aria-label={`Budget match: ${tooltip}`}
    >
      <Icon className="transaction-budget-badge__icon w-3 h-3 shrink-0" aria-hidden="true" />
      <span className="overflow-hidden text-ellipsis" data-ph-mask>{displayName}</span>
    </Link>
  )
}
