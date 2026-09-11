import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'
import { formatPercent } from '@/shared/charts/formatters'

const changeIndicatorVariants = cva('flex items-center gap-1 text-sm', {
  variants: {
    variant: {
      positive: 'text-success',
      negative: 'text-destructive',
      neutral: 'text-muted-foreground',
    },
  },
  defaultVariants: {
    variant: 'neutral',
  },
})

interface ChangeIndicatorProps extends VariantProps<typeof changeIndicatorVariants> {
  amount: number
  percent: number
  /** Subject label used in aria-label, e.g. "Portfolio". Defaults to "Net worth". */
  label?: string
  /**
   * Formats the (already absolute) amount. Defaults to whole dollars.
   *
   * #1631: injected rather than imported so this component gains no dependency on the
   * investments feature's currency helper. The investments Portfolio card renders this directly
   * beneath a currency-aware headline, and a hard-coded dollar here labelled the same figure as
   * two different currencies. Injection also keeps the import graph pointing one way -- a
   * `shared/`-to-feature edge is what reopened the #1601 landing-route budget on this branch.
   */
  formatAmount?: (amount: number) => string
  className?: string
}

const GLYPHS = { positive: '↑', negative: '↓', neutral: '→' } as const

function formatUSD(amount: number): string {
  return amount.toLocaleString('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  })
}

export function ChangeIndicator({ variant = 'neutral', amount, percent, label = 'Net worth', formatAmount = formatUSD, className }: ChangeIndicatorProps) {
  const directionMap = { positive: 'up', negative: 'down', neutral: 'unchanged' } as const
  const direction = directionMap[variant ?? 'neutral']
  // Numeric percent intentionally omitted from aria-label — PostHog session replay captures
  // attribute text, which would expose a financial figure. The visible (data-ph-mask'd) span
  // is still read by screen readers. See docs/security/posthog-pii-masking.md §attribute rule.
  const ariaLabel = `${label} ${direction} versus previous period`

  return (
    <span
      className={cn(changeIndicatorVariants({ variant }), className)}
      aria-label={ariaLabel}
    >
      <span aria-hidden="true">{GLYPHS[variant ?? 'neutral']}</span>
      <span data-ph-mask>{formatAmount(Math.abs(amount))}</span>
      <span data-ph-mask className="text-muted-foreground">({formatPercent(Math.abs(percent))})</span>
    </span>
  )
}
