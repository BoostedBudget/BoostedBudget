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
  className?: string
}

const GLYPHS = { positive: '↑', negative: '↓', neutral: '→' } as const

function formatAmount(amount: number): string {
  return Math.abs(amount).toLocaleString('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  })
}

export function ChangeIndicator({ variant = 'neutral', amount, percent, label = 'Net worth', className }: ChangeIndicatorProps) {
  const directionMap = { positive: 'up', negative: 'down', neutral: 'unchanged' } as const
  const direction = directionMap[variant ?? 'neutral']
  const ariaLabel = `${label} ${direction} by ${formatPercent(Math.abs(percent))} versus previous period`

  return (
    <span
      className={cn(changeIndicatorVariants({ variant }), className)}
      aria-label={ariaLabel}
    >
      <span aria-hidden="true">{GLYPHS[variant ?? 'neutral']}</span>
      <span data-ph-mask>{formatAmount(amount)}</span>
      <span data-ph-mask className="text-muted-foreground">({formatPercent(Math.abs(percent))})</span>
    </span>
  )
}
