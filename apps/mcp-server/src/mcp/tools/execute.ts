/**
 * MCP `readExecute` / `writeExecute` tools — chain API calls through the host-proxy.
 *
 * Issue #1610b split the original single `execute` tool into two, hard cut (no alias kept —
 * production has zero users, and MCP clients re-enumerate tools on connect, so a retained
 * alias would leave the agent three overlapping choices instead of two clear ones):
 *
 *   readExecute()  — only calls admitted by the three-part gate below (see the security note and
 *                    "readExecute's REAL guarantee" below — it is NOT a proof of no side effects).
 *                    Takes no `confirmationToken` — under that gate, one is unreachable.
 *   writeExecute() — every class, confirmation-gated writes included. Reads stay legal inside a
 *                    write batch (a read-then-write chain must keep working); the property that
 *                    matters is that a client can bind readExecute ALONE.
 *
 * Each call is described by:
 *   operationId   — the API operation to invoke (from search())
 *   params        — query parameters
 *   body          — request body for write operations
 *   pathParams    — path parameters (e.g. { id: "abc" })
 *
 * writeExecute's write operations require a confirmation token (issued by the host-proxy on the
 * first attempt). Pass the token back in confirmationToken to confirm.
 *
 * The host-proxy enforces: manifest gate, write gating, credential injection, rate limits, and
 * error redaction. This tool only handles orchestration (plus, for readExecute, the additional
 * read-only admission gate below).
 *
 * ── THE SECURITY POINT: `capability.class === 'read'` alone is NOT a safe gate ──────────────
 * `excluded()` in capability-manifest.ts hard-codes `class: 'read'` onto EVERY excluded entry,
 * regardless of the operation's real HTTP method or behaviour — `GET /api/v1/user/delete/cancel/
 * {token}` is genuinely state-changing (backend/internal/services/deletion_service.go:373-375)
 * and its manifest entry is `excluded()`, which reads `class: 'read'`. It is blocked today only
 * because `excluded` is checked first. And `runMCPRecipe` — a REAL, non-excluded, invocable
 * operation — is `read('runMCPRecipe')` (confirmationRequired: false, by construction of the
 * `read()` helper) yet its HTTP method is POST: a case where the class is honestly 'read' and
 * confirmationRequired is honestly false, but the method still isn't safe to admit on class
 * alone without also checking the verb.
 *
 * So readExecute admits a call only when ALL of:
 *   1. capability.class === 'read', AND
 *   2. capability.confirmationRequired === false, AND
 *   3. EITHER the spec entry's method is in READ_ONLY_METHODS, OR the operationId is in
 *      ACKNOWLEDGED_SAFE_POST_OPERATIONS (mcp/tools/read-execute-admission.ts /
 *      manifest/safe-post-extension.ts, #1630) — a reviewed, human-maintained, explicitly
 *      enumerated list of POSTs proven (by an integration test asserting no rows and no audit
 *      entries change) to write nothing. `previewCategoryMatches` is the sole entry.
 * Any of the first two alone is bypassable — see the two real counter-examples above (the first
 * defeats class-alone, the second defeats class+confirmationRequired without also checking the
 * verb). `runMCPRecipe` has the EXACT SAME shape as `previewCategoryMatches` (class 'read',
 * confirmationRequired false, method POST) yet is NOT in ACKNOWLEDGED_SAFE_POST_OPERATIONS and
 * so stays refused — the difference between the two is not inferred from the method or the
 * manifest, it is a deliberate, reviewed, enumerable exception. A call that fails the gate is
 * refused, naming writeExecute as the alternative.
 *
 * ── readExecute's REAL guarantee (not "read-only by construction") ─────────────────────────
 * Passing all three checks proves an admitted call is non-destructive, not confirmation-gated,
 * scoped to the authenticated caller's own data, and made with a conventionally non-mutating
 * HTTP verb. It does NOT prove the call has no server-side side effect: `getPrivacySettings`
 * and `getUserProfile` (capability-manifest.ts, "Privacy / Profile" section) pass the gate —
 * class 'read', method GET, confirmationRequired false — yet their backend handlers
 * (privacy_service.go / user profile equivalent) lazily `repo.Create` the caller's own default
 * settings/profile row on first access, per their own OpenAPI descriptions. That write is
 * scoped to the caller, idempotent after the first call, and not destructive of anything — which
 * is why it was left admitted rather than excluded — but it is a write, and "readExecute cannot
 * write" is false. Do not build anything on a stronger claim than the one above; see
 * `readExecuteRefusal`'s test file for the two operations pinned by name.
 */

import { getCapability } from '../../manifest/capability-manifest'
import type { CapabilityEntry, OperationClass as AuditOperationClass } from '../../manifest/capability-manifest'
import { HostProxyError } from '../../sandbox/host-proxy'
import { isReadExecuteAdmitted, READ_ONLY_METHODS } from './read-execute-admission'
import type { McpToolResult } from './search'

export interface ExecuteCall {
  operationId: string
  params?: Record<string, unknown>
  body?: Record<string, unknown>
  pathParams?: Record<string, string>
}

/** readExecute's input — deliberately has NO confirmationToken field: under the three-way
 * admission gate one is unreachable by construction, so omitting it is the ergonomic win. */
export interface ReadExecuteInput {
  calls: ExecuteCall[]
}

/** writeExecute's input — accepts every class and keeps confirmationToken. */
export interface WriteExecuteInput {
  calls: ExecuteCall[]
  confirmationToken?: string
}

export interface HostProxy {
  invoke(operationId: string, args: Record<string, unknown>, opts?: { confirmationToken?: string; idempotencyKey?: string }): Promise<unknown>
  /**
   * #1625: batch-level confirmation gate. Optional so existing test doubles that only stub
   * invoke() keep working — when absent, the per-call gate inside invoke() still applies.
   */
  confirmBatch?(
    gated: Array<{ operationId: string; args: Record<string, unknown> }>,
    token?: string,
  ): Promise<{ needsConfirmation: boolean; token?: string; count: number }>
}

/**
 * Per-call audit event surfaced to the caller (agent.ts) so it can record
 * the correct operationClass and result for each executed operation.
 * Previously agent.ts hardcoded operationClass:'read' for all execute calls —
 * confirmed writes and confirmation challenges were logged incorrectly.
 */
export interface ExecuteAuditEvent {
  operationId: string
  operationClass: AuditOperationClass
  result: 'success' | 'confirmation_required' | 'denied' | 'error'
}

/** Return type for the execute handlers — includes tool output AND per-call audit trail */
export interface ExecuteResult {
  toolResult: McpToolResult
  auditEvents: ExecuteAuditEvent[]
}

/** A spec-index entry, reduced to the fields buildRequestArgs / deniedOperationClass need. */
export interface ExecuteSpecEntry {
  operationId: string
  method: string
  path: string
}

/**
 * Build the URL for an operation, substituting path params.
 * Falls back to a lookup from the spec index for the path template.
 */
function buildRequestArgs(
  call: ExecuteCall,
  specEntries: ExecuteSpecEntry[],
): Record<string, unknown> {
  const spec = specEntries.find((e) => e.operationId === call.operationId)
  if (!spec) {
    return { url: `/api/v1/unknown/${call.operationId}`, method: 'GET', params: call.params }
  }

  // Substitute path params
  let path = spec.path
  if (call.pathParams) {
    for (const [key, value] of Object.entries(call.pathParams)) {
      path = path.replace(`{${key}}`, encodeURIComponent(value))
    }
  }

  const args: Record<string, unknown> = {
    url: path,
    method: spec.method,
  }
  if (call.params) args.params = call.params
  if (call.body) args.data = call.body

  return args
}

/**
 * Max characters of a single call's JSON result surfaced in the tool output.
 *
 * MCP results are consumed by a model, not a human — an unbounded result risks
 * blowing the client's context window on a single tool call. But the cap must
 * be large enough to hold realistic list responses (transactions, accounts,
 * budgets) or the tool becomes useless for exactly the operations that matter
 * most. 100_000 chars (~25k tokens at a rough 4 chars/token) comfortably fits
 * a page of most list endpoints while still bounding worst case. No documented
 * MCP or Cloudflare Workers response-size limit is lower than this for a
 * single tool-call text block, so this constant IS the limit.
 *
 * Issue #1609 item 8: the previous cap (500 chars, unnamed, silent) truncated
 * production traffic without any marker — on 2026-09-07 a user's MCP client
 * silently lost merchant names and the reversal field out of a transactions
 * response and fell back to reconstructing the answer from analytics
 * endpoints, reporting a confident answer built on a fragment. Truncation
 * must now be large enough to rarely fire, and self-describing when it does.
 */
const MAX_RESULT_CHARS = 100_000

/**
 * Max characters of the WHOLE BATCH's rendered output, across every call in one
 * readExecute()/writeExecute() invocation (issue #1612).
 *
 * MAX_RESULT_CHARS bounds a single call's result, but `calls` is capped at 25
 * (agent.ts) — 25 * 100_000 = a 2.5 MB aggregate a client could still receive in
 * one tool response, entirely unbounded by the per-call cap. This bounds the
 * TOTAL rendered text across the batch independent of how many individual
 * calls stayed under MAX_RESULT_CHARS. 300_000 chars (~75k tokens) is 3x a
 * single result's cap — generous enough that a handful of large-but-legitimate
 * list responses still fit in one batch, while still bounding the worst case
 * far below the uncapped 2.5 MB.
 */
const MAX_BATCH_RESULT_CHARS = 300_000

/**
 * Render a call's JSON result, truncating to MAX_RESULT_CHARS and appending an
 * explicit marker when truncation occurs. The marker states the fact of
 * truncation, the shown size, the full size, and an actionable hint — it MUST
 * NOT include any sample of the omitted content (sizes only; no financial
 * data in the marker itself).
 */
function renderResult(result: unknown): string {
  // #1625: a 204 No Content arrives here as `undefined` (fetch-mutator.ts maps it), and
  // JSON.stringify(undefined) returns the VALUE undefined, not a string — so `.length` threw a
  // TypeError that was not a HostProxyError, landing in the generic "unexpected error" branch,
  // which ALSO break()s the rest of the batch. The delete had already committed server-side, so
  // the agent was told a successful write had failed and typically retried it.
  // `?? 'null'` covers the same hole for any other value JSON.stringify returns undefined for
  // (a bare function or symbol), which cannot reach here today but would fail identically.
  if (result === undefined) return 'No Content (204) — the operation succeeded and returned no body.'
  const full = JSON.stringify(result) ?? 'null'
  if (full.length <= MAX_RESULT_CHARS) return full

  const shown = full.slice(0, MAX_RESULT_CHARS)
  return (
    `${shown}\n… [truncated: showing ${MAX_RESULT_CHARS} of ${full.length} characters. ` +
    `Narrow the query — add filters, a date range, or a smaller limit — to receive the complete result.]`
  )
}

/**
 * The class to record for a call the manifest gate REFUSED — an excluded operation, or an
 * operationId that is in no manifest at all.
 *
 * ⚠ HISTORY, because the reason this function exists has changed. Round 14, finding CC-1: the
 * permitted path used to derive its class from `capability.confirmationRequired`, which is the
 * field capability-manifest.ts's `excluded()` helper hard-codes to `false` for every excluded
 * entry — so a denied `purgeAccount` was recorded as `operation_class = 'read'`. The HTTP method
 * was adopted as the honest source because the manifest genuinely had no opinion to read.
 *
 * Issue #1569 Task 16 (F1) fixed the underlying defect: excluded entries now carry their TRUE
 * class (excludedWrite() / destructive({ excluded: true })), and two spec-index-driven guards in
 * capability-summary.drift.test.ts keep every GET at 'read' and every state-changing method at
 * 'write' or 'destructive'. So the manifest IS now the honest source, and this function reads it
 * first. `purgeAccount` is recorded as 'destructive', not 'read' and not merely 'write'.
 *
 * The HTTP-method path survives only as the fallback for an operationId with no manifest entry
 * but a spec entry, and the no-evidence-at-all fallback is still `write` — the fail-LOUD
 * direction: for a hallucinated or misspelled operationId, a denial that over-states its class
 * costs an operator one dismissed row, while one that under-states it is invisible to the query
 * that would have found it.
 */
function deniedOperationClass(
  operationId: string,
  specEntries: ExecuteSpecEntry[],
): AuditOperationClass {
  const capability = getCapability(operationId)
  if (capability) return capability.class
  const spec = specEntries.find((e) => e.operationId === operationId)
  if (!spec) return 'write'
  return READ_ONLY_METHODS.has(spec.method.toUpperCase()) ? 'read' : 'write'
}

/**
 * readExecute's admission gate (see the module doc's SECURITY POINT). Returns a refusal
 * message when the call must NOT be admitted, or undefined to proceed.
 */
function readExecuteRefusal(
  call: ExecuteCall,
  capability: CapabilityEntry,
  specEntries: ExecuteSpecEntry[],
): string | undefined {
  const spec = specEntries.find((e) => e.operationId === call.operationId)
  const admitted = !!spec && isReadExecuteAdmitted(capability, spec.method)

  return admitted
    ? undefined
    : `not available via readExecute — use writeExecute for this operation`
}

interface BuildHandlerOptions {
  /** writeExecute threads confirmationToken through; readExecute has no such input field. */
  acceptsConfirmationToken: boolean
  /** readExecute's extra admission gate. Absent for writeExecute — reads stay legal inside a
   * write batch, so a write handler applies no additional per-call restriction. */
  admissionGate?: (call: ExecuteCall, capability: CapabilityEntry, specEntries: ExecuteSpecEntry[]) => string | undefined
}

/**
 * Shared core for readExecute and writeExecute — both walk the same batch, hit the same
 * manifest gate, and render results/truncation/audit events identically. Only the admission
 * gate and whether a confirmationToken is honoured differ between them.
 */
function buildHandlerCore(
  proxy: HostProxy,
  specEntries: ExecuteSpecEntry[],
  opts: BuildHandlerOptions,
): (input: { calls: ExecuteCall[]; confirmationToken?: string }) => Promise<ExecuteResult> {
  return async (input): Promise<ExecuteResult> => {
    const { calls } = input
    const confirmationToken = opts.acceptsConfirmationToken ? input.confirmationToken : undefined

    if (!calls || calls.length === 0) {
      return {
        toolResult: { content: [{ type: 'text', text: 'No operations specified. Provide at least one call.' }] },
        auditEvents: [],
      }
    }

    const results: string[] = []
    const auditEvents: ExecuteAuditEvent[] = []
    let batchCharsUsed = 0

    // ── #1625: confirm the WHOLE batch before executing any of it ────────────────────────
    //
    // A confirmation token used to be bound to one (operationId, argsHash) and burned on use,
    // while this handler passed that same single token to every gated call. So a batch with two
    // or more gated writes could never complete: the first write consumed the token, the second
    // got `invalid_confirmation`, and the batch broke — after the first write had already
    // committed. Retrying re-executed it. That is the createBudget loop.
    //
    // Now one token covers the entire gated batch, and the challenge round executes NOTHING, so
    // a refused or abandoned confirmation can never leave a half-applied batch behind.
    if (opts.acceptsConfirmationToken && proxy.confirmBatch) {
      // Hand over the WHOLE batch. confirmBatch decides which calls are gated and hashes all of
      // them, so the token is bound to the identical calls array the messages promise — an
      // ungated read can no longer be swapped under a valid token. `gated` is still computed here,
      // but only to shape the audit events.
      const canonical = calls.map((call) => ({
        operationId: call.operationId,
        args: buildRequestArgs(call, specEntries),
      }))
      const gated = canonical.filter((c) => {
        const capability = getCapability(c.operationId)
        return Boolean(capability && !capability.excluded && capability.confirmationRequired)
      })

      if (gated.length > 0) {
        try {
          const decision = await proxy.confirmBatch(canonical, confirmationToken)
          if (decision.needsConfirmation) {
            const plural = decision.count === 1 ? 'operation requires' : 'operations require'
            return {
              toolResult: {
                content: [
                  {
                    type: 'text',
                    text:
                      `⚠️ ${decision.count} ${plural} confirmation. Nothing has been executed.\n` +
                      `Re-invoke writeExecute() with the IDENTICAL calls array and ` +
                      `confirmationToken: "${decision.token}"`,
                  },
                ],
              },
              // No call ran, so the only honest audit record is one confirmation_required entry
              // per gated call — not a success or an error for any of them.
              auditEvents: gated.map((g) => ({
                operationId: g.operationId,
                operationClass: getCapability(g.operationId)?.class ?? 'write',
                result: 'confirmation_required' as const,
              })),
            }
          }
        } catch (err) {
          if (err instanceof HostProxyError) {
            return {
              toolResult: { content: [{ type: 'text', text: `❌ batch confirmation failed: ${err.message} (${err.code})` }] },
              auditEvents: gated.map((g) => ({
                operationId: g.operationId,
                operationClass: getCapability(g.operationId)?.class ?? 'write',
                result: 'error' as const,
              })),
            }
          }
          throw err
        }
      }
    }

    // Issue #1612 round 2: charge EVERY string that ends up in the returned text against the
    // budget, not just the rendered payload. Previously only `rendered.length` was counted while
    // the "✅ <operationId>: " prefixes, the ❌/⚠️ refusal and error lines, the '\n\n' separators
    // and the exhaustion marker were all appended afterward and charged to nothing — so the text
    // actually handed back could exceed MAX_BATCH_RESULT_CHARS. The `+ 2` is the separator
    // join('\n\n') will insert; charging it on every push (rather than every push but the last)
    // over-counts by exactly 2 characters per batch, which errs toward the cap, not past it.
    //
    // What this still does NOT make it: a hard cap on the returned string. The check is a
    // PRE-check — it stops the batch before starting the next call, so the final text is bounded
    // by MAX_BATCH_RESULT_CHARS + one call's rendered output (itself capped at MAX_RESULT_CHARS)
    // + the exhaustion marker. Bounding it exactly would mean truncating a result mid-render,
    // which would hand the model malformed JSON. State the real bound; do not claim the tighter one.
    const pushResult = (text: string): void => {
      batchCharsUsed += text.length + 2
      results.push(text)
    }

    for (const call of calls) {
      // Issue #1612: the batch aggregate is unbounded per-call but was unbounded in total.
      // Once the running total across this batch already exceeds the budget, stop — same
      // self-describing marker style as renderResult (shown/full-equivalent sizes, actionable
      // hint), and the same "break, not continue" idiom the manifest-denial path below uses:
      // once the batch can no longer usefully surface more output, the rest is not run.
      if (batchCharsUsed >= MAX_BATCH_RESULT_CHARS) {
        pushResult(
          `⚠️ Batch result budget exhausted after ${auditEvents.length} of ${calls.length} call(s) ` +
            `(${batchCharsUsed} of ${MAX_BATCH_RESULT_CHARS} character budget used). ` +
            `${calls.length - auditEvents.length} call(s) were not executed. Split this batch into ` +
            `smaller invocations, or narrow filters/limits to reduce individual result sizes.`,
        )
        break
      }

      const capability = getCapability(call.operationId)

      if (!capability || capability.excluded) {
        pushResult(`❌ ${call.operationId}: not available via MCP`)
        // Emit a denied audit event so the audit trail is complete (R5-9). Its class
        // comes from the HTTP method, NOT from the manifest — see
        // deniedOperationClass for why the manifest cannot answer here (CC-1).
        auditEvents.push({
          operationId: call.operationId,
          operationClass: deniedOperationClass(call.operationId, specEntries),
          result: 'denied',
        })
        break
      }

      if (opts.admissionGate) {
        const refusal = opts.admissionGate(call, capability, specEntries)
        if (refusal) {
          pushResult(`❌ ${call.operationId}: ${refusal}`)
          auditEvents.push({ operationId: call.operationId, operationClass: capability.class, result: 'denied' })
          break
        }
      }

      // The manifest class verbatim, since #1569 Task 16: it distinguishes a destructive call
      // from an ordinary write, which deriving from `confirmationRequired` could not (both are
      // confirmation-gated). An operator filtering mcp_audit_log for attempts against
      // data-destroying operations can now find them.
      const operationClass: AuditOperationClass = capability.class

      const args = buildRequestArgs(call, specEntries)

      try {
        const result = await proxy.invoke(call.operationId, args, {
          confirmationToken: capability.confirmationRequired ? confirmationToken : undefined,
        })
        const rendered = renderResult(result)
        pushResult(`✅ ${call.operationId}: ${rendered}`)
        auditEvents.push({ operationId: call.operationId, operationClass, result: 'success' })
      } catch (err) {
        if (err instanceof HostProxyError) {
          if (err.code === 'confirmation_required') {
            const token = err.confirmationToken
            pushResult(
              `⚠️ ${call.operationId} requires confirmation.\n` +
                `Re-invoke writeExecute() with confirmationToken: "${token}"`,
            )
            auditEvents.push({ operationId: call.operationId, operationClass, result: 'confirmation_required' })
          } else {
            pushResult(`❌ ${call.operationId}: ${err.message} (${err.code})`)
            auditEvents.push({ operationId: call.operationId, operationClass, result: 'error' })
          }
        } else {
          pushResult(`❌ ${call.operationId}: unexpected error`)
          auditEvents.push({ operationId: call.operationId, operationClass, result: 'error' })
        }
        break // stop on first error
      }
    }

    return {
      toolResult: { content: [{ type: 'text', text: results.join('\n\n') }] },
      auditEvents,
    }
  }
}

/**
 * Build the readExecute tool handler. Admits a call only when the three-way gate in
 * readExecuteRefusal holds — see the module doc's SECURITY POINT. No confirmationToken input.
 */
export function buildReadExecuteHandler(
  proxy: HostProxy,
  specEntries: ExecuteSpecEntry[] = [],
): (input: ReadExecuteInput) => Promise<ExecuteResult> {
  const core = buildHandlerCore(proxy, specEntries, {
    acceptsConfirmationToken: false,
    admissionGate: (call, capability, entries) => readExecuteRefusal(call, capability, entries),
  })
  return (input: ReadExecuteInput) => core({ calls: input.calls })
}

/**
 * Build the writeExecute tool handler. Accepts every capability class and threads
 * confirmationToken through to the host-proxy for confirmation-gated writes. Reads stay legal
 * inside a write batch — a read-then-write chain must keep working.
 */
export function buildWriteExecuteHandler(
  proxy: HostProxy,
  specEntries: ExecuteSpecEntry[] = [],
): (input: WriteExecuteInput) => Promise<ExecuteResult> {
  return buildHandlerCore(proxy, specEntries, { acceptsConfirmationToken: true })
}
