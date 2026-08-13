// Issue #1514 (fix round 7): single source of truth for capability-route path patterns —
// see redactUrlPath below and client.ts's own doc comment for why this lives there, not
// duplicated here.
import { redactCapabilityUrlPath } from '@boostedbudget/api-client/client'

/**
 * Property names that carry financial PII (merchant/account/institution
 * details, balances) or third-party auth/session secrets, and must never be
 * sent to third-party analytics/observability tools.
 *
 * Shared by a single matcher, `isPiiKey` below, used by both PostHog's
 * `before_send` (see `posthog.ts`) and Sentry's `redactPii`: a key is split
 * on camelCase boundaries and any of `_`/`-`/`.`/`/`/`:`/`\`/whitespace (e.g.
 * `accountBalance` / `account_balance` / `account.balance` →
 * `['account', 'balance']`), lowercased, and redacted if
 * any contiguous token window — or the whole lowercased key, for multi-word
 * entries below — is in this set. This is what makes
 * `currentBalance`/`institutionName`/`userEmail`-style camelCase fields get
 * caught, not just their snake_case spellings, on *both* paths. (Before this
 * was unified, PostHog matched only the exact literal key and silently
 * missed every camelCase near-miss Sentry caught — see git history on this
 * file for that bug.)
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
 * The multi-word entries below (`account_name`, `institution_name`,
 * `merchant_name`, `current_balance`, `target_amount`, `current_amount`,
 * `account_mask`, `account_id`, `customer_email`, `query_string`) are still
 * NOT redundant with their component single-word tokens, even now that both
 * PostHog and Sentry tokenize: a bare `account`/`name`/`query`/`string`
 * entry would each cause its own over-scrub (see above), so the only way to
 * catch these specific compounds is as multi-word windows. Deleting one of
 * these compound entries reopens a real gap on *both* analytics paths, not
 * just one — that's the part of this warning that changed post-unification;
 * it used to be "PostHog-only," it is now "both."
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
  // These entries originated from raw Plaid/Paddle payloads and Axios
  // request/response bodies (Sentry's use case), not from hand-built
  // PostHog `properties`. Before unification they applied to Sentry only;
  // now that both consumers share `isPiiKey`, they apply to *both* paths —
  // including PostHog's `trackEvent` properties, which is a real behavior
  // change, not just a doc note. One concrete consequence: a bare `email`/
  // `mask` token also matches boolean flags that merely contain the word,
  // e.g. a hypothetical `emailNotificationsEnabled` or `maskEnabled` — no
  // current call site sends either, so this is a widened contract without
  // present-day data loss, not a live bug. This is accepted rather than
  // narrowed: `email` as a bare token is what catches `userEmail`/
  // `customerEmail` (real PII), and narrowing it to avoid the boolean-flag
  // false positive would reopen that real gap, which is worse than the
  // over-scrub of an unused hypothetical flag name.
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
  // Sentry `event.request` fields that key-based matching otherwise misses:
  // `query_string` tokenizes to ['query', 'string'], neither of which is a
  // PII word on its own, so its raw value (e.g. `account_id=acc_123&token=
  // secret`) passed through unredacted; `cookies` is a nested *container*
  // (`{ session: '...' }`), not a header, so it never matched the singular
  // `cookie` header entry above. Both are bare entries so the whole value/
  // object is redacted, the same way any other denylist hit is. `cookies` is
  // deliberately NOT split into redacting only its inner keys (e.g. a bare
  // `session` entry): `session` alone would also catch benign fields like a
  // hypothetical `sessionCount`/`session_id`-style metadata field, which is
  // exactly the over-scrub class the `account`/`name` exclusions above guard
  // against — redacting the whole `cookies` container avoids that without
  // losing coverage, since nothing legitimate needs cookie values in
  // analytics/error payloads.
  'query_string',
  'cookies',
])

/**
 * Splits a key into lowercase word tokens on camelCase/PascalCase
 * boundaries, acronym boundaries, digit runs, and the separator set below.
 * Exported for direct unit testing — bugs here are exactly as dangerous as
 * the exact-match bug this replaced, since a token that's silently
 * swallowed or never split off is a token that can never match the
 * denylist.
 *
 * Passes run in this order (each one only inserts `_`; nothing is removed):
 * 1. Acronym→word boundary: `HTTPStatus` → `HTTP_Status`, `userIDBalance` →
 *    `userID_Balance` (the trailing word of an uppercase run is peeled off
 *    before the next pass handles the run's own lower→upper boundary).
 * 2. lower/digit→upper boundary: `userID_Balance` → `user_ID_Balance`.
 * 3. letter→digit boundary: `balance2` → `balance_2`.
 * 4. digit→letter boundary: `2Name` → `2_Name`.
 *
 * The final split runs on `[_\-./\s:\\]` — every punctuation/whitespace
 * separator this codebase's key spellings plausibly use, not just `_`/`-`:
 * `.` (dotted property paths, e.g. `merchant.name` from a flattened Axios
 * body), `/` (slash-joined keys, e.g. `merchant/name`), whitespace (a raw
 * label used as a key, e.g. `"current balance"`), `:` (namespaced keys, a
 * common convention for cache/analytics keys, e.g. `user:email`), and `\`
 * (Windows-style path segments that occasionally leak into serialized
 * payloads). Before this widened set, a denylisted word joined to the rest
 * of a key by any separator outside `[_-]` produced a single un-split token
 * that could never match the denylist or enter the window scan as its own
 * word — see `piiScrubbing.test.ts`'s "punctuation-delimited keys" block for
 * the adversarial keys that gap let through.
 */
export function tokenize(key: string): string[] {
  return key
    .replace(/(\p{Lu}+)(\p{Lu}\p{Ll})/gu, '$1_$2')
    .replace(/([\p{Ll}\p{Nd}])(\p{Lu})/gu, '$1_$2')
    .replace(/(\p{L})(\p{Nd})/gu, '$1_$2')
    .replace(/(\p{Nd})(\p{L})/gu, '$1_$2')
    .split(/[_\-./\s:\\]/)
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
 *
 * Pure-digit tokens (e.g. the `1` in `account1Name`) are dropped from the
 * token list used to build the window scan below (but NOT from the list used
 * for the `ID_ALLOWLIST` comparison — see that section). Without this, a
 * digit sitting between two words breaks contiguity and a compound entry
 * like `account_name` can never match `account1Name` even though
 * `accountName` matches it directly — the digit would silently make the
 * field un-redactable. The denylist has no digit-only entries, so dropping
 * digit tokens can only ever surface additional matches (e.g.
 * `account1Name`, `public1Token`), never suppress one that already matched:
 * every entry that matched via the un-filtered tokens still matches once the
 * (non-matching) digit tokens are removed from between them.
 *
 * **`ID_ALLOWLIST`.** Exactly two keys are exempted from the bare-token
 * window scan below, matched on their normalized/tokenized form so both
 * spellings hit one entry: `merchant_id` (covers `merchant_id` and
 * `merchantId`) and `institution_id` (covers `institution_id` and
 * `institutionId`). Nothing else is exempted. These are opaque identifiers —
 * the documented `trackEvent` pattern is to pass `{ merchant_id: uuid }`,
 * never the name — and without this carve-out they'd be deleted anyway,
 * because they still contain the bare `merchant`/`institution` token the
 * window scan matches regardless of the trailing `id`.
 *
 * The allowlist comparison MUST run against the *unfiltered* token join, not
 * the digit-filtered one used for the window scan below. A key like
 * `merchant_1_id` tokenizes to `['merchant', '1', 'id']`; if the digit were
 * dropped first, the remaining `['merchant', 'id']` would join to the exact
 * string `merchant_id` and match the allowlist, exempting an unbounded
 * family of digit-infix variants (`merchant_1_id`, `merchant_99_id`,
 * `institution_1_id`, `merchant_id2`, ...) that the allowlist is documented
 * as never covering — a privacy hole in the same shape as defect 2 below,
 * just one level subtler. Comparing against the unfiltered join closes that:
 * `merchant_1_id`'s unfiltered join is `merchant_1_id`, which is not in
 * `ID_ALLOWLIST`, so it falls through to the ordinary window scan, where the
 * (now digit-filtered) bare `merchant` token redacts it. Genuine allowlist
 * keys are unaffected because they never had a digit token to begin with:
 * `merchant_id`/`merchantId` both tokenize to `['merchant', 'id']` whether or
 * not digits are filtered.
 *
 * This replaces an earlier version of this carve-out that exempted *every*
 * key whose last token is `id`, not just these two. That was a real privacy
 * regression: any denylisted bare token followed by an `id` suffix
 * (`email_id`, `nickname_id`, `balance_id`, `merchant_name_id`, ...) survived
 * scrubbing too, on both the PostHog and Sentry paths — see
 * `piiScrubbing.test.ts`'s "defect 2" test block for the adversarial keys
 * that regression let through. A shape-based exemption (anything ending in
 * `id`) is not a narrow allowlist merely because few denylist *entries*
 * happen to end in `_id`; the number of incoming *keys* it exempts is
 * unbounded. `ID_ALLOWLIST` fixes that by naming the two keys explicitly:
 * everything else — including any future `*_id` field — goes through the
 * ordinary token scan and is redacted if it contains a denylisted token.
 * Add a new key here only if it is itself an opaque identifier with no PII
 * in its name; do not add it just because it ends in `id`.
 *
 * **Punctuation spellings of the allowlisted keys.** Widening `tokenize`'s
 * separator set (see its doc comment) means `merchant.id` and `merchant/id`
 * now normalize to the same `['merchant', 'id']` tokens as `merchant_id`/
 * `merchantId` and are therefore allowlist-exempt too, even though neither
 * punctuation spelling was named explicitly. This is accepted deliberately,
 * not overlooked: the allowlist's own rationale is "this is an opaque
 * identifier, not a name" (see above), and that rationale is about the
 * *value* `merchant_id` identifies, not about which separator glues the two
 * words together — a dotted or slashed spelling of the same identifier
 * carries the same non-PII payload. The alternative (requiring the
 * allowlist to match only the literal `_`/camelCase spellings) would just
 * mean `merchant.id`-style keys fall through to the ordinary window scan and
 * get redacted-as-if-PII, which is a false positive, not a safety gain —
 * there is no PII in an opaque id field regardless of its separator.
 */
const ID_ALLOWLIST = new Set(['merchant_id', 'institution_id'])

export function isPiiKey(key: string): boolean {
  if (PII_PROPERTY_DENYLIST.has(key.toLowerCase())) return true
  const rawTokens = tokenize(key)
  if (rawTokens.length === 0) return false

  if (ID_ALLOWLIST.has(rawTokens.join('_'))) return false

  const tokens = rawTokens.filter(token => !/^\d+$/.test(token))
  if (tokens.length === 0) return false

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
 * Issue #1514 (fix round 6, widened in round 7): redacts known capability-token PATH
 * segments (e.g. the account-deletion cancellation link, the household-invite link) —
 * `stripQueryString` only ever strips from `?` onward, so it does not (and structurally
 * cannot) touch a token embedded as a raw path segment.
 *
 * Round 6 covered only the API path shape (`/api/v1/user/delete/cancel/{token}`) — but the
 * ACTUAL exposure is the FRONTEND route the emailed link points at: `/cancel-deletion/{token}`
 * (`App.tsx`'s route), which lands in the PAGE URL itself once a user clicks the email.
 *
 * The pattern list (`CAPABILITY_URL_PATH_PATTERNS`) used to be duplicated here — it drifted
 * out of sync by missing every frontend route shape entirely. `packages/api-client`'s
 * `client.ts` is now the single source of truth: apps/web already depends on that package
 * (not the reverse), so importing its `redactCapabilityUrlPath` here — rather than
 * maintaining a second copy of the pattern list — respects the existing dependency
 * direction instead of inverting it. See `client.ts`'s own doc comment for the full,
 * re-derived route list and the reasoning for a route-SHAPE allowlist over a value-shape
 * heuristic. Add a new capability route there (only there) when one is introduced.
 */
export function redactUrlPath(url: string): string {
  return redactCapabilityUrlPath(url) ?? url
}

/**
 * The one function call sites should reach for whenever a `url` value (or any string that
 * might embed one, e.g. an error message) could reach an observability payload: strips the
 * query string, then redacts any known capability-token path segment.
 */
export function sanitizeUrl(url: string): string {
  return redactUrlPath(stripQueryString(url))
}

/**
 * Recursively redacts denylisted property names and strips query strings
 * from any property literally named `url`, anywhere inside an arbitrarily
 * nested object/array (e.g. a raw Axios error's `config`/`response`).
 *
 * Redaction of PII (financial/account/session data) is by *property name*
 * only — string/number values are never scanned for PII-SHAPED content. This
 * is a deliberate scope limit: general content scanning would risk destroying
 * unrelated diagnostic signal (error messages, stack traces, non-PII context)
 * that engineers rely on to debug production issues.
 *
 * Issue #1514 (fix round 7): capability-route redaction is the ONE exception to
 * "never scans string content" — every string value this function visits (not
 * just ones keyed `url`) is passed through `redactUrlPath`, so a bearer token
 * embedded in `breadcrumb.message`, an exception `value`, or a stack-frame
 * string is caught even though none of those properties is named `url`. This
 * stays a targeted rewrite, not a content scan, because `redactUrlPath` only
 * ever matches the small set of known capability-route shapes — an ordinary
 * error message containing a slash or a non-capability path is untouched.
 */
export function redactPii<T>(value: T, depth = 0, seen: WeakSet<object> = new WeakSet()): T {
  if (typeof value === 'string') return redactUrlPath(value) as unknown as T
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
        out[key] = sanitizeUrl(val)
        continue
      }
      out[key] = redactPii(val, depth + 1, seen)
    }
    return out as unknown as T
  } finally {
    seen.delete(value as object)
  }
}
