import { describe, it, expect } from 'bun:test'
import type { AccountBreakdownRow } from '../types'
import type { NetWorthBreakdownFilters } from '../hooks/useNetWorthBreakdownFilters'

const { applyFilterAndSort } = await import('./applyFilterAndSort')

function makeRow(overrides: Partial<AccountBreakdownRow>): AccountBreakdownRow {
  return {
    id: 'r-1',
    name: 'Checking',
    type: 'depository',
    institutionName: 'Chase',
    currencyCode: 'USD',
    currentBalance: 1000,
    percentOfSide: 50,
    side: 'assets',
    ...overrides,
  }
}

const emptyFilters: NetWorthBreakdownFilters = { institution: [], type: [] }

describe('applyFilterAndSort', () => {
  it('returns input unchanged when no filter and no sort', () => {
    const rows = [makeRow({ id: 'r-1' }), makeRow({ id: 'r-2' })]
    const result = applyFilterAndSort(rows, emptyFilters, null)
    expect(result).toHaveLength(2)
    expect(result[0].id).toBe('r-1')
    expect(result[1].id).toBe('r-2')
  })

  it('is a pure function — does not mutate input', () => {
    const rows = [makeRow({ id: 'r-1', currentBalance: 500 }), makeRow({ id: 'r-2', currentBalance: 1000 })]
    const original = JSON.stringify(rows)
    applyFilterAndSort(rows, emptyFilters, { column: 'balance', direction: 'desc' })
    expect(JSON.stringify(rows)).toBe(original)
  })

  it('filters by institution multi-select (case-sensitive)', () => {
    const rows = [
      makeRow({ id: 'r-1', institutionName: 'Chase' }),
      makeRow({ id: 'r-2', institutionName: 'Citibank' }),
      makeRow({ id: 'r-3', institutionName: 'Chase' }),
    ]
    const result = applyFilterAndSort(rows, { ...emptyFilters, institution: ['Chase'] }, null)
    expect(result).toHaveLength(2)
    expect(result.every((r) => r.institutionName === 'Chase')).toBe(true)
  })

  it('filters by type multi-select', () => {
    const rows = [
      makeRow({ id: 'r-1', type: 'depository' }),
      makeRow({ id: 'r-2', type: 'credit' }),
      makeRow({ id: 'r-3', type: 'investment' }),
    ]
    const result = applyFilterAndSort(rows, { ...emptyFilters, type: ['depository', 'investment'] }, null)
    expect(result).toHaveLength(2)
    expect(result.map((r) => r.type)).toEqual(['depository', 'investment'])
  })

  it('filters by balance range — min only', () => {
    const rows = [
      makeRow({ id: 'r-1', currentBalance: 500 }),
      makeRow({ id: 'r-2', currentBalance: 1500 }),
      makeRow({ id: 'r-3', currentBalance: 3000 }),
    ]
    const result = applyFilterAndSort(rows, { ...emptyFilters, balanceMin: 1000 }, null)
    expect(result).toHaveLength(2)
    expect(result.every((r) => Math.abs(r.currentBalance) >= 1000)).toBe(true)
  })

  it('filters by balance range — max only', () => {
    const rows = [
      makeRow({ id: 'r-1', currentBalance: 500 }),
      makeRow({ id: 'r-2', currentBalance: 1500 }),
    ]
    const result = applyFilterAndSort(rows, { ...emptyFilters, balanceMax: 1000 }, null)
    expect(result).toHaveLength(1)
    expect(result[0].id).toBe('r-1')
  })

  it('filters by balance range — both min and max', () => {
    const rows = [
      makeRow({ id: 'r-1', currentBalance: 100 }),
      makeRow({ id: 'r-2', currentBalance: 500 }),
      makeRow({ id: 'r-3', currentBalance: 2000 }),
    ]
    const result = applyFilterAndSort(rows, { ...emptyFilters, balanceMin: 200, balanceMax: 1000 }, null)
    expect(result).toHaveLength(1)
    expect(result[0].id).toBe('r-2')
  })

  it('balance range comparison uses abs(balance) for liabilities', () => {
    const rows = [
      makeRow({ id: 'r-1', side: 'liabilities', currentBalance: -500 }),
      makeRow({ id: 'r-2', side: 'liabilities', currentBalance: 1500 }),
    ]
    const result = applyFilterAndSort(rows, { ...emptyFilters, balanceMax: 1000 }, null)
    expect(result).toHaveLength(1)
    expect(result[0].id).toBe('r-1')
  })

  it('sorts by balance desc, breaking ties by id asc', () => {
    const rows = [
      makeRow({ id: 'z-1', currentBalance: 500 }),
      makeRow({ id: 'a-2', currentBalance: 1000 }),
      makeRow({ id: 'b-3', currentBalance: 500 }),
    ]
    const result = applyFilterAndSort(rows, emptyFilters, { column: 'balance', direction: 'desc' })
    expect(result[0].id).toBe('a-2')   // 1000 first
    expect(result[1].id).toBe('b-3')   // 500 but id 'b-3' < 'z-1'
    expect(result[2].id).toBe('z-1')
  })

  it('sorts by balance asc', () => {
    const rows = [
      makeRow({ id: 'r-1', currentBalance: 3000 }),
      makeRow({ id: 'r-2', currentBalance: 1000 }),
    ]
    const result = applyFilterAndSort(rows, emptyFilters, { column: 'balance', direction: 'asc' })
    expect(result[0].id).toBe('r-2')
    expect(result[1].id).toBe('r-1')
  })

  it('sorts by string columns case-insensitively (institution)', () => {
    const rows = [
      makeRow({ id: 'r-1', institutionName: 'citibank' }),
      makeRow({ id: 'r-2', institutionName: 'Chase' }),
      makeRow({ id: 'r-3', institutionName: 'BANK' }),
    ]
    const result = applyFilterAndSort(rows, emptyFilters, { column: 'institution', direction: 'asc' })
    expect(result.map((r) => r.id)).toEqual(['r-3', 'r-2', 'r-1'])
  })

  it('sorts by name case-insensitively', () => {
    const rows = [
      makeRow({ id: 'r-1', name: 'Zebra' }),
      makeRow({ id: 'r-2', name: 'apple' }),
    ]
    const result = applyFilterAndSort(rows, emptyFilters, { column: 'name', direction: 'asc' })
    expect(result[0].id).toBe('r-2')
    expect(result[1].id).toBe('r-1')
  })

  it('sorts by percent desc', () => {
    const rows = [
      makeRow({ id: 'r-1', percentOfSide: 30 }),
      makeRow({ id: 'r-2', percentOfSide: 70 }),
    ]
    const result = applyFilterAndSort(rows, emptyFilters, { column: 'percent', direction: 'desc' })
    expect(result[0].id).toBe('r-2')
    expect(result[1].id).toBe('r-1')
  })

  it('combines filter and sort', () => {
    const rows = [
      makeRow({ id: 'r-1', institutionName: 'Chase', currentBalance: 2000 }),
      makeRow({ id: 'r-2', institutionName: 'Citibank', currentBalance: 500 }),
      makeRow({ id: 'r-3', institutionName: 'Chase', currentBalance: 500 }),
    ]
    const result = applyFilterAndSort(
      rows,
      { ...emptyFilters, institution: ['Chase'] },
      { column: 'balance', direction: 'desc' }
    )
    expect(result).toHaveLength(2)
    expect(result[0].id).toBe('r-1') // Chase, highest balance
    expect(result[1].id).toBe('r-3') // Chase, lower balance
  })

  // Issue #1569 whole-branch review, I1. A row can carry NO balance (an active account whose
  // first sync has not landed). `null` compares false against BOTH bounds — `null < 1` and
  // `null > 100` are each false — so a bare comparison silently ADMITS such a row to every
  // range while reading as correct. The Accounts page settled this: no balance is not a
  // quantity, so it satisfies no bounded range and sorts into its own group, last, in BOTH
  // directions. These rows go through the same shared helpers, so the two surfaces cannot drift
  // apart again.
  describe('a row with no balance', () => {
    const noBalance = makeRow({ id: 'r-nb', currentBalance: null, percentOfSide: null })
    const funded = makeRow({ id: 'r-f', currentBalance: 50, percentOfSide: 50 })

    it('is excluded by a minimum bound', () => {
      const result = applyFilterAndSort([noBalance, funded], { ...emptyFilters, balanceMin: 1 }, null)
      expect(result.map((r) => r.id)).toEqual(['r-f'])
    })

    it('is excluded by a maximum bound — not admitted by it', () => {
      const result = applyFilterAndSort([noBalance, funded], { ...emptyFilters, balanceMax: 100 }, null)
      expect(result.map((r) => r.id)).toEqual(['r-f'])
    })

    it('is excluded by a two-sided range', () => {
      const result = applyFilterAndSort(
        [noBalance, funded],
        { ...emptyFilters, balanceMin: 1, balanceMax: 100 },
        null
      )
      expect(result.map((r) => r.id)).toEqual(['r-f'])
    })

    it('survives a range with neither bound (that is not a predicate)', () => {
      const result = applyFilterAndSort([noBalance, funded], emptyFilters, null)
      expect(result.map((r) => r.id)).toEqual(['r-nb', 'r-f'])
    })

    it('sorts last ascending, below a negative-magnitude row', () => {
      const overdrawn = makeRow({ id: 'r-o', currentBalance: -10, percentOfSide: 10 })
      const result = applyFilterAndSort(
        [noBalance, funded, overdrawn],
        emptyFilters,
        { column: 'balance', direction: 'asc' }
      )
      expect(result.map((r) => r.id)).toEqual(['r-o', 'r-f', 'r-nb'])
    })

    it('sorts last DESCENDING too — it is neither the largest nor the smallest', () => {
      const overdrawn = makeRow({ id: 'r-o', currentBalance: -10, percentOfSide: 10 })
      const result = applyFilterAndSort(
        [noBalance, funded, overdrawn],
        emptyFilters,
        { column: 'balance', direction: 'desc' }
      )
      expect(result.map((r) => r.id)).toEqual(['r-f', 'r-o', 'r-nb'])
    })

    it('sorts last on the percent column in both directions', () => {
      const asc = applyFilterAndSort([noBalance, funded], emptyFilters, {
        column: 'percent',
        direction: 'asc',
      })
      expect(asc.map((r) => r.id)).toEqual(['r-f', 'r-nb'])
      const desc = applyFilterAndSort([noBalance, funded], emptyFilters, {
        column: 'percent',
        direction: 'desc',
      })
      expect(desc.map((r) => r.id)).toEqual(['r-f', 'r-nb'])
    })
  })
})
