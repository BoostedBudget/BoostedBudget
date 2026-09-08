import { describe, it, expect } from 'bun:test'
import {
  CONDITION_CHOICES,
  FIELD_OPERATORS,
  conditionChoiceKey,
  type ConditionField,
} from './types'

// #1625: the frontend mirrors backend `domain/condition_schema.go`. A drift here is silent —
// the UI would offer a field/operator pair the server rejects with a 422 — so it is pinned.
describe('condition vocabulary mirror (#1625)', () => {
  it('offers every pair choice as a combination the backend matrix actually accepts', () => {
    for (const choice of CONDITION_CHOICES) {
      if (!choice.field || !choice.operator) continue
      expect(
        FIELD_OPERATORS[choice.field],
        `${choice.field} does not accept ${choice.operator}`,
      ).toContain(choice.operator)
    }
  })

  it('exposes the capabilities that had no fused type name — the actual gap in #1625', () => {
    const fields = new Set(CONDITION_CHOICES.map((c) => c.field).filter(Boolean) as ConditionField[])
    // The reported case: a $1,000 Zelle to one person. merchant_name is null on those, so only
    // `name` can identify the payee, and only a signed amount can tell an outgoing $1,000 from
    // an incoming one.
    expect(fields.has('name')).toBe(true)
    expect(fields.has('signed_amount')).toBe(true)
    expect(fields.has('direction')).toBe(true)
    expect(fields.has('merchant_id')).toBe(true)
  })

  it('never offers the response-only expression sentinel as something to pick', () => {
    expect(CONDITION_CHOICES.some((c) => c.type === 'expression')).toBe(false)
  })

  it('keys every choice uniquely so the two namespaces cannot collide', () => {
    const keys = CONDITION_CHOICES.map((c) => c.key)
    expect(new Set(keys).size).toBe(keys.length)
  })
})

describe('conditionChoiceKey', () => {
  it('resolves a legacy fused condition to its own key', () => {
    expect(conditionChoiceKey({ type: 'merchant_contains', value: 'x' } as never)).toBe('merchant_contains')
  })

  it('shows a normalized pair as its fused choice, so pre- and post-rework rules look identical', () => {
    // The server normalizes merchant_contains to {merchant, contains} and emits BOTH shapes.
    expect(
      conditionChoiceKey({ type: 'merchant_contains', field: 'merchant', operator: 'contains' } as never),
    ).toBe('merchant_contains')
  })

  it('resolves a pair with no fused ancestor to its field:operator key', () => {
    expect(conditionChoiceKey({ type: 'expression', field: 'name', operator: 'contains' } as never))
      .toBe('name:contains')
  })
})
