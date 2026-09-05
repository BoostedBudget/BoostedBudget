import { balanceMatchesRange, compareBalanceValues } from '@/features/accounts/utils'
import type { AccountBreakdownRow } from '../types'
import type { NetWorthBreakdownFilters, SortColumn, SortDirection } from '../hooks/useNetWorthBreakdownFilters'

const collator = new Intl.Collator('en', { sensitivity: 'base' })

function compareStrings(a: string, b: string): number {
  return collator.compare(a, b)
}

/**
 * The columns sorted as text. Declared as a `const` tuple and used BOTH as the runtime set and
 * as the source of the `StringSortColumn` type, so the two cannot drift: a column that is in the
 * type is in the set, by construction.
 */
const STRING_SORT_COLUMNS = ['institution', 'name', 'type'] as const
type StringSortColumn = (typeof STRING_SORT_COLUMNS)[number]
/** Everything `SortColumn` allows that is not sorted as text — i.e. sorted as a number. */
type NumericSortColumn = Exclude<SortColumn, StringSortColumn>

const STRING_COLS: ReadonlySet<SortColumn> = new Set<SortColumn>(STRING_SORT_COLUMNS)

function isStringColumn(col: SortColumn): col is StringSortColumn {
  return STRING_COLS.has(col)
}

/**
 * ⚠ THIS IS WHY NEITHER ACCESSOR BELOW HAS A FALLBACK VALUE (Issue #1569 whole-branch review,
 * residual to I1). Both used to end `default: return ''` / `default: return 0` — unreachable
 * today, and a silent wrong answer the moment a sixth column is added to `SortColumn`. A new
 * numeric column would have sorted at 0, landing between the overdrawn rows and the funded ones
 * instead of grouping with "no balance" — which is the EXACT defect I1 just fixed one line
 * above, reintroduced by a fallback rather than by a comparison.
 *
 * Passing the unhandled column to `assertNever` makes that unrepresentable rather than unlikely:
 * the parameter is typed `never`, so adding a case to `SortColumn` without adding a case here is
 * a COMPILE error (`bun run typecheck`), not a subtly mis-sorted table. The throw is the runtime
 * half, and it only fires if the type system was bypassed — see the tests, which cast to reach
 * it. Failing loudly there is deliberate: a wrong sort order is invisible, an exception is not.
 */
function assertNever(col: never): never {
  throw new Error(`unhandled sort column: ${String(col)}`)
}

export function getStringValue(row: AccountBreakdownRow, col: StringSortColumn): string {
  switch (col) {
    case 'institution': return row.institutionName
    case 'name': return row.name
    case 'type': return row.type
    default: return assertNever(col)
  }
}

/**
 * ⚠ Returns `null`, not 0, when the row has no balance (Issue #1569 whole-branch review, I1).
 * `Math.abs(null)` is 0, which would sort a balance-less account into the middle of the funded
 * ones — the same defect the Accounts list already fixed.
 */
export function getNumericValue(row: AccountBreakdownRow, col: NumericSortColumn): number | null {
  switch (col) {
    case 'balance': return row.currentBalance === null ? null : Math.abs(row.currentBalance)
    case 'percent': return row.percentOfSide
    default: return assertNever(col)
  }
}

export function applyFilterAndSort(
  rows: AccountBreakdownRow[],
  filters: NetWorthBreakdownFilters,
  sort: { column: SortColumn; direction: SortDirection } | null
): AccountBreakdownRow[] {
  let result = rows
  // Magnitude, because a liability's balance is negative on this surface — but `null` stays
  // `null` rather than collapsing to 0 through `Math.abs`.
  const absBalance = (r: AccountBreakdownRow) =>
    r.currentBalance === null ? null : Math.abs(r.currentBalance)

  if (filters.institution.length > 0) {
    result = result.filter((r) => filters.institution.includes(r.institutionName))
  }
  if (filters.type.length > 0) {
    result = result.filter((r) => filters.type.includes(r.type))
  }
  // The SHARED predicate from the accounts list (Issue #1569 whole-branch review, I1). Written
  // out here as a bare comparison, `null` satisfied every bounded range — `null < min` and
  // `null > max` are both false — so a balance-less account was silently admitted, the exact
  // opposite of the settled semantics, and it read as correct.
  if (filters.balanceMin !== undefined || filters.balanceMax !== undefined) {
    const range = { min: filters.balanceMin, max: filters.balanceMax }
    result = result.filter((r) => balanceMatchesRange(absBalance(r), range))
  }

  if (!sort) return result

  const { column, direction } = sort
  const sign = direction === 'asc' ? 1 : -1

  return [...result].sort((a, b) => {
    // `direction` is applied INSIDE compareBalanceValues for the numeric columns and by `sign`
    // for the string ones. Multiplying the numeric result by `sign` would flip the "no balance
    // last" grouping as well as the ordering, and "no balance" is not a quantity — it can be
    // neither the largest value nor the smallest.
    const cmp = isStringColumn(column)
      ? compareStrings(getStringValue(a, column), getStringValue(b, column)) * sign
      : compareBalanceValues(getNumericValue(a, column), getNumericValue(b, column), direction)
    return cmp !== 0 ? cmp : a.id < b.id ? -1 : a.id > b.id ? 1 : 0
  })
}
