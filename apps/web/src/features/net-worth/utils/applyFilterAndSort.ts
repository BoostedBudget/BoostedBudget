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

function getNumericValue(row: AccountBreakdownRow, col: SortColumn): number {
  switch (col) {
    case 'balance': return Math.abs(row.currentBalance)
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
  const absBalance = (r: AccountBreakdownRow) => Math.abs(r.currentBalance)

  if (filters.institution.length > 0) {
    result = result.filter((r) => filters.institution.includes(r.institutionName))
  }
  if (filters.type.length > 0) {
    result = result.filter((r) => filters.type.includes(r.type))
  }
  if (filters.balanceMin !== undefined) {
    result = result.filter((r) => absBalance(r) >= filters.balanceMin!)
  }
  if (filters.balanceMax !== undefined) {
    result = result.filter((r) => absBalance(r) <= filters.balanceMax!)
  }

  if (!sort) return result

  const { column, direction } = sort
  const sign = direction === 'asc' ? 1 : -1

  return [...result].sort((a, b) => {
    const cmp = STRING_COLS.has(column)
      ? compareStrings(getStringValue(a, column), getStringValue(b, column))
      : getNumericValue(a, column) - getNumericValue(b, column)
    return cmp !== 0 ? cmp * sign : a.id < b.id ? -1 : a.id > b.id ? 1 : 0
  })
}
