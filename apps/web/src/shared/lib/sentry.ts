import * as Sentry from '@sentry/react'
import type { Breadcrumb, ErrorEvent, EventHint } from '@sentry/react'
import { redactPii, redactUrlPath, sanitizeUrl } from './piiScrubbing'

// `SpanJSON`/`TransactionEvent` aren't part of `@sentry/react`'s public export surface
// (they live in `@sentry/core`, which this package doesn't declare as a direct
// dependency) — derived structurally from `Sentry.init`'s own options type instead of
// importing them by name.
type SentryInitOptions = NonNullable<Parameters<typeof Sentry.init>[0]>
type BeforeSendTransactionFn = NonNullable<SentryInitOptions['beforeSendTransaction']>
type BeforeSendSpanFn = NonNullable<SentryInitOptions['beforeSendSpan']>
type TransactionEvent = Parameters<BeforeSendTransactionFn>[0]

// The field set `redactCommonEventFields` below walks — every field an empirical probe
// (fix rounds 9-11) confirmed BOTH `ErrorEvent` and `TransactionEvent` can carry the
// capability token in. Declared once so both hooks are structurally required to accept
// the same shape; there is no way for one hook's parameter type to silently drop a field
// the other still has.
interface RedactableCommonEventFields {
  transaction?: string
  request?: { url?: string }
  contexts?: Record<string, unknown>
  extra?: Record<string, unknown>
  tags?: Record<string, unknown>
  user?: Record<string, unknown> | null
  breadcrumbs?: unknown[]
  sdkProcessingMetadata?: Record<string, unknown>
  server_name?: string
}

/**
 * FULL HOOK ENUMERATION (issue #1514 fix round 9-10) — every event-emitting hook this SDK
 * version (`@sentry/core` ~10.70) exposes, checked against its actual type declarations
 * (`node_modules/@sentry/core/build/types/types/options.d.ts`), not assumed from memory or
 * fixed piecemeal as each one was pointed out:
 *
 *   - `beforeSend`            — errors/messages. SANITIZED (rounds 6-8).
 *   - `beforeBreadcrumb`      — breadcrumbs. SANITIZED (rounds 6-7).
 *   - `beforeSendTransaction` — performance transactions. SANITIZED (round 9-10).
 *   - `beforeSendSpan`        — individual child spans within a transaction. SANITIZED
 *     (round 9).
 *   - `beforeSendMetric`      — `Sentry.metrics.*` custom metrics. NOT sanitized — not
 *     applicable: grepped this codebase, nothing calls the Metrics API anywhere. Would
 *     need a `beforeSendMetric` scrub if that API is ever adopted.
 *   - `beforeSendLog`         — Sentry's newer structured-logging API
 *     (`Sentry.logger.*`/`consoleLoggingIntegration`). EXISTS in this SDK version, UNUSED
 *     here (no `logger.*` calls, no logging integration added) — not exploitable today,
 *     but would need the same treatment as `beforeSend` if adopted.
 *   - Feedback (`@sentry/feedback`'s `feedbackIntegration()`, with its own
 *     `onSubmit`/`onSubmitSuccess`/`onSubmitError` hooks — a different shape than the
 *     `beforeSend*` family above) — EXISTS as an installable integration, UNUSED here (not
 *     added to `integrations` below). Submitted feedback becomes a normal event and would
 *     go through `beforeSend` if this were ever wired up, but the widget itself lets a user
 *     type free text, which is a distinct review before enabling it.
 *   - `tracesSampler`         — decides sampling, does not touch event content; not an
 *     emission path. Not used here (static `tracesSampleRate: 0.1` instead).
 *   - Session Replay          — CONFIRMED INERT, not merely low-sample-rate:
 *     `replaysSessionSampleRate`/`replaysOnErrorSampleRate` are set on `Sentry.init` below,
 *     but `Sentry.replayIntegration()` is never added to `integrations`, and Replay is not
 *     one of `@sentry/browser`'s default integrations (checked
 *     `getDefaultIntegrations()` in that package's source) — so those two options are
 *     currently dead configuration, not a live (if low-probability) leak path. Left
 *     alone deliberately; whether to wire it up or remove the inert options is a separate
 *     product decision, not addressed here.
 *
 * Two halves of the same problem:
 * - `beforeBreadcrumb` covers the default `Breadcrumbs` integration, which
 *   records every `console.*` call verbatim. Several call sites in this app
 *   log third-party (Plaid/Paddle) payloads that can carry financial PII —
 *   dropping the whole `console` category is simpler and safer than trying
 *   to redact arbitrary, unstructured log arguments.
 * - `beforeSend` covers everything `beforeBreadcrumb` cannot: exceptions
 *   captured directly via `Sentry.captureException` (see `useApiError.ts`,
 *   `ErrorBoundary.tsx`, `FeatureErrorBoundary.tsx`), and any Axios error
 *   `config`/`response` data an error handler attaches to `extra`/`contexts`.
 *   It also strips query strings from request URLs, which can carry
 *   account/session identifiers, and redacts `event.exception` plus the full
 *   shared field set `redactCommonEventFields` walks (`transaction`, `request`,
 *   `extra`, `contexts`, `tags`, `user`, `breadcrumbs`, `sdkProcessingMetadata`,
 *   `server_name` — fix round 11 widened this from just
 *   `exception`/`request`/`extra`/`contexts` after a probe found the token
 *   surviving in `tags`/`transaction` on error events too) — covering
 *   `event.request`'s `headers`/`data`/`query_string` fields (e.g. an
 *   `Authorization`/`Cookie` header) as well as stack-frame `vars` (local
 *   variables some SDKs attach per frame) inside `event.exception`. Redaction
 *   is by *key name* only (see `redactPii` in `piiScrubbing.ts`): `query_string`
 *   and `cookies` are both bare entries in `PII_PROPERTY_DENYLIST`, so a
 *   `request.query_string` value or a nested `cookies` object is redacted
 *   wholesale (the entire value replaced, not scanned) whenever that exact
 *   key is hit — regardless of what its own nested keys are named. The
 *   residual scope limit is narrower than that: a query string or cookie
 *   value nested under some *other*, non-denylisted key name (e.g. a
 *   third-party payload that renames the field) would still pass through
 *   unredacted, because matching is by key name, not by content, for PII.
 *   Capability-route redaction (fix round 7) is the one exception: `redactPii`
 *   runs every string value it visits — including `event.exception`'s `value`
 *   (message) field and breadcrumb `message` (handled directly in
 *   `beforeBreadcrumb` below, since it isn't nested under `data`) — through
 *   `redactUrlPath`, so a bearer token embedded in ordinary error text is
 *   still caught even though it isn't a denylisted key. This stays a targeted
 *   rewrite (only the known capability-route shapes match), not a general PII
 *   content scan — an ordinary error message is otherwise preserved verbatim.
 */
/**
 * Issue #1514 (fix round 11): the field set `beforeSend` (errors) and `beforeSendTransaction`
 * (performance) both need to walk — `transaction`, `request`, `contexts`, `extra`, `tags`,
 * `user`, `breadcrumbs`, `sdkProcessingMetadata`, `server_name` — factored into ONE function
 * both hooks call, so they cannot drift apart the way they just did: round 9 added
 * `beforeSendTransaction` covering only `transaction`/`request`/`contexts`/`extra`/`spans`;
 * round 10 widened it (after a probe found `tags.route` surviving) to also cover `tags`,
 * `measurements`, `user`, `breadcrumbs`, `sdkProcessingMetadata`, `server_name` — but
 * `beforeSend` was never widened to match, even though a probe confirmed `ErrorEvent` carries
 * (and can leak the token via) the exact same `tags`/`user`/`breadcrumbs`/
 * `sdkProcessingMetadata` fields, since `browserTracingIntegration` is active for error
 * events on the same page. Two hooks independently re-implementing "walk this event" is
 * exactly how that asymmetry happened; one shared function structurally cannot cover one
 * event type's fields and not the other's.
 *
 * `exception` (errors only) and `spans`/`measurements` (transactions only) are deliberately
 * NOT here — they don't exist on the other event type, so each hook still redacts its own
 * type-specific field(s) directly, right after calling this.
 */
function redactCommonEventFields<E extends RedactableCommonEventFields>(event: E): E {
  if (typeof event.transaction === 'string') {
    event.transaction = redactUrlPath(event.transaction)
  }
  if (event.request?.url) {
    event.request = { ...event.request, url: sanitizeUrl(event.request.url) }
  }
  if (event.request) {
    event.request = redactPii(event.request)
  }
  if (event.contexts) {
    event.contexts = redactPii(event.contexts)
  }
  if (event.extra) {
    event.extra = redactPii(event.extra)
  }
  if (event.tags) {
    event.tags = redactPii(event.tags)
  }
  if (event.breadcrumbs) {
    event.breadcrumbs = redactPii(event.breadcrumbs)
  }
  if (event.user) {
    event.user = redactPii(event.user)
  }
  if (event.sdkProcessingMetadata) {
    event.sdkProcessingMetadata = redactPii(event.sdkProcessingMetadata)
  }
  if (typeof event.server_name === 'string') {
    event.server_name = redactUrlPath(event.server_name)
  }
  return event
}

export function sentryScrubbingOptions(): {
  beforeBreadcrumb: (breadcrumb: Breadcrumb) => Breadcrumb | null
  beforeSend: (event: ErrorEvent, hint: EventHint) => ErrorEvent | null
  beforeSendTransaction: (event: TransactionEvent, hint: EventHint) => TransactionEvent | null
  beforeSendSpan: BeforeSendSpanFn
} {
  return {
    beforeBreadcrumb(breadcrumb) {
      if (breadcrumb.category === 'console') return null
      if (breadcrumb.data) {
        breadcrumb.data = redactPii(breadcrumb.data)
      }
      // Issue #1514 (fix round 7): `breadcrumb.message` (e.g. a 'navigation' breadcrumb's
      // "Navigated to <url>" text) was never touched — only `breadcrumb.data` was. A
      // capability URL (the account-deletion/household-invite page a user just navigated
      // to) reaches this field directly, not nested under `data`.
      if (typeof breadcrumb.message === 'string') {
        breadcrumb.message = redactUrlPath(breadcrumb.message)
      }
      return breadcrumb
    },
    // Issue #1514 (fix round 6, widened round 11): `redactCommonEventFields` covers
    // `transaction`/`request`/`contexts`/`extra`/`tags`/`user`/`breadcrumbs`/
    // `sdkProcessingMetadata`/`server_name` — see that function's doc comment for why the
    // walk is shared with `beforeSendTransaction` rather than reimplemented here. `exception`
    // is the one field unique to error events, redacted directly below: `redactPii` covers
    // `event.exception`'s `value` (message) field and stack-frame `vars`, and also every
    // linked-error entry `linkedErrorsIntegration` adds for a `.cause` chain (probe-confirmed
    // round 10 — see `useDeletion.ts`'s `sanitizeError` — there is no bypass via `.cause`).
    beforeSend(event) {
      redactCommonEventFields(event)
      if (event.exception) {
        event.exception = redactPii(event.exception)
      }
      return event
    },
    // Issue #1514 (fix round 9, widened rounds 10-11): `beforeSend` is ERROR/message-only —
    // it never sees performance transaction events, which are a completely separate
    // pipeline. With `tracesSampleRate: 0.1` and `browserTracingIntegration()` enabled below,
    // roughly 1 in 10 page views of a capability route (e.g. /cancel-deletion/{token})
    // emitted a transaction whose OWN name is the raw pathname — bypassing every fix in
    // rounds 6-8 entirely, since none of them touch this hook.
    //
    // Verified empirically (not guessed) three times, with a real transport recording the
    // actual envelope instead of sending it:
    //   - Round 9's probe: a real pageload at a token-bearing path put the token in
    //     `event.transaction` (the transaction "name"),
    //     `event.contexts.trace.data['url.path']`/`['url.full']`, and `event.request.url`.
    //   - Round 10's probe fixed round 9's own mistake — it found the token surviving in
    //     `tags.route` (browser tracing sets route-shaped tags) because round 9 only fixed
    //     the ONE field it was told about. That probe injected every field a
    //     `TransactionEvent` can carry to check all of them at once: `tags`, `measurements`,
    //     `user`, `sdkProcessingMetadata` (carries `dynamicSamplingContext.transaction` — the
    //     raw transaction name again, propagated for distributed tracing), `breadcrumbs`, and
    //     `server_name`.
    //   - Round 11's probe found the SAME asymmetry one level up: `beforeSend` (errors) had
    //     never been widened to match `beforeSendTransaction`'s round-10 field set, even
    //     though `browserTracingIntegration` populates `tags`/`transaction` on error events
    //     too (confirmed live: `event.transaction` and `tags.route` both carried the raw
    //     token on a real captured exception). `redactCommonEventFields` is the fix — one
    //     function both hooks call, so this asymmetry cannot recur silently.
    //
    // `spans`/`measurements` are the two fields unique to transaction events (no `exception`
    // field exists here), redacted directly below.
    beforeSendTransaction(event) {
      redactCommonEventFields(event)
      if (event.spans) {
        event.spans = redactPii(event.spans)
      }
      if (event.measurements) {
        event.measurements = redactPii(event.measurements)
      }
      return event
    },
    // Issue #1514 (fix round 9): child spans (e.g. an instrumented fetch/XHR call to a
    // capability route) are ALSO reachable independently of `beforeSendTransaction` — this
    // hook runs once per span. Verified the same way: a span's `description` (e.g.
    // "GET /households/invites/{token}/accept") and `data['url.path']`/`['url.full']`
    // carry the token exactly like the transaction-level fields above.
    beforeSendSpan(span) {
      if (typeof span.description === 'string') {
        span.description = redactUrlPath(span.description)
      }
      return redactPii(span)
    },
  }
}

/**
 * Initialize Sentry from runtime config.
 * Falls back to build-time env vars for Cloudflare Pages deployments.
 * No-op if SENTRY_DSN is not configured.
 */
export function initSentry(): void {
  const dsn = window.ENV?.SENTRY_DSN || import.meta.env.VITE_SENTRY_DSN
  if (!dsn) return

  Sentry.init({
    dsn,
    environment: window.ENV?.ENVIRONMENT || import.meta.env.VITE_ENVIRONMENT || 'production',
    tracesSampleRate: 0.1,
    replaysSessionSampleRate: 0,
    replaysOnErrorSampleRate: 1,
    integrations: [Sentry.browserTracingIntegration()],
    ...sentryScrubbingOptions(),
  })
}

export { Sentry }
