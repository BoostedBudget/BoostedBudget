/**
 * Scanner for the `x-mcp-lazy-init` OpenAPI extension (#1610).
 *
 * `readExecute` (execute.ts) admits any operation that is class `read`, non-confirmation-gated,
 * and uses a conventionally read-only HTTP verb — a property that is true of `getPrivacySettings`
 * and `getUserProfile` even though BOTH lazily `repo.Create` the caller's own default row on
 * first access. That was previously acknowledged only by a hardcoded array in
 * `mcp/tools/execute.test.ts` (`ACKNOWLEDGED_LAZY_INIT_ON_READ`) — a tripwire that pins the two
 * known exceptions but cannot see a THIRD one added later, because nothing in this package can
 * see the Go implementation to detect a write on its own.
 *
 * The backend now annotates each such `huma.Operation` literal with
 * `Extensions: map[string]any{"x-mcp-lazy-init": true}` (`privacy.go`, `profile.go`), which flows
 * into `api/openapi.yaml` as a sibling of `operationId` (huma.Operation.Extensions IS marshaled —
 * unlike Metadata, which is `yaml:"-"`). This module reads that extension directly from
 * api/openapi.yaml (independent of the generated spec-index.json, so it does not require carrying
 * `x-` extensions through SpecEntry) and exposes:
 *
 *  - `extractLazyInitOperationIds` / `readLazyInitOperationIds` — what the SPEC currently tags.
 *  - `ACKNOWLEDGED_LAZY_INIT_OPERATIONS` — the single source of truth for what has been reviewed
 *    and accepted, now shared by `check-manifest-completeness.ts` (a real CI gate) and
 *    `execute.test.ts` (which no longer hardcodes its own copy).
 *  - `findUnacknowledgedLazyInitOperations` — the diff between the two. A non-empty result means
 *    a new lazy-init-tagged operation was added to the Go layer without a human updating this
 *    list — exactly the gap the by-name-only tripwire could not close.
 */

import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { parse as parseYaml } from 'yaml'
import { HTTP_METHODS } from '../spec/index-builder'

const __dirname = dirname(fileURLToPath(import.meta.url))
const DEFAULT_OPENAPI_PATH = resolve(__dirname, '../../../../api/openapi.yaml')

/**
 * The two operations reviewed and accepted as admitted-but-lazy-init-writing (#1610). Any
 * operation the OpenAPI spec tags `x-mcp-lazy-init: true` that is NOT in this list is unreviewed
 * drift — see `findUnacknowledgedLazyInitOperations`.
 */
export const ACKNOWLEDGED_LAZY_INIT_OPERATIONS = ['getPrivacySettings', 'getUserProfile']

interface OpenAPIOperationWithExtensions {
  operationId?: string
  'x-mcp-lazy-init'?: unknown
}

interface OpenAPISpecLike {
  paths?: Record<string, Record<string, OpenAPIOperationWithExtensions | undefined> | undefined>
}

/**
 * Pure extraction over an already-parsed OpenAPI document — kept separate from file I/O so it
 * can be exercised against synthetic fixtures (see lazy-init-extension.test.ts).
 */
export function extractLazyInitOperationIds(spec: OpenAPISpecLike): string[] {
  const ids: string[] = []
  for (const pathItem of Object.values(spec.paths ?? {})) {
    if (!pathItem) continue
    for (const method of HTTP_METHODS) {
      const op = pathItem[method]
      if (op?.operationId && op['x-mcp-lazy-init'] === true) {
        ids.push(op.operationId)
      }
    }
  }
  return ids.sort()
}

/** Reads and parses api/openapi.yaml (or `openapiPath`, for tests), then extracts. */
export function readLazyInitOperationIds(openapiPath: string = DEFAULT_OPENAPI_PATH): string[] {
  const spec = parseYaml(readFileSync(openapiPath, 'utf-8')) as OpenAPISpecLike
  return extractLazyInitOperationIds(spec)
}

/**
 * Set difference: operation IDs the spec tags `x-mcp-lazy-init: true` that are NOT in
 * `acknowledged`. Non-empty means a new admitted-read-that-writes operation was added without a
 * human reviewing and acknowledging it here.
 *
 * Deliberately one-directional: an acknowledged operation the spec no longer tags is a separate
 * (and fine) drift — it means the operation was removed or the extension was dropped, not that
 * something new and unreviewed showed up.
 */
export function findUnacknowledgedLazyInitOperations(specTagged: string[], acknowledged: string[]): string[] {
  const acknowledgedSet = new Set(acknowledged)
  return specTagged.filter((id) => !acknowledgedSet.has(id)).sort()
}
