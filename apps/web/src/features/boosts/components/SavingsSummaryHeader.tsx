import { TrendingUp, TrendingDown, Minus } from 'lucide-react'

const formatCurrency = (amount: number) =>
  new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    maximumFractionDigits: 0,
  }).format(amount)

const STAT_CARD_CLASS = 'rounded-lg border border-border bg-card p-4'
const STAT_LABEL_CLASS = 'text-xs text-muted-foreground uppercase tracking-wide mb-1'
const STAT_VALUE_CLASS = 'text-2xl font-bold'

export interface SavingsSummaryHeaderProps {
  totalMoved: number | null
  /** Month-over-month change in dollars; null means prev month was $0 (suppress display) */
  momChange: number | null
  completedCount: number | null
  activeRulesCount: number
  isLoading: boolean
}

export function SavingsSummaryHeader({
  totalMoved,
  momChange,
  completedCount,
  activeRulesCount,
  isLoading,
}: SavingsSummaryHeaderProps) {
  if (isLoading) {
    return (
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-4 animate-pulse" data-testid="summary-loading">
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className="h-16 rounded-lg bg-muted" />
        ))}
      </div>
    )
  }

  const momPositive = momChange !== null && momChange >= 0

  return (
    <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
      <div className={STAT_CARD_CLASS}>
        <p className={STAT_LABEL_CLASS}>Total Moved to Savings</p>
        <p className={`${STAT_VALUE_CLASS} text-success`} data-ph-mask>
          {totalMoved !== null ? formatCurrency(totalMoved) : '—'}
        </p>
      </div>

      <div className={STAT_CARD_CLASS}>
        <p className={STAT_LABEL_CLASS}>vs. Last Month</p>
        {momChange !== null ? (
          <p
            className={`${STAT_VALUE_CLASS} flex items-center gap-1 ${momPositive ? 'text-success' : 'text-destructive'}`}
            data-ph-mask
            data-testid="mom-change"
          >
            {momPositive ? <TrendingUp className="h-5 w-5" /> : <TrendingDown className="h-5 w-5" />}
            {momPositive ? '+' : ''}
            {formatCurrency(momChange)}
          </p>
        ) : (
          <p className={`${STAT_VALUE_CLASS} text-muted-foreground flex items-center gap-1`}>
            <Minus className="h-5 w-5" />
            No data
          </p>
        )}
      </div>

      <div className={STAT_CARD_CLASS}>
        <p className={STAT_LABEL_CLASS}>Transfers Completed</p>
        <p className={STAT_VALUE_CLASS}>{completedCount !== null ? completedCount : '—'}</p>
      </div>

      <div className={STAT_CARD_CLASS}>
        <p className={STAT_LABEL_CLASS}>Active Rules</p>
        <p className={STAT_VALUE_CLASS}>{activeRulesCount}</p>
      </div>
    </div>
  )
}
