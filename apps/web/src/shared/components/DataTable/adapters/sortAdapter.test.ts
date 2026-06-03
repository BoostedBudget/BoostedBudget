import { describe, it, expect } from 'bun:test'
import { bbSortToTanStack, tanStackSortToBb } from './sortAdapter'
import type { SortState } from '../types'

describe('bbSortToTanStack', () => {
  it('null → []', () => {
    expect(bbSortToTanStack(null)).toEqual([])
  })

  it('asc → [{id, desc: false}]', () => {
    expect(bbSortToTanStack({ columnId: 'amount', direction: 'asc' })).toEqual([
      { id: 'amount', desc: false },
    ])
  })

  it('desc → [{id, desc: true}]', () => {
    expect(bbSortToTanStack({ columnId: 'amount', direction: 'desc' })).toEqual([
      { id: 'amount', desc: true },
    ])
  })
})

describe('tanStackSortToBb', () => {
  it('[] → null', () => {
    expect(tanStackSortToBb([])).toBeNull()
  })

  it('[{id, desc: false}] → {columnId, direction: asc}', () => {
    expect(tanStackSortToBb([{ id: 'amount', desc: false }])).toEqual({
      columnId: 'amount',
      direction: 'asc',
    })
  })

  it('[{id, desc: true}] → {columnId, direction: desc}', () => {
    expect(tanStackSortToBb([{ id: 'amount', desc: true }])).toEqual({
      columnId: 'amount',
      direction: 'desc',
    })
  })
})

describe('sort adapter round-trip', () => {
  it('null survives round-trip', () => {
    expect(tanStackSortToBb(bbSortToTanStack(null))).toBeNull()
  })

  it('asc sort survives round-trip', () => {
    const s: SortState = { columnId: 'amount', direction: 'asc' }
    expect(tanStackSortToBb(bbSortToTanStack(s))).toEqual(s)
  })

  it('desc sort survives round-trip', () => {
    const s: SortState = { columnId: 'name', direction: 'desc' }
    expect(tanStackSortToBb(bbSortToTanStack(s))).toEqual(s)
  })
})
