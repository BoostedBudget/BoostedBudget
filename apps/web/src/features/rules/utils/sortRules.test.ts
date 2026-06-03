import { describe, it, expect } from 'bun:test'
import type { AutomationRule } from '@boostedbudget/api-client/models'
import { sortRules } from './sortRules'

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const rule = (overrides: Record<string, any> = {}): AutomationRule =>
  ({
    id: 'r',
    name: 'rule',
    description: '',
    enabled: true,
    rule_type: 'tag',
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
  }) as any as AutomationRule

describe('sortRules', () => {
  it('returns input order when sortColumn is undefined', () => {
    const rules = [rule({ id: 'a', name: 'B' }), rule({ id: 'b', name: 'A' })]
    expect(sortRules(rules, undefined, 'asc').map((r) => r.id)).toEqual(['a', 'b'])
  })

  it('sorts by name ascending', () => {
    const rules = [rule({ id: 'a', name: 'Charlie' }), rule({ id: 'b', name: 'Alpha' })]
    expect(sortRules(rules, 'name', 'asc').map((r) => r.id)).toEqual(['b', 'a'])
  })

  it('sorts by name descending', () => {
    const rules = [rule({ id: 'a', name: 'Alpha' }), rule({ id: 'b', name: 'Charlie' })]
    expect(sortRules(rules, 'name', 'desc').map((r) => r.id)).toEqual(['b', 'a'])
  })

  it('sorts by priority numerically ascending', () => {
    const rules = [rule({ id: 'a', priority: 10 }), rule({ id: 'b', priority: 2 })]
    expect(sortRules(rules, 'priority', 'asc').map((r) => r.id)).toEqual(['b', 'a'])
  })

  it('sorts by priority numerically descending', () => {
    const rules = [rule({ id: 'a', priority: 2 }), rule({ id: 'b', priority: 10 })]
    expect(sortRules(rules, 'priority', 'desc').map((r) => r.id)).toEqual(['b', 'a'])
  })

  it('sorts by status with enabled last (asc = disabled first)', () => {
    const rules = [
      rule({ id: 'a', enabled: false }),
      rule({ id: 'b', enabled: true }),
      rule({ id: 'c', enabled: false }),
    ]
    const asc = sortRules(rules, 'status', 'asc').map((r) => r.id)
    expect(asc[asc.length - 1]).toBe('b')
  })

  it('sorts by trigger config type ascending', () => {
    const rules = [
      rule({ id: 'a', trigger_config: { type: 'schedule' } as never }),
      rule({ id: 'b', trigger_config: { type: 'event' } as never }),
    ]
    expect(sortRules(rules, 'trigger', 'asc').map((r) => r.id)).toEqual(['b', 'a'])
  })

  it('sorts by rule_type ascending', () => {
    const rules = [
      rule({ id: 'a', rule_type: 'tag' }),
      rule({ id: 'b', rule_type: 'budget_alert' }),
    ]
    expect(sortRules(rules, 'type', 'asc').map((r) => r.id)).toEqual(['b', 'a'])
  })

  it('sorts lastRun ascending with nulls last', () => {
    const rules = [
      rule({ id: 'a', last_executed_at: null }),
      rule({ id: 'b', last_executed_at: '2026-01-01T00:00:00Z' }),
      rule({ id: 'c', last_executed_at: '2026-03-01T00:00:00Z' }),
    ]
    expect(sortRules(rules, 'lastRun', 'asc').map((r) => r.id)).toEqual(['b', 'c', 'a'])
  })

  it('sorts lastRun descending with nulls still last (regression #1134)', () => {
    const rules = [
      rule({ id: 'a', last_executed_at: null }),
      rule({ id: 'b', last_executed_at: '2026-01-01T00:00:00Z' }),
      rule({ id: 'c', last_executed_at: '2026-03-01T00:00:00Z' }),
    ]
    expect(sortRules(rules, 'lastRun', 'desc').map((r) => r.id)).toEqual(['c', 'b', 'a'])
  })

  it('does not mutate the input array', () => {
    const rules = [rule({ id: 'a', name: 'Z' }), rule({ id: 'b', name: 'A' })]
    sortRules(rules, 'name', 'asc')
    expect(rules.map((r) => r.id)).toEqual(['a', 'b'])
  })
})
