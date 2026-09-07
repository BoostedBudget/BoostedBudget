/**
 * Spec index builder — BUILD-TIME script, not a Worker module.
 * Runs via: bun run src/spec/index-builder.ts
 *
 * Reads api/openapi.yaml → dereferences $refs → flattens each operation
 * into a token-efficient SpecEntry → writes generated/spec-index.json.
 *
 * The generated file is committed and CI-verified for drift (same pattern
 * as api/openapi.yaml and the Orval client).
 */

import { readFileSync, writeFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import $RefParser from '@apidevtools/json-schema-ref-parser'
import { parse as parseYaml } from 'yaml'
import { getCapability } from '../manifest/capability-manifest'
import type { SpecBodyField, SpecEntry, SpecIndex, SpecParameter } from './search-runtime'

// Paths relative to repo root
const __dirname = dirname(fileURLToPath(import.meta.url))
const REPO_ROOT = resolve(__dirname, '../../../../')
const OPENAPI_PATH = resolve(REPO_ROOT, 'api/openapi.yaml')
const OUTPUT_PATH = resolve(__dirname, 'generated/spec-index.json')

interface OpenAPIParameterSchema {
  type?: string | string[]
  enum?: string[]
  items?: { type?: string; enum?: string[] }
  [key: string]: unknown
}

interface OpenAPIParameter {
  name?: string
  in?: string
  required?: boolean
  description?: string
  schema?: OpenAPIParameterSchema
  [key: string]: unknown
}

/**
 * OpenAPI JSON Schema fragment, as it appears either inline or as the target of a `$ref`.
 * Deliberately loose — we only read the handful of keywords the flattener understands.
 */
interface OpenAPISchema {
  $ref?: string
  type?: string | string[]
  properties?: Record<string, OpenAPISchema>
  items?: OpenAPISchema
  required?: string[]
  enum?: unknown[]
  description?: string
  [key: string]: unknown
}

interface OpenAPIRequestBody {
  content?: Record<string, { schema?: OpenAPISchema } | undefined>
  required?: boolean
  [key: string]: unknown
}

interface OpenAPIOperation {
  operationId?: string
  summary?: string
  description?: string
  tags?: string[]
  security?: unknown[]
  parameters?: OpenAPIParameter[]
  requestBody?: OpenAPIRequestBody
}

interface OpenAPIPathItem {
  get?: OpenAPIOperation
  post?: OpenAPIOperation
  put?: OpenAPIOperation
  patch?: OpenAPIOperation
  delete?: OpenAPIOperation
  [key: string]: unknown
}

interface OpenAPISpec {
  paths?: Record<string, OpenAPIPathItem>
  components?: { schemas?: Record<string, OpenAPISchema> }
  [key: string]: unknown
}

/**
 * Resolve a `$ref` (and any chain of `$ref`s) against `components.schemas`, breaking on a
 * self-referential cycle (a schema whose `$ref` chain points back at a name already visited).
 * `buildSpecIndex` runs `$RefParser.dereference` first, so in production this is mostly a
 * no-op passthrough — but `indexFromSpec` is also exercised directly with synthetic,
 * UNDEREFERENCED fixtures in tests, so resolution must not depend on that prior pass.
 */
function resolveSchema(
  schema: OpenAPISchema | undefined,
  spec: OpenAPISpec,
  seen: Set<string> = new Set(),
): OpenAPISchema | undefined {
  if (!schema) return undefined
  if (!schema.$ref) return schema

  const refName = schema.$ref.replace(/^#\/components\/schemas\//, '')
  if (seen.has(refName)) return undefined // self-referential cycle — stop, don't recurse forever

  const resolved = spec.components?.schemas?.[refName]
  if (!resolved) return undefined

  const nextSeen = new Set(seen)
  nextSeen.add(refName)
  return resolveSchema(resolved, spec, nextSeen)
}

/** First non-`null` entry of a (possibly nullable-union) OpenAPI `type`. */
function primaryType(type: string | string[] | undefined): string | undefined {
  return Array.isArray(type) ? type.find((t) => t !== 'null') : type
}

// Nested objects / array-of-object items flatten to this many levels deep (dotted names).
// Depth 1 = the request body's own top-level fields; depth 2 = one level of nesting
// (`action.custom_category_id`, `conditions[].field`). Beyond that we mark the field as
// `object` / `array of object` without expanding it further — see the brief: "flatten... to
// a reasonable depth (2 is enough)... or clearly mark a nested object rather than recursing
// without bound."
const MAX_BODY_DEPTH = 2

/**
 * Flatten an object schema's properties into token-frugal SpecBodyField entries, recursing
 * into nested objects and array-of-object items up to MAX_BODY_DEPTH using dotted names.
 */
function flattenBodySchema(
  schema: OpenAPISchema | undefined,
  spec: OpenAPISpec,
  depth: number,
  prefix: string,
  out: SpecBodyField[],
): void {
  const resolved = resolveSchema(schema, spec)
  if (!resolved?.properties) return

  const required = resolved.required ?? []

  for (const [key, rawProp] of Object.entries(resolved.properties)) {
    const prop = resolveSchema(rawProp, spec)
    if (!prop) continue
    // Huma stamps a `$schema` (readOnly) property onto every generated request-body schema
    // for tooling/editor use; it is never a field the client is meant to send. Surfacing it
    // here would mislead an MCP client into thinking it's part of the write body.
    if (prop.readOnly === true) continue

    const fieldName = prefix ? `${prefix}.${key}` : key
    const isRequired = required.includes(key)
    const schemaType = primaryType(prop.type)

    if (schemaType === 'array') {
      const itemSchema = resolveSchema(prop.items, spec)
      const itemType = itemSchema ? primaryType(itemSchema.type) : undefined
      const isItemObject = itemType === 'object' || (!itemType && !!itemSchema?.properties)

      out.push({
        name: fieldName,
        type: isItemObject ? 'array of object' : itemType ? `array of ${itemType}` : 'array',
        ...(isRequired && { required: true }),
        ...(prop.description && { description: prop.description }),
        ...(itemSchema?.enum && { enum: itemSchema.enum.map(String) }),
      })

      if (isItemObject && depth < MAX_BODY_DEPTH) {
        flattenBodySchema(itemSchema, spec, depth + 1, `${fieldName}[]`, out)
      }
      continue
    }

    if (schemaType === 'object' || prop.properties) {
      out.push({
        name: fieldName,
        type: 'object',
        ...(isRequired && { required: true }),
        ...(prop.description && { description: prop.description }),
      })

      if (depth < MAX_BODY_DEPTH) {
        flattenBodySchema(prop, spec, depth + 1, fieldName, out)
      }
      continue
    }

    out.push({
      name: fieldName,
      ...(schemaType && { type: schemaType }),
      ...(isRequired && { required: true }),
      ...(prop.enum && { enum: prop.enum.map(String) }),
      ...(prop.description && { description: prop.description }),
    })
  }
}

/**
 * Extract and flatten an operation's `application/json` request body schema. Prefer
 * `content['application/json'].schema`; a body defined only under some other content type is
 * skipped (acceptable per the brief — Huma-generated specs use JSON bodies throughout).
 */
function extractRequestBodyFields(
  requestBody: OpenAPIRequestBody | undefined,
  spec: OpenAPISpec,
): SpecBodyField[] {
  const schema = requestBody?.content?.['application/json']?.schema
  if (!schema) return []

  const out: SpecBodyField[] = []
  flattenBodySchema(schema, spec, 1, '', out)
  return out
}

const HTTP_METHODS = ['get', 'post', 'put', 'patch', 'delete'] as const
export { HTTP_METHODS }

export async function buildSpecIndex(): Promise<SpecIndex> {
  // Parse YAML
  const rawYaml = readFileSync(OPENAPI_PATH, 'utf-8')
  const rawSpec = parseYaml(rawYaml) as OpenAPISpec

  // Dereference all $refs (inline — no remote refs in this spec)
  const spec = (await $RefParser.dereference(rawSpec as object)) as OpenAPISpec

  return indexFromSpec(spec)
}

/**
 * The flattening + classification pass, split out from buildSpecIndex so the
 * UNCLASSIFIED-OPERATION FAILURE PATH CAN BE TESTED. buildSpecIndex reads the committed
 * api/openapi.yaml, in which every operation is classified by definition, so a test that only
 * calls buildSpecIndex can never reach the throw below — it would stay green if the throw were
 * reverted to the old warn-and-drop. Tests feed a synthetic spec here instead.
 */
export function indexFromSpec(spec: OpenAPISpec): SpecIndex {
  const entries: SpecEntry[] = []
  const unclassified: string[] = []

  for (const [path, pathItem] of Object.entries(spec.paths ?? {})) {
    for (const method of HTTP_METHODS) {
      const operation = pathItem[method] as OpenAPIOperation | undefined
      if (!operation?.operationId) continue

      const { operationId, summary = '', description, tags = [], parameters: rawParams } = operation

      // Look up capability classification. An operation absent from the manifest is a
      // BUILD FAILURE, not a warning — see the throw below.
      const capability = getCapability(operationId)
      if (capability === undefined) {
        unclassified.push(`${method.toUpperCase()} ${path} → ${operationId}`)
        continue
      }

      // Extract query + path parameters with description/enum only (token-efficient)
      const parameters: SpecParameter[] = (rawParams ?? [])
        .filter((p): p is OpenAPIParameter & { name: string; in: string } =>
          typeof p.name === 'string' && (p.in === 'query' || p.in === 'path') && !!(p.description || p.schema?.enum || p.schema?.items?.enum)
        )
        .map((p) => {
          const enumValues = p.schema?.enum ?? p.schema?.items?.enum
          const schemaType = Array.isArray(p.schema?.type) ? p.schema.type.find((t) => t !== 'null') : p.schema?.type
          return {
            name: p.name,
            in: p.in,
            ...(p.required && { required: true }),
            ...(p.description && { description: p.description }),
            ...(enumValues && { enum: enumValues.map(String) }),
            ...(schemaType && { type: schemaType }),
          }
        })

      // Extract the request body schema (issue #1609 Task 6). Prefer application/json;
      // resolves $ref against components.schemas locally (spec is already in scope).
      const requestBody = extractRequestBodyFields(operation.requestBody, spec)

      const entry: SpecEntry = {
        operationId,
        method: method.toUpperCase(),
        path,
        summary,
        tags,
        manifestClass: capability.class,
        excluded: capability.excluded,
      }
      if (description) entry.description = description
      if (parameters.length > 0) entry.parameters = parameters
      if (requestBody.length > 0) entry.requestBody = requestBody

      entries.push(entry)
    }
  }

  // Issue #1503: previously an unclassified operationId was merely `console.warn`ed and
  // dropped. The drift gate (`make validate-mcp-artifacts`) compares the GENERATED index
  // against the committed one, so a dropped operation leaves the artifact byte-identical
  // and the gate exits 0 with the warning scrolling past. listAdminPlaidOrphanedItems sat
  // omitted behind exactly that green tick. Throwing makes the drift gate fail closed on
  // an unclassified operation too, instead of relying solely on
  // scripts/check-manifest-completeness.ts (which does catch it, but only runs in
  // `make test-fast` / `make test-all` / CI).
  if (unclassified.length > 0) {
    throw new Error(
      `Unclassified operations in api/openapi.yaml (add each to src/manifest/capability-manifest.ts ` +
        `as read/write/excluded — deny-by-default means all must be explicit):\n` +
        unclassified.map((o) => `   ${o}`).join('\n')
    )
  }

  const index: SpecIndex = {
    version: '1',
    entries,
  }

  return index
}

// ── CLI entrypoint ──────────────────────────────────────────────────────────

if (import.meta.main) {
  const index = await buildSpecIndex()
  writeFileSync(OUTPUT_PATH, JSON.stringify(index, null, 2) + '\n')
  const total = index.entries.length
  const excluded = index.entries.filter((e) => e.excluded).length
  const allowed = total - excluded
  console.log(`✅ spec-index.json generated: ${total} operations (${allowed} allowed, ${excluded} excluded)`)
}
