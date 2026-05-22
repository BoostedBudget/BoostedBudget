/**
 * Chart Formatters Tests
 */

import { describe, it, expect } from 'bun:test'
import {
  formatCurrency,
  formatCurrencyCompact,
  formatCurrencyFull,
} from './formatters'

describe('formatCurrency', () => {
  it('formats positive whole numbers', () => {
    expect(formatCurrency(1234)).toBe('$1,234')
    expect(formatCurrency(1000000)).toBe('$1,000,000')
  })

  it('formats zero', () => {
    expect(formatCurrency(0)).toBe('$0')
  })

  it('formats negative numbers', () => {
    expect(formatCurrency(-1234)).toBe('-$1,234')
  })

  it('rounds decimal values', () => {
    expect(formatCurrency(1234.56)).toBe('$1,235')
    expect(formatCurrency(1234.44)).toBe('$1,234')
  })
})

describe('formatCurrencyCompact', () => {
  it('formats small numbers without abbreviation', () => {
    expect(formatCurrencyCompact(123)).toBe('$123')
    expect(formatCurrencyCompact(999)).toBe('$999')
  })

  it('formats thousands with k suffix', () => {
    expect(formatCurrencyCompact(1000)).toBe('$1.0k')
    expect(formatCurrencyCompact(1234)).toBe('$1.2k')
    expect(formatCurrencyCompact(12345)).toBe('$12.3k')
    expect(formatCurrencyCompact(999999)).toBe('$1000.0k')
  })

  it('formats millions with M suffix', () => {
    expect(formatCurrencyCompact(1000000)).toBe('$1.0M')
    expect(formatCurrencyCompact(1234567)).toBe('$1.2M')
    expect(formatCurrencyCompact(12345678)).toBe('$12.3M')
  })

  it('formats negative numbers', () => {
    expect(formatCurrencyCompact(-1234)).toBe('-$1.2k')
    expect(formatCurrencyCompact(-1234567)).toBe('-$1.2M')
  })

  it('formats zero', () => {
    expect(formatCurrencyCompact(0)).toBe('$0')
  })
})

describe('formatCurrencyFull', () => {
  it('formats with cents', () => {
    expect(formatCurrencyFull(1234.56)).toBe('$1,234.56')
    expect(formatCurrencyFull(1234)).toBe('$1,234.00')
  })

  it('formats negative numbers with cents', () => {
    expect(formatCurrencyFull(-1234.56)).toBe('-$1,234.56')
  })

  it('formats zero with cents', () => {
    expect(formatCurrencyFull(0)).toBe('$0.00')
  })
})
