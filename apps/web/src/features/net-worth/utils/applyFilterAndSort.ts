import { balanceMatchesRange, compareBalanceValues } from '@/features/accounts/utils'
import type { AccountBreakdownRow } from '../types'
import type { NetWorthBreakdownFilters, SortColumn, SortDirection } from '../hooks/useNetWorthBreakdownFilters'

const collator = new Intl.Collator('en', { sensitivity: 'base' })

function compareStrings(a: string, b: string): number {
  return collator.compare(a, b)
}

function getStringValue(row: AccountBreakdownRow, col: SortColumn): string {
  switch (col) {
    case 'institution': return row.institutionName
    case 'name': return row.name
    case 'type': return row.type
    default: return ''
  }
}

/**
 * ⚠ Returns `null`, not 0, when the row has no balance (Issue #1569 whole-branch review, I1).
 * `Math.abs(null)` is 0, which would sort a balance-less account into the middle of the funded
 * ones — the same defect the Accounts list already fixed.
 */
function getNumericValue(row: AccountBreakdownRow, col: SortColumn): number | null {
  switch (col) {
    case 'balance': return row.currentBalance === null ? null : Math.abs(row.currentBalance)
    case 'percent': return row.percentOfSide
    default: return 0
  }
}

const STRING_COLS = new Set<SortColumn>(['institution', 'name', 'type'])

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
    const cmp = STRING_COLS.has(column)
      ? compareStrings(getStringValue(a, column), getStringValue(b, column)) * sign
      : compareBalanceValues(getNumericValue(a, column), getNumericValue(b, column), direction)
    return cmp !== 0 ? cmp : a.id < b.id ? -1 : a.id > b.id ? 1 : 0
  })
}
