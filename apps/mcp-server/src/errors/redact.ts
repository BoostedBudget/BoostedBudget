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

// Patterns to redact from error messages. Order matters — patterns run in sequence via
// String#replace, so a more specific shape (a whole UUID, a $-prefixed currency amount) must run
// BEFORE the generic bare-digit catch-all, or the catch-all would chew a numeric segment out of it
// and leave the rest (e.g. a UUID's own digit-only groups, or the fractional part of an amount)
// exposed as a partial leftover. #1630 Task-4-fixwave: the previous list missed plain (no-$)
// integer/comma-grouped amounts, one-decimal-digit amounts, emails, UUIDs, and hyphen-grouped
// account numbers — an independent adversarial review's concrete example,
// "Balance is 50000 for alice@example.com, account 1234-5678-9012", passed through every one of
// those gaps unredacted.
const REDACT_PATTERNS: RegExp[] = [
  /\$[\d,]+\.?\d*/g,                                            // currency: $1,234.56
  /\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b/g, // UUIDs
  /\b\d[\d,]*\.\d+\b/g,          // decimal amounts, any precision, optionally comma-grouped:
                                 // 9.5, 1234.56, 1,234.56
  /\b\d{1,3}(?:,\d{3})+\b/g,     // comma-grouped integers with no decimal: 50,000
  /\b\d[\d-]*\d\b/g,             // any other bare multi-digit run, optionally hyphen-grouped —
                                 // catches plain integer balances (50000) and hyphenated account
                                 // numbers (1234-5678-9012) alike
  /[\w.+-]+@[\w-]+\.[a-zA-Z]{2,}/g, // email addresses
  /eyJ[A-Za-z0-9_-]{10,}/g,      // JWT header prefix
  /Bearer\s+\S+/gi,               // Bearer tokens
  /\baccess_token\s*[:=]\s*\S+/gi, // access token key-value
]

function redactString(s: string): string {
  // #1699: preserve a leading `API error <status>` prefix verbatim. The HTTP status is diagnostic
  // metadata, not user data, but the bare multi-digit catch-all below would otherwise rewrite it to
  // `API error [REDACTED]` — hiding the one field that tells the agent whether a failure is
  // retryable. Extract the prefix, redact only the remainder, then re-prepend it.
  const prefixMatch = /^API error (\d{3})\b/.exec(s)
  const prefix = prefixMatch ? prefixMatch[0] : ''
  const remainder = prefix ? s.slice(prefix.length) : s
  const redacted = REDACT_PATTERNS.reduce(
    (result, pattern) => result.replace(pattern, '[REDACTED]'),
    remainder,
  )
  return prefix + redacted
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

    // #1630 Task-4-fixwave: only surface body detail for a 4xx. A 4xx body's strings are the
    // backend's own hand-written validation prose (domain.ErrInvalidBudget's field-naming errors,
    // Huma's schema-validation location/message pairs) — content the backend author chose to put
    // in a client-facing error on purpose. A 5xx body is an unhandled failure: its strings are
    // whatever the failing code happened to be holding (a raw value, an internal detail), never
    // authored for a client audience, and every mapper string this codebase actually returns for a
    // 5xx is static (see internal/huma/errors) — so restricting passthrough to 4xx costs nothing
    // and removes a whole class of incidental-PII/financial-value leak risk for free.
    const status = typeof obj.status === 'number' ? obj.status : undefined
    const detail = status !== undefined && status >= 400 && status < 500 ? extractBodyDetail(obj.body) : undefined
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
