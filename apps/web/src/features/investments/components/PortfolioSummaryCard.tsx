/**
 * PortfolioSummaryCard — KPI stat tiles for the portfolio summary.
 * Issue #1296: Displays total value, gain/loss, and day change.
 *
 * Handles nullable day_change/day_change_pct (Plaid may not always provide intraday data)
 * by rendering "--" and suppressing the ChangeIndicator arrow.
 */

import { StatCard } from '@/components/ui/stat-card'
import { ChangeIndicator } from '@/features/net-worth/components/ChangeIndicator'
import { formatCurrencyFull } from '@/shared/charts/formatters'
import type { PortfolioSummary } from '@boostedbudget/api-client/models/portfolioSummary'

interface PortfolioSummaryCardProps {
  summary: PortfolioSummary | undefined
}

function Skeleton() {
  return <div className="animate-pulse h-6 w-24 rounded-md bg-muted" />
}

type ChangeVariant = 'positive' | 'negative' | 'neutral'

function toChangeVariant(value: number): ChangeVariant {
  return value > 0 ? 'positive' : value < 0 ? 'negative' : 'neutral'
}

function toStatTrend(variant: ChangeVariant): 'up' | 'down' | 'neutral' {
  const map = { positive: 'up', negative: 'down', neutral: 'neutral' } as const
  return map[variant]
}

/**
 * Renders 3 side-by-side stat cards:
 * - Total Value (masked)
 * - Total Gain/Loss with change indicator (masked)
 * - Day Change with change indicator, or "--" when null (masked)
 */
export function PortfolioSummaryCard({ summary }: PortfolioSummaryCardProps) {
  if (!summary) {
    return (
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
        {[0, 1, 2].map((i) => (
          <div key={i} className="rounded-xl border p-4 space-y-2" aria-label="Loading portfolio summary">
            <Skeleton />
            <Skeleton />
          </div>
        ))}
      </div>
    )
  }

  const gainVariant = toChangeVariant(summary.total_gain_loss)
  const dayVariant = summary.day_change != null ? toChangeVariant(summary.day_change) : 'neutral'

  return (
    <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
      <StatCard
        label="Total Value"
        value={<span data-ph-mask>{formatCurrencyFull(summary.total_value)}</span>}
      />

      <StatCard
        label="Total Gain / Loss"
        trend={toStatTrend(gainVariant)}
        value={
          <div className="flex flex-col gap-1">
            <span data-ph-mask className="text-2xl font-bold">
              {formatCurrencyFull(summary.total_gain_loss)}
            </span>
            {summary.total_gain_loss_pct != null && (
              <ChangeIndicator
                variant={gainVariant}
                amount={summary.total_gain_loss}
                percent={summary.total_gain_loss_pct}
                label="Portfolio gain/loss"
              />
            )}
          </div>
        }
      />

      <StatCard
        label="Day Change"
        trend={summary.day_change != null ? toStatTrend(dayVariant) : undefined}
        value={
          summary.day_change != null && summary.day_change_pct != null ? (
            <div className="flex flex-col gap-1">
              <span data-ph-mask className="text-2xl font-bold">
                {formatCurrencyFull(summary.day_change)}
              </span>
              <ChangeIndicator
                variant={dayVariant}
                amount={summary.day_change}
                percent={summary.day_change_pct}
                label="Day change"
              />
            </div>
          ) : (
            <span data-ph-mask className="text-2xl font-bold text-muted-foreground">
              --
            </span>
          )
        }
      />
    </div>
  )
}
