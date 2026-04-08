/**
 * TransactionBudgetBadge Component
 * Issue #499: Transaction-Budget Linking UI
 *
 * Displays a clickable badge on TransactionCard showing which budget category
 * the transaction matches. Clicking navigates to the budget detail page.
 */

import { Link } from 'react-router-dom'
import { FolderIcon, TagIcon, SparklesIcon } from 'lucide-react'
import type { BudgetCategoryMatch, MatchReason } from '../../utils/transactionBudgetMatcher'

export interface TransactionBudgetBadgeProps {
  /** Match result from transactionBudgetMatcher, or null/undefined if no match */
  match: BudgetCategoryMatch | null | undefined
}

/**
 * Icons for each match reason type
 */
const MATCH_REASON_ICONS: Record<MatchReason, typeof FolderIcon> = {
  category: FolderIcon,
  tag: TagIcon,
  both: SparklesIcon,
}

/**
 * Tooltip text for each match reason type
 */
const MATCH_REASON_TOOLTIPS: Record<MatchReason, string> = {
  category: 'Matched by category',
  tag: 'Matched by tag',
  both: 'Matched by category and tag',
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
  const Icon = MATCH_REASON_ICONS[matchReason]
  const tooltip = MATCH_REASON_TOOLTIPS[matchReason]

  // Display primary category only (first element), with fallback
  const displayName = category.category[0] || 'Budget'

  return (
    <Link
      to={`/budgets/${category.budget_id}`}
      className="inline-flex items-center gap-1 px-2 py-0.5 text-[0.6875rem] font-medium text-primary bg-primary/10 border border-primary/20 rounded no-underline whitespace-nowrap max-w-[120px] overflow-hidden text-ellipsis shrink-0 transition-colors duration-150 hover:bg-primary/20 hover:border-primary/40 focus:outline-none focus:ring-2 focus:ring-ring focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
      title={tooltip}
      aria-label={`Budget: ${displayName} (${tooltip})`}
    >
      <Icon className="w-3 h-3 shrink-0" aria-hidden="true" />
      <span className="overflow-hidden text-ellipsis">{displayName}</span>
    </Link>
  )
}
