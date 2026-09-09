import { describe, it, expect } from 'bun:test'
import { RuleConditionField } from '@boostedbudget/api-client/models/ruleConditionField'
import { RuleConditionOperator } from '@boostedbudget/api-client/models/ruleConditionOperator'
import {
  CONDITION_CHOICES,
  FIELD_OPERATORS,
  conditionChoiceKey,
  type ConditionField,
} from './types'

// #1625: the frontend mirrors backend `domain/condition_schema.go`. A drift here is silent —
// the UI would offer a field/operator pair the server rejects with a 422.
//
// ⚠ The pin that matters is against RuleConditionField / RuleConditionOperator, which are
// GENERATED from the Go schema via openapi -> orval. An earlier revision of this file claimed to
// pin the mirror while comparing CONDITION_CHOICES against FIELD_OPERATORS — two FRONTEND
// constants. That cannot fail when the backend changes, which is the only drift worth guarding.
// The internal-consistency checks below are still useful; they are just not the pin.
describe('condition vocabulary pinned to the generated backend enums (#1625)', () => {
  it('offers exactly the fields the backend declares', () => {
    expect(Object.keys(FIELD_OPERATORS).sort()).toEqual(Object.values(RuleConditionField).sort())
  })

  // Task 4: the cross-language pin above (and the old `types.test.ts`) only ever asserted that
  // every OFFERED pair is legal — never that every LEGAL field is offered. That one-directional
  // gap is exactly how `effective_merchant` shipped to the domain/API/OpenAPI/frontend TYPE and
  // then sat unreachable from the dropdown: nothing failed. This asserts the missing direction,
  // against the GENERATED enum (not a hand-written copy), so a future field added to the backend
  // schema and mirrored into `ConditionField`/`FIELD_OPERATORS` but never wired into a
  // CONDITION_CHOICES entry fails here instead of shipping silently unreachable again.
  it('makes every backend-declared field reachable through CONDITION_CHOICES', () => {
    const reachableFields = new Set(CONDITION_CHOICES.map((c) => c.field).filter(Boolean))
    for (const field of Object.values(RuleConditionField)) {
      expect(reachableFields, `${field} is a legal RuleConditionField but no CONDITION_CHOICES entry offers it`).toContain(field)
    }
  })

  it('never offers an operator the backend does not define', () => {
    const backendOps = new Set<string>(Object.values(RuleConditionOperator))
    for (const [field, ops] of Object.entries(FIELD_OPERATORS)) {
      for (const op of ops) {
        expect(backendOps, `${field} offers ${op}, which is absent from the backend enum`).toContain(op)
      }
    }
  })
})

describe('condition vocabulary internal consistency (#1625)', () => {
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
