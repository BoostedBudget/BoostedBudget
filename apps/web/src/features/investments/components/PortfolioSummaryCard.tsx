/**
 * PortfolioSummaryCard — KPI stat tiles for the portfolio summary.
 * Issue #1296: Displays total value, gain/loss, and day change.
 *
 * Handles nullable day_change/day_change_pct (Plaid may not always provide intraday data)
 * by rendering "--" and suppressing the ChangeIndicator arrow.
 */

import { StatCard } from '@/components/ui/stat-card'
import { ChangeIndicator } from '@/features/net-worth/components/ChangeIndicator'
import { formatPortfolioAmount, isCurrencyUnnameable } from '../utils/portfolioCurrency'
import { MixedCurrencyBadge } from './MixedCurrencyBadge'
import type { PortfolioSummary } from '@boostedbudget/api-client/models/portfolioSummary'

interface PortfolioSummaryCardProps {
  summary: PortfolioSummary | undefined
}

function Skeleton() {
  return <div className="animate-pulse h-6 w-24 rounded-md bg-muted" />
}

function getVariantAndTrend(
  value: number,
): { variant: 'positive' | 'negative' | 'neutral'; trend: 'up' | 'down' | 'neutral' } {
  const variant = value > 0 ? 'positive' : value < 0 ? 'negative' : 'neutral'
  const trend = variant === 'positive' ? 'up' : variant === 'negative' ? 'down' : 'neutral'
  return { variant, trend }
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

  // #1631 CX1: every figure on this card is rendered in the currency the API NAMED, and
  // unlabelled when it refused to name one. The card used to hard-code the dollar, so a GBP
  // portfolio read "$52,340.50" and a mixed one read a dollar figure that is not a quantity of any
  // currency at all.
  const money = (value: number) => formatPortfolioAmount(value, summary)
  // Whole-currency form for the change indicators, which show a delta rather than a balance.
  // Hoisted rather than written inline at both call sites: one shared closure instead of two.
  const moneyWhole = (value: number) => formatPortfolioAmount(value, summary, 0)
  const mixed = isCurrencyUnnameable(summary)

  const { variant: gainVariant, trend: gainTrend } = getVariantAndTrend(summary.total_gain_loss)
  const dayChange = summary.day_change
  const dayVariantAndTrend = dayChange != null ? getVariantAndTrend(dayChange) : null
  // #1631 Gap B: a REFUSAL (the prior snapshot and today's total disagree on currency, or either
  // is a currency-blind sum -- see domain.PortfolioSummary.DayChangeUnavailableReason) is a
  // different claim than missing data, and must not collapse into the same bare "--". Mirrors
  // HoldingsTable's renderDayChange treatment of the identical reason on HoldingView.
  const dayChangeRefused = summary.day_change_unavailable_reason === 'currency_mismatch'

  return (
    <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
      <StatCard
        label="Total Value"
        value={
          <span className="flex flex-col gap-1">
            <span data-ph-mask>{money(summary.total_value)}</span>
            {/* The badge carries no figure, so it is deliberately OUTSIDE data-ph-mask: it is a
                structural label, not PII. */}
            {mixed && <MixedCurrencyBadge className="self-start" />}
          </span>
        }
      />

      <StatCard
        label="Total Gain / Loss"
        trend={gainTrend}
        value={
          <span className="flex flex-col gap-1">
            <span data-ph-mask className="text-2xl font-bold">
              {money(summary.total_gain_loss)}
            </span>
            {summary.total_gain_loss_pct != null && (
              <ChangeIndicator
                variant={gainVariant}
                amount={summary.total_gain_loss}
                percent={summary.total_gain_loss_pct}
                label="Portfolio gain/loss"
                formatAmount={moneyWhole}
              />
            )}
          </span>
        }
      />

      <StatCard
        label="Day Change"
        trend={dayVariantAndTrend?.trend}
        value={
          dayChange != null ? (
            <span className="flex flex-col gap-1">
              <span data-ph-mask className="text-2xl font-bold">
                {money(dayChange)}
              </span>
              {summary.day_change_pct != null && dayVariantAndTrend && (
                <ChangeIndicator
                  variant={dayVariantAndTrend.variant}
                  amount={dayChange}
                  percent={summary.day_change_pct}
                  label="Day change"
                  formatAmount={moneyWhole}
                />
              )}
            </span>
          ) : dayChangeRefused ? (
            <span data-ph-mask className="text-sm text-muted-foreground">
              Currency mismatch — day change not available
            </span>
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
