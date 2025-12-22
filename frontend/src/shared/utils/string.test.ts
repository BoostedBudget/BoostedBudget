import { describe, it, expect } from 'vitest'
import { abbreviateUUID } from './string'

describe('abbreviateUUID', () => {
  it('abbreviates standard UUID format', () => {
    expect(abbreviateUUID('ad8d6c49-4f4a-4756-945a-603fea50d77a')).toBe(
      'ad8d6c49...d77a'
    )
  })

  it('abbreviates another UUID correctly', () => {
    expect(abbreviateUUID('12345678-1234-1234-1234-123456789012')).toBe(
      '12345678...9012'
    )
  })

  it('returns empty string for null input', () => {
    expect(abbreviateUUID(null as unknown as string)).toBe('')
  })

  it('returns empty string for undefined input', () => {
    expect(abbreviateUUID(undefined as unknown as string)).toBe('')
  })

  it('returns empty string for empty string input', () => {
    expect(abbreviateUUID('')).toBe('')
  })

  it('returns original string if too short to abbreviate', () => {
    // Default: prefixLen=8, suffixLen=4, plus "..." = minimum 15 chars needed
    expect(abbreviateUUID('short')).toBe('short')
    expect(abbreviateUUID('1234567890')).toBe('1234567890')
  })

  it('abbreviates at exact boundary length', () => {
    // 15 chars is exactly prefixLen(8) + ...(3) + suffixLen(4)
    // A 16 char string should abbreviate
    expect(abbreviateUUID('1234567890123456')).toBe('12345678...3456')
  })

  it('handles custom prefix and suffix lengths', () => {
    expect(
      abbreviateUUID('ad8d6c49-4f4a-4756-945a-603fea50d77a', 4, 4)
    ).toBe('ad8d...d77a')
  })

  it('handles whitespace-only strings', () => {
    expect(abbreviateUUID('   ')).toBe('   ')
  })
})
