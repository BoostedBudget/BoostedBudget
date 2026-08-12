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
 *   carries no PII itself.
 * - `name`: would redact `typeName`/`fieldName`-style generic fields.
 *   `institutionName`/`merchantName` are still caught via the `institution`/
 *   `merchant` tokens, so a bare `name` entry isn't needed for those; and
 *   `accountName` is caught not via a bare token but via the *adjacent-pair*
 *   check in `isPiiKey` below, which matches `account_name` as a two-token
 *   window — so `accountCount`/`typeName` stay safe while `accountName`
 *   doesn't silently slip through as an asymmetric gap.
 *
 * `description` was removed as a bare entry for the same reason: it would
 * redact `descriptionLength`-style metadata. This does drop token-level
 * protection for a literal `description` field (e.g. a transaction/rule
 * description, which can carry raw merchant text) — no current call site
 * forwards one into analytics properties, and UI display of description
 * content is already covered by `data-ph-mask` (session replay), which is
 * the actual defense for that field's *content*.
 *
 * ⚠ Do NOT delete `account_name` / `institution_name` / `merchant_name` /
 * `current_balance` / `target_amount` / `current_amount` / `account_mask` /
 * `account_id` / `customer_email` as "redundant" because their component
 * words (`institution`, `merchant`, `balance`, `amount`, `email`) already
 * cover them via Sentry's token matching below. They are NOT redundant:
 * **PostHog's** `before_send` (see `posthog.ts`) deletes properties by
 * *exact literal key* match against this same Set and never tokenizes —
 * `balance` alone cannot match a literal `current_balance` property there.
 * Deleting the compound entry would silently reopen PostHog leakage of that
 * exact field while Sentry's coverage stayed intact, which is the opposite
 * of an obvious regression.
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

/**
 * Splits a key into lowercase word tokens on camelCase/PascalCase
 * boundaries, acronym boundaries, digit runs, and `_`/`-`. Exported for
 * direct unit testing — bugs here are exactly as dangerous as the exact-
 * match bug this replaced, since a token that's silently swallowed or never
 * split off is a token that can never match the denylist.
 *
 * Passes run in this order (each one only inserts `_`; nothing is removed):
 * 1. Acronym→word boundary: `HTTPStatus` → `HTTP_Status`, `userIDBalance` →
 *    `userID_Balance` (the trailing word of an uppercase run is peeled off
 *    before the next pass handles the run's own lower→upper boundary).
 * 2. lower/digit→upper boundary: `userID_Balance` → `user_ID_Balance`.
 * 3. letter→digit boundary: `balance2` → `balance_2`.
 * 4. digit→letter boundary: `2Name` → `2_Name`.
 */
export function tokenize(key: string): string[] {
  return key
    .replace(/(\p{Lu}+)(\p{Lu}\p{Ll})/gu, '$1_$2')
    .replace(/([\p{Ll}\p{Nd}])(\p{Lu})/gu, '$1_$2')
    .replace(/(\p{L})(\p{Nd})/gu, '$1_$2')
    .replace(/(\p{Nd})(\p{L})/gu, '$1_$2')
    .split(/[_-]/)
    .filter(Boolean)
    .map(token => token.toLowerCase())
}

// The longest denylist entry (`link_session_id`) has 3 underscore-separated
// words; adjacent-token windows up to that width are checked so multi-word
// entries like `account_name` still match a camelCase `accountName` even
// though `account` and `name` are each individually excluded as bare tokens
// (see the denylist comment above for why).
const DENYLIST_MAX_WORDS = Math.max(
  ...Array.from(PII_PROPERTY_DENYLIST, entry => entry.split(/[_-]/).length)
)

/**
 * True if `key` should be redacted: either the whole lowercased key matches
 * a denylist entry, or some contiguous run of its tokens — joined with `_`,
 * from length 1 up to the longest denylist entry's word count — does.
 */
function isPiiKey(key: string): boolean {
  if (PII_PROPERTY_DENYLIST.has(key.toLowerCase())) return true
  const tokens = tokenize(key)
  const maxWindow = Math.min(DENYLIST_MAX_WORDS, tokens.length)
  for (let windowSize = 1; windowSize <= maxWindow; windowSize++) {
    for (let start = 0; start + windowSize <= tokens.length; start++) {
      if (PII_PROPERTY_DENYLIST.has(tokens.slice(start, start + windowSize).join('_'))) {
        return true
      }
    }
  }
  return false
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
