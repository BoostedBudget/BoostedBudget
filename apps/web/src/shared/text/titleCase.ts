/**
 * titleCaseWords humanises a space- or underscore-separated lower-case token, e.g. Plaid's
 * "qualified dividend" → "Qualified Dividend".
 *
 * Lives in `shared/` rather than beside its first caller, and that placement is load-bearing
 * rather than tidiness (#1631). It was briefly exported from
 * `features/transactions/investmentSubcategory.ts` and imported here by
 * `shared/categories/formatPfc.ts` — a `shared/` → `features/` RUNTIME edge. `formatPfc` is in
 * the eagerly-loaded `shared` chunk, so that one import made the whole `transactions` chunk a
 * static dependency of the landing route and pushed its eager download from 542 KiB to 859 KiB
 * across 22 assets, reopening the exact regression `scripts/check-route-weight.sh` was written
 * for in #1601. A generic text helper that `shared/` needs belongs in `shared/`.
 */
export function titleCaseWords(value: string): string {
  return value
    .split(/[_\s]+/)
    .filter(Boolean)
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1).toLowerCase())
    .join(' ')
}
