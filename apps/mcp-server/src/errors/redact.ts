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

/**
 * Extracts a human-readable detail string from a backend error response body
 * (Issue #1630 Task 11). Handles Huma's ErrorModel shape (`detail`, and
 * `errors[].location`/`errors[].message` for schema-validation 422s) plus the
 * plainer `{ error }`/`{ message }` fallback fetch-mutator.ts uses when the body
 * itself fails to parse as JSON.
 *
 * Deliberately narrow: only known string fields are read. `errors[].value` is
 * NEVER read — Huma echoes the offending value back for debugging and it may
 * carry a raw financial amount or other PII that redactString's pattern list
 * was never designed to catch reliably inside an arbitrary JSON value.
 */
function extractBodyDetail(body: unknown): string | undefined {
  if (!body || typeof body !== 'object') return undefined
  const b = body as Record<string, unknown>
  const parts: string[] = []

  if (typeof b.detail === 'string' && b.detail.length > 0) parts.push(b.detail)
  else if (typeof b.error === 'string' && b.error.length > 0) parts.push(b.error)
  else if (typeof b.message === 'string' && b.message.length > 0) parts.push(b.message)

  if (Array.isArray(b.errors)) {
    for (const entry of b.errors) {
      if (!entry || typeof entry !== 'object') continue
      const e = entry as Record<string, unknown>
      const location = typeof e.location === 'string' ? e.location : undefined
      const msg = typeof e.message === 'string' ? e.message : undefined
      if (location && msg) parts.push(`${location}: ${msg}`)
      else if (msg) parts.push(msg)
    }
  }

  return parts.length > 0 ? parts.join('; ') : undefined
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
    let message = typeof obj.message === 'string' ? redactString(obj.message) : 'An error occurred'

    const detail = extractBodyDetail(obj.body)
    if (detail) {
      message = `${message}: ${redactString(detail)}`
    }

    const result: RedactedError = { message }
    if (typeof obj.status === 'number') result.status = obj.status
    if (typeof obj.code === 'string') result.code = obj.code
    return result
  }

  return { message: 'An unexpected error occurred' }
}
