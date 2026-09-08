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
