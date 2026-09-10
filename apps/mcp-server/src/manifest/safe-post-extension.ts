/**
 * Scanner for the `x-mcp-safe-post` OpenAPI extension (#1630).
 *
 * `readExecuteRefusal` (mcp/tools/execute.ts) admits an operation only when class is `read`,
 * `confirmationRequired` is `false`, AND the HTTP method is conventionally non-mutating (GET,
 * HEAD, OPTIONS). `previewCategoryMatches` is a POST — its body is a filter, not a resource to
 * persist — but its backend handler (CategoryPreviewService.PreviewCategoryMatches) issues only
 * SELECTs and writes nothing. The manifest used to note this with a comment
 * (`read('previewCategoryMatches'), // POST but non-mutating`) that had no effect on the
 * admission gate: the method check still refused it, so the comment and the actual behaviour
 * disagreed.
 *
 * This is the MIRROR IMAGE of `lazy-init-extension.ts`'s problem: that module documents a GET
 * that unexpectedly writes; this one documents a POST that unexpectedly doesn't. The mitigation
 * is copied part for part from that precedent:
 *
 * The backend now annotates the `huma.Operation` literal for such an operation with
 * `Extensions: map[string]any{"x-mcp-safe-post": true}` (`category_preview.go`), which flows
 * into `api/openapi.yaml` as a sibling of `operationId` (huma.Operation.Extensions IS marshaled —
 * unlike Metadata, which is `yaml:"-"`). This module reads that extension directly from
 * api/openapi.yaml (independent of the generated spec-index.json), and exposes:
 *
 *  - `extractSafePostOperationIds` / `readSafePostOperationIds` — what the SPEC currently tags.
 *  - `ACKNOWLEDGED_SAFE_POST_OPERATIONS` — the single source of truth for what has been reviewed
 *    and accepted, imported directly by `mcp/tools/read-execute-admission.ts` (the actual
 *    runtime admission decision) as well as by `check-manifest-completeness.ts` (a real CI gate)
 *    and by tests.
 *  - `findUnacknowledgedSafePostOperations` — the diff between the two. A non-empty result means
 *    a new safe-post-tagged operation was added to the Go layer without a human reviewing and
 *    adding it here — exactly the "explicit and enumerable, never inferred" property this
 *    annotation requires, since it deliberately weakens a security gate.
 *
 * Unlike lazy-init (which is documentation only — neither GET admits or excludes on it),
 * `ACKNOWLEDGED_SAFE_POST_OPERATIONS` here is READ AT RUNTIME by the admission gate itself. That
 * is deliberate and safe: it is a plain, hardcoded array literal (no filesystem access), so
 * importing it into the Cloudflare Workers runtime bundle is exactly as safe as importing any
 * other constant. Only `readSafePostOperationIds` touches `node:fs`, and only the CI script and
 * this module's own tests call it.
 */

import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { parse as parseYaml } from 'yaml'
import { HTTP_METHODS } from '../spec/index-builder'

const __dirname = dirname(fileURLToPath(import.meta.url))
const DEFAULT_OPENAPI_PATH = resolve(__dirname, '../../../../api/openapi.yaml')

/**
 * The operations reviewed and accepted as safe-POSTs — a POST, PUT, PATCH, or DELETE that is
 * genuinely non-mutating and therefore admissible via readExecute (#1630). Any operation the
 * OpenAPI spec tags `x-mcp-safe-post: true` that is NOT in this list is unreviewed drift — see
 * `findUnacknowledgedSafePostOperations`.
 *
 * `previewCategoryMatches` is the only entry: it is a POST whose handler
 * (CategoryPreviewService.PreviewCategoryMatches) issues only SELECT queries and writes nothing,
 * proven by TestPreviewCategoryMatches_WritesNothing_Integration.
 *
 * ⚠ `runMCPRecipe` is a deliberate NON-example — it is class `read`, `confirmationRequired:
 * false`, and its method is POST, exactly matching previewCategoryMatches' shape. It is NOT
 * annotated `x-mcp-safe-post` in the backend and must NEVER be added to this list: it composes
 * and returns ordered API calls for the CALLER to execute, which is a different thing from
 * proving its OWN handler writes nothing. See execute.test.ts's pinned refusal test.
 */
// Re-exported from the dependency-free module so the Worker runtime never reaches this file's
// node: imports. See safe-post-operations.ts for why.
export { ACKNOWLEDGED_SAFE_POST_OPERATIONS } from './safe-post-operations'

interface OpenAPIOperationWithExtensions {
  operationId?: string
  'x-mcp-safe-post'?: unknown
}

interface OpenAPISpecLike {
  paths?: Record<string, Record<string, OpenAPIOperationWithExtensions | undefined> | undefined>
}

/**
 * Pure extraction over an already-parsed OpenAPI document — kept separate from file I/O so it
 * can be exercised against synthetic fixtures (see safe-post-extension.test.ts).
 */
export function extractSafePostOperationIds(spec: OpenAPISpecLike): string[] {
  const ids: string[] = []
  for (const pathItem of Object.values(spec.paths ?? {})) {
    if (!pathItem) continue
    for (const method of HTTP_METHODS) {
      const op = pathItem[method]
      if (op?.operationId && op['x-mcp-safe-post'] === true) {
        ids.push(op.operationId)
      }
    }
  }
  return ids.sort()
}

/** Reads and parses api/openapi.yaml (or `openapiPath`, for tests), then extracts. */
export function readSafePostOperationIds(openapiPath: string = DEFAULT_OPENAPI_PATH): string[] {
  const spec = parseYaml(readFileSync(openapiPath, 'utf-8')) as OpenAPISpecLike
  return extractSafePostOperationIds(spec)
}

/**
 * Set difference: operation IDs the spec tags `x-mcp-safe-post: true` that are NOT in
 * `acknowledged`. Non-empty means a new admitted-non-read-method operation was added without a
 * human reviewing and acknowledging it here.
 *
 * Deliberately one-directional: an acknowledged operation the spec no longer tags is a separate
 * (and fine) drift — it means the operation was removed or the extension was dropped, not that
 * something new and unreviewed showed up.
 */
export function findUnacknowledgedSafePostOperations(specTagged: string[], acknowledged: string[]): string[] {
  const acknowledgedSet = new Set(acknowledged)
  return specTagged.filter((id) => !acknowledgedSet.has(id)).sort()
}
