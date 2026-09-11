/**
 * The reviewed-and-accepted set of deliberately-safe POST operations that `readExecute` admits.
 *
 * ⚠ This module is deliberately SEPARATE from `safe-post-extension.ts`, and must stay
 * dependency-free. The admission gate (`mcp/tools/read-execute-admission.ts`) reads this list at
 * RUNTIME, inside the Cloudflare Worker. Its sibling `safe-post-extension.ts` imports `node:fs`,
 * `node:path` and `node:url` to scan `api/openapi.yaml`, which is a BUILD-time concern — that is
 * why `tsconfig.json` excludes it, exactly as it excludes `spec/index-builder.ts` and
 * `manifest/lazy-init-extension.ts`. Importing the constant from there dragged `node:` modules
 * into the Worker's own import graph and broke `bun run typecheck` (#1630).
 *
 * Add nothing here that imports anything.
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
 *
 * ── Issue #1630 Task B4: acknowledgement is a TUPLE, not a bare operationId ──────────────────
 * Admission used to be keyed on operationId alone: deleting the backend's `x-mcp-safe-post`
 * annotation, or moving `previewCategoryMatches` to PUT/PATCH/DELETE or a different path, would
 * leave it admitted forever, because nothing compared the CURRENT spec shape to what was
 * actually reviewed — only the id survived. `AcknowledgedSafePostOperation` carries method and
 * path alongside the id so the admission gate (`isReadExecuteAdmitted`) and the CI drift check
 * (`findSafePostDrift`, `safe-post-extension.ts`) can require an EXACT match against the current
 * OpenAPI spec, not merely "an operation with this id exists somewhere". A verb or path change
 * makes the tuple stop matching, which refuses the call and fails the CI gate — it does not
 * silently keep admitting a shape nobody reviewed.
 */
export interface AcknowledgedSafePostOperation {
  operationId: string
  method: string
  path: string
}

export const ACKNOWLEDGED_SAFE_POST_OPERATIONS: AcknowledgedSafePostOperation[] = [
  { operationId: 'previewCategoryMatches', method: 'POST', path: '/api/v1/budgets/categories/preview' },
]

/** Bare operationIds, derived from the tuple list above — for callers that only need names
 * (test titles, `it.each`, audit-log filtering) and have no reason to import the fuller shape. */
export const ACKNOWLEDGED_SAFE_POST_OPERATION_IDS: string[] = ACKNOWLEDGED_SAFE_POST_OPERATIONS.map(
  (op) => op.operationId,
)
