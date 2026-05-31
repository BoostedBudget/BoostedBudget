/**
 * Error redaction — strips financial payloads and PII before returning
 * errors to the agent. No raw financial amounts, account numbers, tokens,
 * or user data should ever appear in error messages sent to the sandbox.
 */

export interface RedactedError {
  message: string
  status?: number
  code?: string
}

// Patterns to redact from error messages
const REDACT_PATTERNS: RegExp[] = [
  /\$[\d,]+\.?\d*/g,              // currency amounts: $1,234.56
  /\b\d+\.\d{2,}\b/g,            // decimal amounts: 9999.99, 1234.56
  /\b\d{10,}\b/g,                 // account/card numbers
  /eyJ[A-Za-z0-9_-]{10,}/g,      // JWT header prefix
  /Bearer\s+\S+/gi,               // Bearer tokens
  /\baccess_token\s*[:=]\s*\S+/gi, // access token key-value
]

function redactString(s: string): string {
  return REDACT_PATTERNS.reduce((result, pattern) => result.replace(pattern, '[REDACTED]'), s)
}

export function redactError(err: unknown): RedactedError {
  if (err === null || err === undefined) {
    return { message: 'An unexpected error occurred' }
  }

  if (typeof err === 'string') {
    return { message: redactString(err) }
  }

  if (typeof err === 'object') {
    const obj = err as Record<string, unknown>
    const result: RedactedError = {
      message: typeof obj.message === 'string' ? redactString(obj.message) : 'An error occurred',
    }
    if (typeof obj.status === 'number') result.status = obj.status
    if (typeof obj.code === 'string') result.code = obj.code
    return result
  }

  return { message: 'An unexpected error occurred' }
}
