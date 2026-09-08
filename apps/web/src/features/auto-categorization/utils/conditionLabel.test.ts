import { describe, it, expect } from 'bun:test'
import { conditionLabel } from './conditionLabel'
import { formatConditionValue } from './formatConditionValue'

describe('conditionLabel (#1625)', () => {
  it('composes the label from field + operator', () => {
    expect(conditionLabel({ field: 'name', operator: 'contains' })).toBe('Description contains')
    expect(conditionLabel({ field: 'signed_amount', operator: 'gt' })).toBe('Amount (signed) is greater than')
    expect(conditionLabel({ field: 'direction', operator: 'eq' })).toBe('Money in / out is')
  })

  // Frozen definition_snapshot rows are legacy-shaped forever — they are deliberately not
  // normalized on read — so the fused fallback is permanent, not transitional.
  it('falls back to the legacy fused type for a frozen snapshot', () => {
    expect(conditionLabel({ type: 'merchant_contains' })).toBe('Merchant contains')
    expect(conditionLabel({ type: 'amount_range' })).toBe('Amount between')
  })

  it('prefers the pair when both are present', () => {
    expect(conditionLabel({ type: 'merchant_contains', field: 'name', operator: 'contains' }))
      .toBe('Description contains')
  })

  it('never renders the bare expression sentinel at the user', () => {
    expect(conditionLabel({ type: 'expression' })).toBe('Condition')
  })
})

describe('formatConditionValue partial ranges (#1625)', () => {
  it('renders a min-only range instead of [object Object]', () => {
    const out = formatConditionValue({ type: 'amount_range', value: { min: 100 } })
    expect(out).not.toContain('[object Object]')
    expect(out).toBe('at least 100')
  })

  it('renders a max-only range', () => {
    expect(formatConditionValue({ type: 'amount_range', value: { max: 50 } })).toBe('at most 50')
  })

  it('still renders a full range unchanged', () => {
    expect(formatConditionValue({ type: 'amount_range', value: { min: 10, max: 20 } })).toBe('10 - 20')
  })
})

// Codex review: conditionLabel shipped with tests but was not actually WIRED into the card or
// the history panel, so a condition with no fused ancestor rendered as the bare word
// "Condition" — e.g. "Condition jack" instead of "Description contains jack". These pin the
// composition that the two render sites now rely on.
describe('the expression case the render sites had been getting wrong', () => {
  it('renders a name condition as its field + operator, not as "Condition"', () => {
    const cond = { type: 'expression' as const, field: 'name' as const, operator: 'contains' as const }
    expect(conditionLabel(cond)).toBe('Description contains')
    expect(conditionLabel(cond)).not.toBe('Condition')
  })

  it('renders every pair that has no fused ancestor', () => {
    expect(conditionLabel({ type: 'expression', field: 'merchant_id', operator: 'eq' } as never)).toBe('Merchant ID is')
    expect(conditionLabel({ type: 'expression', field: 'direction', operator: 'eq' } as never)).toBe('Money in / out is')
    expect(conditionLabel({ type: 'expression', field: 'signed_amount', operator: 'gt' } as never))
      .toBe('Amount (signed) is greater than')
  })
})
