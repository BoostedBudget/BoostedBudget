/**
 * Scanner for the `x-mcp-read-side-effect` OpenAPI extension (#1610).
 *
 * `readExecute` admits reviewed non-destructive, caller-scoped operations. A GET can satisfy that
 * policy without being side-effect-free: `getPrivacySettings` may create caller-owned defaults,
 * while `getUserProfile` attempts a best-effort last-login update whose failure does not fail the
 * read and whose response retains the pre-update timestamp. The backend tags such operations so
 * the manifest completeness gate can detect an unreviewed addition.
 */

import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { parse as parseYaml } from 'yaml'
import { HTTP_METHODS } from '../spec/index-builder'

const __dirname = dirname(fileURLToPath(import.meta.url))
const DEFAULT_OPENAPI_PATH = resolve(__dirname, '../../../../api/openapi.yaml')

/** Reviewed admitted GETs with caller-scoped read side effects. */
export const ACKNOWLEDGED_READ_SIDE_EFFECT_OPERATIONS = [
  'getPrivacySettings',
  'getUserProfile',
]

interface OpenAPIOperationWithExtensions {
  operationId?: string
  'x-mcp-read-side-effect'?: unknown
  'x-mcp-lazy-init'?: unknown
}

interface OpenAPISpecLike {
  paths?: Record<string, Record<string, OpenAPIOperationWithExtensions | undefined> | undefined>
}

export interface LegacyLazyInitOccurrence {
  operationId: string
  method: string
  path: string
}

/**
 * Finds every use of the removed extension. Presence is invalid regardless of its value: keeping
 * the old key as `false` would still advertise a stale contract and invite future reuse.
 */
export function extractLegacyLazyInitOccurrences(
  spec: OpenAPISpecLike,
): LegacyLazyInitOccurrence[] {
  const occurrences: LegacyLazyInitOccurrence[] = []
  for (const [path, pathItem] of Object.entries(spec.paths ?? {})) {
    if (!pathItem) continue
    for (const method of HTTP_METHODS) {
      const operation = pathItem[method]
      if (operation && 'x-mcp-lazy-init' in operation) {
        occurrences.push({
          operationId: operation.operationId ?? '<missing operationId>',
          method: method.toUpperCase(),
          path,
        })
      }
    }
  }
  return occurrences.sort((a, b) =>
    `${a.path}\0${a.method}\0${a.operationId}`.localeCompare(`${b.path}\0${b.method}\0${b.operationId}`),
  )
}

/** Pure extraction over an already-parsed OpenAPI document. */
export function extractReadSideEffectOperationIds(spec: OpenAPISpecLike): string[] {
  const ids: string[] = []
  for (const pathItem of Object.values(spec.paths ?? {})) {
    if (!pathItem) continue
    for (const method of HTTP_METHODS) {
      const operation = pathItem[method]
      if (operation?.operationId && operation['x-mcp-read-side-effect'] === true) {
        ids.push(operation.operationId)
      }
    }
  }
  return ids.sort()
}

/** Reads api/openapi.yaml (or `openapiPath`, for tests), then extracts the tagged operations. */
export function readReadSideEffectOperationIds(
  openapiPath: string = DEFAULT_OPENAPI_PATH,
): string[] {
  const spec = parseYaml(readFileSync(openapiPath, 'utf-8')) as OpenAPISpecLike
  return extractReadSideEffectOperationIds(spec)
}

/** Reads api/openapi.yaml (or `openapiPath`) and reports removed extension occurrences. */
export function readLegacyLazyInitOccurrences(
  openapiPath: string = DEFAULT_OPENAPI_PATH,
): LegacyLazyInitOccurrence[] {
  const spec = parseYaml(readFileSync(openapiPath, 'utf-8')) as OpenAPISpecLike
  return extractLegacyLazyInitOccurrences(spec)
}

/** Returns spec-tagged operations that have not been explicitly reviewed and acknowledged. */
export function findUnacknowledgedReadSideEffectOperations(
  specTagged: string[],
  acknowledged: string[],
): string[] {
  const acknowledgedSet = new Set(acknowledged)
  return specTagged.filter((id) => !acknowledgedSet.has(id)).sort()
}
