import { TrendingUp, TrendingDown, Minus } from 'lucide-react'
import { StatCard } from '@/components/ui/stat-card'

const formatCurrency = (amount: number) =>
  new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    maximumFractionDigits: 0,
  }).format(amount)

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
      <StatCard
        label="Total Moved to Savings"
        value={totalMoved !== null ? formatCurrency(totalMoved) : '—'}
        trend="up"
        maskValue
      />

      <StatCard
        label="vs. Last Month"
        value={
          momChange !== null ? (
            <span
              className="flex items-center gap-1"
              data-testid="mom-change"
            >
              {momPositive ? (
                <>
                  <TrendingUp className="h-5 w-5 text-success" />
                  +{formatCurrency(momChange)}
                </>
              ) : (
                <>
                  <TrendingDown className="h-5 w-5 text-destructive" />
                  {formatCurrency(momChange)}
                </>
              )}
            </span>
          ) : (
            <span className="flex items-center gap-1 text-muted-foreground">
              <Minus className="h-5 w-5" />
              No data
            </span>
          )
        }
        maskValue={momChange !== null}
      />

      <StatCard
        label="Transfers Completed"
        value={completedCount !== null ? completedCount : '—'}
      />

      <StatCard
        label="Active Rules"
        value={activeRulesCount}
      />
    </div>
  )
}
