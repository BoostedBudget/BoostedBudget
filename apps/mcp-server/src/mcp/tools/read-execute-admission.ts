/**
 * The readExecute admission predicate (#1630), factored out of execute.ts so search.ts can use
 * the SAME logic to disclose the SAME truth — previously search()'s `(read)` label only echoed
 * `capability.class`, so an operation that is class 'read' but refused by readExecute (a POST
 * without the safe-post annotation, e.g. `runMCPRecipe`) was labelled identically to one
 * readExecute actually admits. Both call sites now import this one function; there is exactly
 * one place the three-part gate is expressed.
 *
 * See mcp/tools/execute.ts's module doc ("THE SECURITY POINT") for why class alone, or class
 * plus confirmationRequired, is not a safe admission test.
 */

import type { CapabilityEntry } from '../../manifest/capability-manifest'
import { ACKNOWLEDGED_SAFE_POST_OPERATIONS } from '../../manifest/safe-post-operations'

/**
 * HTTP methods readExecute treats as conventionally non-mutating. This is a convention the gate
 * requires, not proof that no method in this set can produce a server-side write — see
 * execute.ts's module doc: `getPrivacySettings` may create caller-owned defaults on first access,
 * while `getUserProfile` attempts a best-effort last-login update whose response retains the
 * timestamp fetched before the update.
 */
export const READ_ONLY_METHODS = new Set(['GET', 'HEAD', 'OPTIONS'])

/** The minimal spec-index shape isReadExecuteAdmitted needs — satisfied by ExecuteSpecEntry
 * (mcp/tools/execute.ts) and by search's SpecEntry (spec/search-runtime.ts) without either
 * needing to import the other's type. */
export interface ReadExecuteAdmissionSpec {
  method: string
  path: string
}

/**
 * True when readExecute admits a call to this operation. All of:
 *   1. capability.class === 'read', AND
 *   2. capability.confirmationRequired === false, AND
 *   3. EITHER spec.method is conventionally read-only (GET/HEAD/OPTIONS), OR (capability.
 *      operationId, spec.method, spec.path) exactly matches one entry in
 *      ACKNOWLEDGED_SAFE_POST_OPERATIONS — a POST reviewed and proven to write nothing (#1630;
 *      see safe-post-extension.ts). `runMCPRecipe` satisfies 1 and 2 but is a POST whose tuple is
 *      NOT in that list, so it stays refused — the deliberate counter-example pinned in
 *      execute.test.ts.
 *
 * Issue #1630 Task B4: admission is anchored to the FULL (operationId, method, path) tuple, not
 * the operationId alone. `spec` is the caller's own CURRENT reading of the generated spec index
 * (ExecuteSpecEntry / SpecEntry), so if the operation MOVES to a different HTTP method or path,
 * the tuple this function compares against no longer matches what was reviewed — the call is
 * refused rather than staying admitted on a stale id match.
 *
 * ⚠ This function does NOT protect against the backend's `x-mcp-safe-post` annotation being
 * removed while the operationId/method/path stay the same. At runtime it only ever compares
 * against the hardcoded `ACKNOWLEDGED_SAFE_POST_OPERATIONS` list (`safe-post-operations.ts`) —
 * it never reads `api/openapi.yaml` or the annotation itself, and by design can't: that module
 * is kept dependency-free specifically so this Worker-bundled code pulls in no `node:` imports
 * (see that file's header comment). If the annotation is deleted but the tuple is untouched,
 * this function still returns true; runtime admission and the backend annotation can silently
 * disagree.
 *
 * The actual protection for that case is BUILD-time, not runtime: CI's
 * `scripts/check-manifest-completeness.ts` re-derives the currently-tagged operations from
 * `api/openapi.yaml` (via `safe-post-extension.ts`'s `extractSafePostOperations`, which is where
 * the `node:fs` read happens) and calls `findSafePostDrift` to diff them against
 * `ACKNOWLEDGED_SAFE_POST_OPERATIONS` in both directions — an acknowledged tuple the spec no
 * longer tags `x-mcp-safe-post: true` at fails the build as "stale". So removing the annotation
 * is caught before the next deploy ships it, not by this function at request time. Until a
 * human updates `safe-post-operations.ts` to match, the CI gate is what stands between "annotation
 * removed" and "runtime keeps admitting it" — not this code.
 */
export function isReadExecuteAdmitted(capability: CapabilityEntry, spec: ReadExecuteAdmissionSpec): boolean {
  if (capability.class !== 'read' || capability.confirmationRequired !== false) return false
  const method = spec.method.toUpperCase()
  if (READ_ONLY_METHODS.has(method)) return true
  return ACKNOWLEDGED_SAFE_POST_OPERATIONS.some(
    (op) => op.operationId === capability.operationId && op.method.toUpperCase() === method && op.path === spec.path,
  )
}
