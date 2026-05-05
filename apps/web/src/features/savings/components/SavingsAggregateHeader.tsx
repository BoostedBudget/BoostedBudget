/**
 * SavingsAggregateHeader
 * Issue #954: Savings page — T7
 *
 * Aggregates savings goals across currencies and renders a header card.
 * For a single currency, renders a single-row summary.
 * For multiple currencies, renders one row per currency + total counts.
 */

import type { SavingsGoal } from '../types'

export interface SavingsAggregateHeaderProps {
  goals: SavingsGoal[]
}

interface CurrencyGroup {
  currencyCode: string
  totalSaved: number
  totalTarget: number
  overallProgress: number
}

function formatCurrency(amount: number, currencyCode: string): string {
  try {
    return new Intl.NumberFormat(undefined, {
      style: 'currency',
      currency: currencyCode,
    }).format(amount)
  } catch {
    return `${currencyCode} ${amount.toFixed(2)}`
  }
}

function groupByCurrency(goals: SavingsGoal[]): CurrencyGroup[] {
  const map = new Map<string, { saved: number; target: number }>()

  for (const goal of goals) {
    const key = goal.currency_code
    const existing = map.get(key) ?? { saved: 0, target: 0 }
    map.set(key, {
      saved: existing.saved + goal.current_amount,
      target: existing.target + goal.target_amount,
    })
  }

  return Array.from(map.entries()).map(([currencyCode, { saved, target }]) => ({
    currencyCode,
    totalSaved: saved,
    totalTarget: target,
    overallProgress:
      target === 0 ? 0 : Math.min(Math.round((saved / target) * 100), 100),
  }))
}

export function SavingsAggregateHeader({ goals }: SavingsAggregateHeaderProps) {
  const groups = groupByCurrency(goals)
  const activeCount = goals.filter((g) => !g.is_completed).length
  const completedCount = goals.filter((g) => g.is_completed).length
  const isMultiCurrency = groups.length > 1

  if (goals.length === 0) {
    return (
      <section
        aria-label="Savings overview"
        className="bg-card border border-border rounded-xl p-6 mb-6"
      >
        <p className="text-sm text-muted-foreground">No savings goals yet.</p>
      </section>
    )
  }

  return (
    <section
      aria-label="Savings overview"
      className="bg-card border border-border rounded-xl p-6 mb-6"
    >
      {isMultiCurrency ? (
        <div>
          <div className="space-y-2 mb-4" role="list" aria-label="Savings by currency">
            {groups.map((group) => (
              <div
                key={group.currencyCode}
                role="listitem"
                className="flex items-center gap-3 text-sm"
              >
                <span className="font-medium text-muted-foreground w-12">
                  {group.currencyCode}:
                </span>
                <span
                  data-ph-mask
                  aria-label={`${group.currencyCode} saved amount`}
                  className="font-bold text-foreground"
                >
                  {formatCurrency(group.totalSaved, group.currencyCode)}
                </span>
                <span className="text-muted-foreground">/</span>
                <span
                  data-ph-mask
                  aria-label={`${group.currencyCode} target amount`}
                  className="text-muted-foreground"
                >
                  {formatCurrency(group.totalTarget, group.currencyCode)}
                </span>
                <span className="inline-flex items-center rounded-full bg-primary/10 px-2 py-0.5 text-xs font-medium text-primary">
                  {group.overallProgress}%
                </span>
              </div>
            ))}
          </div>
          <div className="border-t border-border pt-3 text-sm text-muted-foreground">
            <span aria-label={`${activeCount} active goals`}>{activeCount} active</span>
            {' · '}
            <span aria-label={`${completedCount} completed goals`}>{completedCount} completed</span>
          </div>
        </div>
      ) : (
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <span
            data-ph-mask
            aria-label="Total saved"
            className="text-2xl font-bold text-foreground"
          >
            {formatCurrency(groups[0].totalSaved, groups[0].currencyCode)}
          </span>
          <span className="text-muted-foreground text-sm">saved /</span>
          <span
            data-ph-mask
            aria-label="Savings target"
            className="text-base text-muted-foreground"
          >
            {formatCurrency(groups[0].totalTarget, groups[0].currencyCode)}
          </span>
          <span className="text-muted-foreground text-sm">target</span>
          <span className="inline-flex items-center rounded-full bg-primary/10 px-2.5 py-0.5 text-sm font-medium text-primary">
            {groups[0].overallProgress}%
          </span>
          <span className="text-muted-foreground text-sm">·</span>
          <span
            className="text-sm text-muted-foreground"
            aria-label={`${activeCount} active goals`}
          >
            {activeCount} active
          </span>
          <span className="text-muted-foreground text-sm">·</span>
          <span
            className="text-sm text-muted-foreground"
            aria-label={`${completedCount} completed goals`}
          >
            {completedCount} completed
          </span>
        </div>
      )}
    </section>
  )
}
