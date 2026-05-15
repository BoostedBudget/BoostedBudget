import { describe, it, expect } from 'bun:test'
import { bbFiltersToTanStack, tanStackFiltersToBb } from './filterAdapter'
import type { FilterState } from '../types'

describe('bbFiltersToTanStack', () => {
  it('{} → []', () => {
    expect(bbFiltersToTanStack({})).toEqual([])
  })

  it('string[] value → [{id, value: string[]}]', () => {
    expect(bbFiltersToTanStack({ account: ['bank1', 'bank2'] })).toEqual([
      { id: 'account', value: ['bank1', 'bank2'] },
    ])
  })

  it('range value → [{id, value: {min, max}}]', () => {
    expect(bbFiltersToTanStack({ amount: { min: 10, max: 100 } })).toEqual([
      { id: 'amount', value: { min: 10, max: 100 } },
    ])
  })

  it('null value → [] (dropped)', () => {
    expect(bbFiltersToTanStack({ amount: null })).toEqual([])
  })

  it('multiple entries: non-null kept, null dropped', () => {
    const result = bbFiltersToTanStack({
      account: ['bank1'],
      amount: null,
    })
    expect(result).toEqual([{ id: 'account', value: ['bank1'] }])
  })
})

describe('tanStackFiltersToBb', () => {
  it('[] → {}', () => {
    expect(tanStackFiltersToBb([])).toEqual({})
  })

  it('entries → FilterState', () => {
    const result = tanStackFiltersToBb([
      { id: 'account', value: ['bank1'] },
      { id: 'amount', value: null },
    ])
    expect(result).toEqual({ account: ['bank1'], amount: null })
  })
})

describe('filter adapter round-trip', () => {
  it('empty state survives round-trip', () => {
    const f: FilterState = {}
    // bbFiltersToTanStack({}) = [] → tanStackFiltersToBb([]) = {}
    expect(tanStackFiltersToBb(bbFiltersToTanStack(f))).toEqual(f)
  })

  it('string[] value survives round-trip', () => {
    const f: FilterState = { account: ['bank1', 'bank2'] }
    expect(tanStackFiltersToBb(bbFiltersToTanStack(f))).toEqual(f)
  })

  it('range value survives round-trip', () => {
    const f: FilterState = { amount: { min: 10, max: 100 } }
    expect(tanStackFiltersToBb(bbFiltersToTanStack(f))).toEqual(f)
  })

  it('values preserved regardless of key order', () => {
    const f: FilterState = { b: ['x'], a: ['y'] }
    const roundTripped = tanStackFiltersToBb(bbFiltersToTanStack(f))
    expect(roundTripped['a']).toEqual(['y'])
    expect(roundTripped['b']).toEqual(['x'])
  })

  it('does not restore null keys on round-trip (documented asymmetry)', () => {
    const original: FilterState = { account: ['bank1'], amount: null }
    const roundTripped = tanStackFiltersToBb(bbFiltersToTanStack(original))
    // null keys are dropped — 'amount' will be absent, not null
    expect(roundTripped).toEqual({ account: ['bank1'] })
    expect('amount' in roundTripped).toBe(false)
  })
})
