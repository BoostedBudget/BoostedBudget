import type { ActivityType } from './types'
import { titleCaseWords } from '@/shared/text/titleCase'

/**
 * SECURITY_TYPE_ACRONYM_LABELS maps raw `securities.type` tokens that are acronyms to their
 * canonical uppercase form (etf → "ETF") rather than a naive title-case ("Etf"). Shared so the
 * Holdings table and the allocation donut render the SAME label for the SAME value — see
 * securityTypeLabel below.
 */
export const SECURITY_TYPE_ACRONYM_LABELS: Readonly<Record<string, string>> = {
  etf: 'ETF',
  reit: 'REIT',
  mbs: 'MBS',
  abs: 'ABS',
}

/**
 * securityTypeLabel humanises a `securities.type` value for display (#1631).
 *
 * `security_type` is a raw lowercase Plaid/provider token ("etf", "mutual fund", "stock") and
 * `securities.type` is NULL for some rows (COALESCE'd to 'unknown' in the allocation SQL, but
 * never assigned on the live holdings join — HoldingView.SecurityType stays nil). Before #1631,
 * HoldingsTable's Type column had no `render` at all, so it printed the raw token verbatim, and a
 * nil/empty value rendered as a blank cell — despite HoldingView's own comment promising
 * `nil -> "unknown"`.
 *
 * `fallback` lets each caller choose its own copy for the empty case: the Holdings table wants
 * "Unknown" (matching the promised value), the allocation donut wants "Other" (an aggregation
 * bucket name, not a security-type label).
 */
export function securityTypeLabel(value: string | null | undefined, fallback: string): string {
  const trimmed = (value ?? '').trim()
  if (!trimmed || trimmed.toLowerCase() === 'unknown') return fallback
  const lower = trimmed.toLowerCase()
  if (SECURITY_TYPE_ACRONYM_LABELS[lower]) return SECURITY_TYPE_ACRONYM_LABELS[lower]
  return titleCaseWords(lower)
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
