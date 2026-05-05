import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

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

export function ChangeIndicator({ variant = 'neutral', amount, percent, className }: ChangeIndicatorProps) {
  const directionMap = { positive: 'up', negative: 'down', neutral: 'unchanged' } as const
  const direction = directionMap[variant ?? 'neutral']
  const ariaLabel = `Net worth ${direction} ${formatAmount(amount)} (${Math.abs(percent).toFixed(2)}%) versus previous period`
  const glyph = GLYPHS[variant ?? 'neutral']

  return (
    <span
      className={cn(changeIndicatorVariants({ variant }), className)}
      aria-label={ariaLabel}
    >
      <span aria-hidden="true">{glyph}</span>
      <span>{formatAmount(amount)}</span>
      <span className="text-muted-foreground">({Math.abs(percent).toFixed(2)}%)</span>
    </span>
  )
}
