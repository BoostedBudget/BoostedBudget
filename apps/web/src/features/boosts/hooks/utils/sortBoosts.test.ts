import { describe, it, expect } from 'bun:test'
import type { Boost } from '@boostedbudget/api-client/models'
import { sortBoosts } from './sortBoosts'

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const boost = (overrides: Record<string, any> = {}): Boost =>
  ({
    id: 'r',
    name: 'rule',
    description: '',
    enabled: true,
    boost_type: 'tag',
    trigger_config: { type: 'event' },
    priority: 0,
    last_executed_at: null,
    user_id: 'u',
    conditions: [],
    condition_logic: 'AND',
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    actions: {} as any,
    source: 'manual',
    match_count: 0,
    execution_count: 0,
    created_at: '',
    updated_at: '',
    ...overrides,
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
  }) as any as Boost

describe('sortBoosts', () => {
  it('returns input order when sortColumn is undefined', () => {
    const boosts = [boost({ id: 'a', name: 'B' }), boost({ id: 'b', name: 'A' })]
    expect(sortBoosts(boosts, undefined, 'asc').map((r) => r.id)).toEqual(['a', 'b'])
  })

  it('sorts by name ascending', () => {
    const boosts = [boost({ id: 'a', name: 'Charlie' }), boost({ id: 'b', name: 'Alpha' })]
    expect(sortBoosts(boosts, 'name', 'asc').map((r) => r.id)).toEqual(['b', 'a'])
  })

  it('sorts by name descending', () => {
    const boosts = [boost({ id: 'a', name: 'Alpha' }), boost({ id: 'b', name: 'Charlie' })]
    expect(sortBoosts(boosts, 'name', 'desc').map((r) => r.id)).toEqual(['b', 'a'])
  })

  it('sorts by priority numerically ascending', () => {
    const boosts = [boost({ id: 'a', priority: 10 }), boost({ id: 'b', priority: 2 })]
    expect(sortBoosts(boosts, 'priority', 'asc').map((r) => r.id)).toEqual(['b', 'a'])
  })

  it('sorts by priority numerically descending', () => {
    const boosts = [boost({ id: 'a', priority: 2 }), boost({ id: 'b', priority: 10 })]
    expect(sortBoosts(boosts, 'priority', 'desc').map((r) => r.id)).toEqual(['b', 'a'])
  })

  it('sorts by status with enabled last (asc = disabled first)', () => {
    const boosts = [
      boost({ id: 'a', enabled: false }),
      boost({ id: 'b', enabled: true }),
      boost({ id: 'c', enabled: false }),
    ]
    const asc = sortBoosts(boosts, 'status', 'asc').map((r) => r.id)
    expect(asc[asc.length - 1]).toBe('b')
  })

  it('sorts by trigger config type ascending', () => {
    const boosts = [
      boost({ id: 'a', trigger_config: { type: 'schedule' } as never }),
      boost({ id: 'b', trigger_config: { type: 'event' } as never }),
    ]
    expect(sortBoosts(boosts, 'trigger', 'asc').map((r) => r.id)).toEqual(['b', 'a'])
  })

  it('sorts by boost_type ascending', () => {
    const boosts = [
      boost({ id: 'a', boost_type: 'tag' }),
      boost({ id: 'b', boost_type: 'budget_alert' }),
    ]
    expect(sortBoosts(boosts, 'type', 'asc').map((r) => r.id)).toEqual(['b', 'a'])
  })

  it('sorts lastRun ascending with nulls last', () => {
    const boosts = [
      boost({ id: 'a', last_executed_at: null }),
      boost({ id: 'b', last_executed_at: '2026-01-01T00:00:00Z' }),
      boost({ id: 'c', last_executed_at: '2026-03-01T00:00:00Z' }),
    ]
    expect(sortBoosts(boosts, 'lastRun', 'asc').map((r) => r.id)).toEqual(['b', 'c', 'a'])
  })

  it('sorts lastRun descending with nulls still last (regression #1134)', () => {
    const boosts = [
      boost({ id: 'a', last_executed_at: null }),
      boost({ id: 'b', last_executed_at: '2026-01-01T00:00:00Z' }),
      boost({ id: 'c', last_executed_at: '2026-03-01T00:00:00Z' }),
    ]
    expect(sortBoosts(boosts, 'lastRun', 'desc').map((r) => r.id)).toEqual(['c', 'b', 'a'])
  })

  it('does not mutate the input array', () => {
    const boosts = [boost({ id: 'a', name: 'Z' }), boost({ id: 'b', name: 'A' })]
    sortBoosts(boosts, 'name', 'asc')
    expect(boosts.map((r) => r.id)).toEqual(['a', 'b'])
  })
})
