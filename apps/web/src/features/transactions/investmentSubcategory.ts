import type { ActivityType } from './types'

/**
 * titleCaseWords humanises a space- or underscore-separated lower-case token, e.g. Plaid's
 * "qualified dividend" → "Qualified Dividend".
 *
 * Exported because three sites needed one and each had grown its own private copy: formatPfc.ts,
 * AllocationDonutChart.tsx, and (before #1631) nothing at all for investment subtypes.
 */
export function titleCaseWords(value: string): string {
  return value
    .split(/[_\s]+/)
    .filter(Boolean)
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1).toLowerCase())
    .join(' ')
}

/**
 * investmentSubcategoryLabel derives the Sub-category cell for an investment row.
 *
 * Before #1631 the Sub-category column had no investment branch at all: it cast the row to a
 * banking Transaction and read PFC fields the feed's UNION hard-codes NULL, so it was always "—"
 * even though `investment_subtype` was stored, queried, serialised and typed on the client.
 *
 * ⚠ Where the backend's structural rule fired, the RAW subtype is misleading rather than merely
 * terse: a reinvestment's stored subtype is literally "withdrawal" and a dividend's is "deposit",
 * because this brokerage reports an ETF distribution as a pair of cash movements. Rendering those
 * verbatim would read "Reinvest / Withdrawal" and "Dividend / Deposit", which describes the cash
 * mechanics and hides the meaning. So those two cases get the derived label — consistent with the
 * Category column, which is already a derived value ("Reinvest" is not in the provider data
 * either).
 *
 * Every other row keeps the humanised provider subtype ("Qualified Dividend", "Management Fee"),
 * so provider detail is never discarded.
 */
export function investmentSubcategoryLabel(
  activityType: ActivityType | string | null | undefined,
  subtype: string | null | undefined,
): string | null {
  const raw = subtype?.trim().toLowerCase() ?? ''

  if (activityType === 'reinvest' && raw === 'withdrawal') return 'Dividend Reinvestment'
  if (activityType === 'dividend' && raw === 'deposit') return 'Dividend'

  if (!raw) return null
  return titleCaseWords(raw)
}
