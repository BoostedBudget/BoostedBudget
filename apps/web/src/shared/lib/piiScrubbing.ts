/**
 * Property names that carry financial PII (merchant/account/institution
 * details, balances) or third-party auth/session secrets, and must never be
 * sent to third-party analytics/observability tools.
 *
 * Shared by PostHog's `before_send` (see `posthog.ts`), which deletes
 * properties by exact key match, and Sentry's `redactPii` below, which also
 * matches by *token*: a key is split on camelCase boundaries and `_`/`-`
 * (e.g. `accountBalance` / `account_balance` → `['account', 'balance']`),
 * lowercased, and redacted if any token — or the whole lowercased key, for
 * multi-word snake_case entries below — is in this set. This is what makes
 * `currentBalance`/`institutionName`/`userEmail`-style camelCase fields get
 * caught, not just their snake_case spellings.
 *
 * Because token matching is per-word, every entry here must be safe to
 * redact in *any* compound it appears in — not just the specific field name
 * that motivated adding it. Two words are deliberately NOT bare entries:
 *
 * - `account`: would redact `accountCount`/`accountId`-style metadata that
 *   carries no PII itself. (`account_name`/`account_mask`/`account_id`
 *   below stay as exact multi-word entries — they still catch the literal
 *   snake_case key, just not `accountName` etc. via token matching.)
 * - `name`: would redact `typeName`/`fieldName`-style generic fields.
 *   `institutionName`/`merchantName` are still caught via the `institution`/
 *   `merchant` tokens, so a bare `name` entry isn't needed for the PII
 *   cases we know about; the tradeoff is `accountName` isn't caught by
 *   token matching either (only its literal snake_case form is, via
 *   `account_name`) — a known, accepted gap rather than an over-scrub.
 *
 * `description` was removed as a bare entry for the same reason: it would
 * redact `descriptionLength`-style metadata. This does drop token-level
 * protection for a literal `description` field (e.g. a transaction/rule
 * description, which can carry raw merchant text) — no current call site
 * forwards one into analytics properties, and UI display of description
 * content is already covered by `data-ph-mask` (session replay), which is
 * the actual defense for that field's *content*.
 */
export const PII_PROPERTY_DENYLIST = new Set([
  'merchant',
  'merchant_name',
  'nickname',
  'institution',
  'institution_name',
  'account_name',
  'balance',
  'amount',
  'current_balance',
  'target_amount',
  'current_amount',
  'total_saved',
  // Sentry-only additions: fields that show up in raw Plaid/Paddle payloads
  // and Axios request/response bodies, but never in PostHog's analytics
  // `properties` payloads (which are hand-built, not raw third-party data).
  'email',
  'customer_email',
  'account_mask',
  'mask',
  'account_id',
  'access_token',
  'public_token',
  'link_session_id',
  'authorization',
  'cookie',
  'set-cookie',
])

/** Splits a key on camelCase boundaries and `_`/`-`, lowercased. */
function tokenize(key: string): string[] {
  return key
    .replace(/([a-z0-9])([A-Z])/g, '$1_$2')
    .split(/[_-]/)
    .filter(Boolean)
    .map(token => token.toLowerCase())
}

/**
 * True if `key` should be redacted: either the whole lowercased key matches
 * a denylist entry (covers multi-word snake_case entries like
 * `account_name`, which are never reachable via single-token matching), or
 * any individual camelCase/snake_case/kebab-case token of the key does.
 */
function isPiiKey(key: string): boolean {
  if (PII_PROPERTY_DENYLIST.has(key.toLowerCase())) return true
  return tokenize(key).some(token => PII_PROPERTY_DENYLIST.has(token))
}

const REDACTED = '[Redacted]'
// Deep enough for real-world nested payloads this function is asked to scrub
// (e.g. event.exception.values[].stacktrace.frames[].vars is 5 levels deep
// on its own, before the Axios config/response objects nested inside extra
// add more) while still bounding recursion against pathological input.
const MAX_DEPTH = 12

/** Strips everything from `?` onward, if present. Query strings can carry account/session identifiers. */
export function stripQueryString(url: string): string {
  const queryIndex = url.indexOf('?')
  return queryIndex === -1 ? url : url.slice(0, queryIndex)
}

/**
 * Recursively redacts denylisted property names and strips query strings
 * from any property literally named `url`, anywhere inside an arbitrarily
 * nested object/array (e.g. a raw Axios error's `config`/`response`).
 *
 * Redaction is by *property name* only — string/number values are never
 * scanned for PII-shaped content. This is a deliberate scope limit: content
 * scanning would risk destroying unrelated diagnostic signal (error
 * messages, stack traces, non-PII context) that engineers rely on to debug
 * production issues.
 */
export function redactPii<T>(value: T, depth = 0, seen: WeakSet<object> = new WeakSet()): T {
  if (value === null || typeof value !== 'object') return value
  if (depth >= MAX_DEPTH) return '[Truncated]' as unknown as T
  if (seen.has(value as object)) return '[Circular]' as unknown as T

  // `seen` tracks the current recursion *ancestor path*, not every object
  // ever visited: it's removed again in `finally` once this node (and its
  // descendants) finish processing. This distinguishes a true cycle (an
  // object that is its own ancestor) from a DAG where the same object is
  // legitimately referenced from two independent branches (e.g. an Axios
  // error's `config` object shared by both the error and its `response`) —
  // the latter must still be redacted on each occurrence, not replaced with
  // a sentinel that would silently drop real diagnostic data.
  seen.add(value as object)
  try {
    if (Array.isArray(value)) {
      return value.map(item => redactPii(item, depth + 1, seen)) as unknown as T
    }

    const out: Record<string, unknown> = {}
    for (const [key, val] of Object.entries(value as Record<string, unknown>)) {
      if (isPiiKey(key)) {
        out[key] = REDACTED
        continue
      }
      if (key === 'url' && typeof val === 'string') {
        out[key] = stripQueryString(val)
        continue
      }
      out[key] = redactPii(val, depth + 1, seen)
    }
    return out as unknown as T
  } finally {
    seen.delete(value as object)
  }
}
