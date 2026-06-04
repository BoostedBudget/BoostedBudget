import { Clock, Sparkles, Zap, CheckCircle2 } from 'lucide-react'
import { cn } from '@/lib/utils'

export type ReviewBadgeVariant = 'unreviewed' | 'plaid' | 'rule' | 'manual'

const variantStyles: Record<ReviewBadgeVariant, string> = {
  unreviewed: 'bg-warning/10 text-warning border border-warning/20',
  plaid: 'bg-info/10 text-info border border-info/20',
  rule: 'bg-info/10 text-info border border-info/20',
  manual: 'bg-success/10 text-success border border-success/20',
}

const variantLabels: Record<ReviewBadgeVariant, string> = {
  unreviewed: 'Unreviewed',
  plaid: 'Categorized',
  rule: 'By Rule',
  manual: 'Reviewed',
}

const variantIcons = {
  unreviewed: Clock,
  plaid: Sparkles,
  rule: Zap,
  manual: CheckCircle2,
} satisfies Record<ReviewBadgeVariant, React.ElementType>

interface ReviewBadgeProps {
  variant: ReviewBadgeVariant
  className?: string
  'data-testid'?: string
}

/**
 * ReviewBadge — displays the review/categorization state of a transaction.
 *
 * Variants:
 *   - unreviewed: amber/warning — Clock icon — "Unreviewed"
 *   - plaid:      info/blue — Sparkles icon — "Categorized" (auto by Plaid)
 *   - rule:       info/blue — Zap icon — "By Rule" (automation rule)
 *   - manual:     success/green — CheckCircle2 icon — "Reviewed"
 *
 * Color is never used alone — every variant pairs color with icon + text
 * for WCAG AA compliance.
 *
 * Use reviewBadgeVariantFromTransaction() to derive the variant from a
 * transaction's reviewed/category_source fields.
 */
export function ReviewBadge({ variant, className, 'data-testid': testId }: ReviewBadgeProps) {
  const Icon = variantIcons[variant]
  const label = variantLabels[variant]
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium',
        variantStyles[variant],
        className,
      )}
      aria-label={`Review status: ${label}`}
      data-testid={testId}
    >
      <Icon className="size-3 shrink-0" aria-hidden="true" />
      {label}
    </span>
  )
}

