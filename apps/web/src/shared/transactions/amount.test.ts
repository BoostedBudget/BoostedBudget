import { describe, it, expect } from 'bun:test'
import { isOutflow, isInflow, formatTransactionAmount } from './amount'

describe('isOutflow', () => {
  it('returns true for positive amounts (outflows/spending)', () => {
    expect(isOutflow(25.5)).toBe(true)
  })

  it('returns false for negative amounts (inflows)', () => {
    expect(isOutflow(-25.5)).toBe(false)
  })

  it('returns false for zero', () => {
    expect(isOutflow(0)).toBe(false)
  })
})

describe('isInflow', () => {
  it('returns true for negative amounts (inflows/refunds)', () => {
    expect(isInflow(-25.5)).toBe(true)
  })

  it('returns false for positive amounts (outflows)', () => {
    expect(isInflow(25.5)).toBe(false)
  })

  it('returns false for zero', () => {
    expect(isInflow(0)).toBe(false)
  })
})

describe('formatTransactionAmount', () => {
  it('formats outflow (positive amount) with minus sign, destructive class, expense arrow', () => {
    const result = formatTransactionAmount(25.5)
    expect(result.text).toBe('-$25.50')
    expect(result.signClass).toBe('text-destructive')
    expect(result.arrow).toBe('down')
    expect(result.testId).toBe('expense-arrow')
  })

  it('formats inflow (negative amount) with plus sign, success class, income arrow', () => {
    const result = formatTransactionAmount(-25.5)
    expect(result.text).toBe('+$25.50')
    expect(result.signClass).toBe('text-success')
    expect(result.arrow).toBe('up')
    expect(result.testId).toBe('income-arrow')
  })

  it('formats zero without sign, with foreground class, no arrow', () => {
    const result = formatTransactionAmount(0)
    expect(result.text).toBe('$0.00')
    expect(result.signClass).toBe('text-foreground')
    expect(result.arrow).toBe('none')
    expect(result.testId).toBeUndefined()
  })

  it('formats large outflow correctly', () => {
    const result = formatTransactionAmount(1234.56)
    expect(result.text).toBe('-$1,234.56')
    expect(result.signClass).toBe('text-destructive')
  })

  it('formats large inflow correctly', () => {
    const result = formatTransactionAmount(-5000)
    expect(result.text).toBe('+$5,000.00')
    expect(result.signClass).toBe('text-success')
  })
})
