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

  it('R4-15: redacts Bearer token strings', () => {
    const result = redactError({ message: 'Invalid token: Bearer eyJhbGci.abc.def' })
    expect(result.message).not.toContain('eyJhbGci.abc.def')
    expect(result.message).toContain('[REDACTED]')
  })

  it('R4-15: redacts access_token key-value pairs', () => {
    const result = redactError({ message: 'Token access_token=sk-abc123xyz expired' })
    expect(result.message).not.toContain('sk-abc123xyz')
    expect(result.message).toContain('[REDACTED]')
  })

  it('R4-15: nested data fields are dropped not leaked — regression guard', () => {
    // redactError only outputs message/status/code — data is structurally excluded.
    // This test locks in that data fields (which may contain PII) never appear in output,
    // even if redactString is not applied to them directly.
    const result = redactError({
      message: 'Server error',
      data: { accountBalance: 99999.99, userId: 'user_secret_123' },
    } as unknown)
    const serialized = JSON.stringify(result)
    expect(serialized).not.toContain('99999')
    expect(serialized).not.toContain('user_secret_123')
    expect(serialized).not.toContain('accountBalance')
  })

  it('Issue #1630 Task 11: surfaces a backend error body detail field into the message', () => {
    const result = redactError({
      message: 'API error 400',
      status: 400,
      body: { title: 'Bad Request', status: 400, detail: 'invalid budget: name is required' },
    })
    expect(result.message).toContain('name is required')
    expect(result.status).toBe(400)
  })

  it('Issue #1630 Task 11: surfaces Huma structured errors[] location/message from the body', () => {
    const result = redactError({
      message: 'API error 422',
      status: 422,
      body: {
        title: 'Unprocessable Entity',
        status: 422,
        detail: 'validation failed',
        errors: [{ message: 'expected string', location: 'body.start_date' }],
      },
    })
    expect(result.message).toContain('body.start_date')
    expect(result.message).toContain('expected string')
  })

  it('Issue #1630 Task 11: redacts a currency amount surfaced via body.detail — regression guard', () => {
    const result = redactError({
      message: 'API error 400',
      status: 400,
      body: { detail: 'amount must be positive, got $1,234.56' },
    })
    expect(result.message).toContain('amount must be positive')
    expect(result.message).not.toContain('1,234.56')
    expect(result.message).not.toContain('$')
  })

  it('Issue #1630 Task 11: body.errors[].value is never surfaced — it can echo raw user input', () => {
    // Huma's ErrorDetail.Value echoes the offending value back for debugging; it is not a
    // human-readable field name/message and may carry raw financial values or PII, so it must
    // never reach the sandbox even redacted-in-place — the extraction only reads message/location.
    const result = redactError({
      message: 'API error 422',
      status: 422,
      body: {
        errors: [{ message: 'must be positive', location: 'body.amount', value: 9999.99 }],
      },
    })
    expect(result.message).not.toContain('9999.99')
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
