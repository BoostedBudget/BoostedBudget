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
 * execute.ts's module doc for `getPrivacySettings` / `getUserProfile`, which are GET yet lazily
 * write on first access.
 */
export const READ_ONLY_METHODS = new Set(['GET', 'HEAD', 'OPTIONS'])

const ACKNOWLEDGED_SAFE_POST_SET = new Set(ACKNOWLEDGED_SAFE_POST_OPERATIONS)

/**
 * True when readExecute admits a call to this operation. All of:
 *   1. capability.class === 'read', AND
 *   2. capability.confirmationRequired === false, AND
 *   3. EITHER the method is conventionally read-only (GET/HEAD/OPTIONS), OR the operationId is
 *      in ACKNOWLEDGED_SAFE_POST_OPERATIONS — a POST reviewed and proven to write nothing
 *      (#1630; see safe-post-extension.ts). `runMCPRecipe` satisfies 1 and 2 but is a POST NOT
 *      in that list, so it stays refused — the deliberate counter-example pinned in
 *      execute.test.ts.
 */
export function isReadExecuteAdmitted(capability: CapabilityEntry, method: string): boolean {
  if (capability.class !== 'read' || capability.confirmationRequired !== false) return false
  if (READ_ONLY_METHODS.has(method.toUpperCase())) return true
  return ACKNOWLEDGED_SAFE_POST_SET.has(capability.operationId)
}
