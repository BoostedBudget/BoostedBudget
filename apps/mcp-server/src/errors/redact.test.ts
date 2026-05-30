import { describe, expect, it } from 'bun:test'
import { redactError } from './redact'

describe('redactError', () => {
  it('redacts financial amounts', () => {
    const result = redactError({ message: 'Balance is $1,234.56 which is too low' })
    expect(result.message).not.toContain('1,234.56')
    expect(result.message).not.toContain('$')
  })

  it('redacts what looks like account numbers', () => {
    const result = redactError({ message: 'Account 1234567890123456 not found' })
    expect(result.message).not.toContain('1234567890123456')
  })

  it('redacts tokens/secrets in messages', () => {
    const result = redactError({ message: 'Token eyJhbGciOiJSUzI1NiJ9.abc123 was invalid' })
    expect(result.message).not.toContain('eyJhbGciOiJSUzI1NiJ9')
  })

  it('keeps generic error messages intact', () => {
    const result = redactError({ message: 'Not found', status: 404 })
    expect(result.message).toBe('Not found')
    expect(result.status).toBe(404)
  })

  it('handles string input', () => {
    const result = redactError('Something went wrong')
    expect(result.message).toBe('Something went wrong')
  })

  it('handles unknown/non-object errors', () => {
    const result = redactError(null)
    expect(result.message).toBe('An unexpected error occurred')
  })

  it('never includes raw financial data in output', () => {
    const sensitiveError = {
      message: 'Transaction amount 9999.99 exceeded limit',
      data: { balance: 50000, accountId: 'acc_123456789012345' },
    }
    const result = redactError(sensitiveError)
    const serialized = JSON.stringify(result)
    expect(serialized).not.toContain('9999.99')
    expect(serialized).not.toContain('50000')
    expect(serialized).not.toContain('acc_123456789012345')
  })
})
