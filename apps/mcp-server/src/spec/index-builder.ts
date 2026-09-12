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
import { validateResponseCatalog } from '../responses/catalog-validation'
import { responseCatalog, responseProfiles, type ProjectionTarget, type ResponseDetail } from '../responses/projection'
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
  /** OpenAPI `format` keyword (e.g. `date`, `date-time`, `uuid`, `double`). */
  format?: string
  /**
   * A field validated by a union of alternative schemas instead of a single `type` — Huma emits
   * this for `domain.FlexibleDate` (issue #1630 Task 9): `anyOf: [{type: string, format: date},
   * {type: string, format: date-time}]`. Each branch is itself an OpenAPISchema (inline here in
   * practice, never a bare `$ref` string, but resolveSchema is applied defensively anyway).
   */
  anyOf?: OpenAPISchema[]
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

interface OpenAPIResponse {
  content?: Record<string, { schema?: OpenAPISchema } | undefined>
  description?: string
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
  /** Response objects keyed by status code (e.g. '200', '201', 'default'). */
  responses?: Record<string, OpenAPIResponse | undefined>
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

/**
 * Resolve a property's effective `type`/`format`, understanding `anyOf` (issue #1630 Task 12a).
 *
 * A plain schema reads `type`/`format` directly. An `anyOf` schema (Huma's
 * `domain.FlexibleDate`: `anyOf: [{type: string, format: date}, {type: string, format:
 * date-time}]`) has NEITHER at the top level — reading only `prop.type`/`prop.format` (the old
 * behavior) silently drops both, rendering the field with no type at all. Instead, resolve each
 * branch and merge: every distinct type found across branches, and every distinct format,
 * joined `|` so a client sees `type: "string"`, `format: "date|date-time"` rather than nothing.
 */
function resolveTypeAndFormat(
  prop: OpenAPISchema,
  spec: OpenAPISpec,
): { type?: string; format?: string } {
  if (!prop.anyOf) {
    return { type: primaryType(prop.type), format: prop.format }
  }

  const types = new Set<string>()
  const formats = new Set<string>()
  for (const branch of prop.anyOf) {
    const resolved = resolveSchema(branch, spec)
    if (!resolved) continue
    const t = primaryType(resolved.type)
    if (t) types.add(t)
    if (resolved.format) formats.add(resolved.format)
  }

  return {
    type: types.size ? [...types].join('|') : undefined,
    format: formats.size ? [...formats].join('|') : undefined,
  }
}

// Nested objects / array-of-object items flatten to this many levels deep (dotted names).
// Depth 1 = the request body's own top-level fields; depth 2 = one level of nesting
// (`action.custom_category_id`, `conditions[].field`). Beyond that we mark the field as
// `object` / `array of object` without expanding it further — see the brief: "flatten... to
// a reasonable depth (2 is enough)... or clearly mark a nested object rather than recursing
// without bound."
const MAX_BODY_DEPTH = 2

// Response fields remain shallow except for explicit catalog child targets (#1634).
// This keeps conditions/actions and other arbitrary JSON opaque while expanding entity
// collections and nested identity fields. Request depth is independent and stays two.
const MAX_RESPONSE_DEPTH = 1

/**
 * Per-property skip policy for flattenBodySchema — request and response bodies need OPPOSITE
 * answers for `readOnly`, so this cannot be a single hardcoded rule (see the two exported
 * extract functions below for how each side sets this).
 *
 *  - Request bodies: skip every `readOnly` property. It is server-set and "not yours to send" —
 *    Huma stamps a `readOnly: true` `$schema` property onto every generated request-body schema
 *    for tooling/editor use, and surfacing it would mislead a client into thinking it's part of
 *    the write body.
 *  - Response bodies: do NOT skip `readOnly` — a `readOnly` field is server-set but definitely
 *    RETURNED, so hiding it would hide real response shape. Only `$schema` itself is skipped, by
 *    name, because it is the one property that is never meaningful API output either way. This
 *    only looks equivalent to "skip readOnly" today because `$schema` happens to be the sole
 *    `readOnly` property in this spec — that coincidence must not be load-bearing.
 */
interface FlattenPolicy {
  maxDepth: number
  skipProperty: (key: string, prop: OpenAPISchema) => boolean
}

const REQUEST_BODY_POLICY: FlattenPolicy = {
  maxDepth: MAX_BODY_DEPTH,
  skipProperty: (_key, prop) => prop.readOnly === true,
}

const RESPONSE_BODY_POLICY: FlattenPolicy = {
  maxDepth: MAX_RESPONSE_DEPTH,
  skipProperty: (key) => key === '$schema',
}

/**
 * Flatten an object schema's properties into token-frugal SpecBodyField entries, recursing
 * into nested objects and array-of-object items up to `policy.maxDepth` using dotted names.
 * `policy` also controls which properties are skipped — see FlattenPolicy above; request and
 * response bodies need different (in fact opposite) answers for `readOnly`.
 */
function flattenBodySchema(
  schema: OpenAPISchema | undefined,
  spec: OpenAPISpec,
  depth: number,
  prefix: string,
  out: SpecBodyField[],
  policy: FlattenPolicy,
): void {
  const resolved = resolveSchema(schema, spec)
  if (!resolved?.properties) return

  const required = resolved.required ?? []

  for (const [key, rawProp] of Object.entries(resolved.properties)) {
    const prop = resolveSchema(rawProp, spec)
    if (!prop) continue
    if (policy.skipProperty(key, prop)) continue

    const fieldName = prefix ? `${prefix}.${key}` : key
    const isRequired = required.includes(key)
    const { type: schemaType, format: schemaFormat } = resolveTypeAndFormat(prop, spec)

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

      if (isItemObject && depth < policy.maxDepth) {
        flattenBodySchema(itemSchema, spec, depth + 1, `${fieldName}[]`, out, policy)
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

      if (depth < policy.maxDepth) {
        flattenBodySchema(prop, spec, depth + 1, fieldName, out, policy)
      }
      continue
    }

    out.push({
      name: fieldName,
      ...(schemaType && { type: schemaType }),
      ...(schemaFormat && { format: schemaFormat }),
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
  flattenBodySchema(schema, spec, 1, '', out, REQUEST_BODY_POLICY)
  return out
}

/**
 * Find the operation's JSON success response schema — prefer '200', then '201' (the only two
 * status codes this spec ever documents a JSON body for on success). Never falls through to
 * 'default' (the error schema).
 */
function findJsonSuccessResponseSchema(
  responses: Record<string, OpenAPIResponse | undefined> | undefined,
): OpenAPISchema | undefined {
  if (!responses) return undefined
  for (const code of ['200', '201']) {
    const schema = responses[code]?.content?.['application/json']?.schema
    if (schema) return schema
  }
  return undefined
}

/**
 * Extract and flatten an operation's JSON success-response body schema (issue #1610a). Unlike
 * request bodies, `readOnly` properties are KEPT (see RESPONSE_BODY_POLICY) and only the
 * `$schema` tooling property is dropped by name.
 *
 * A response whose root schema is itself an array (many list endpoints return a bare array
 * rather than `{ items: [...] }`) flattens the ITEM schema's fields directly, with no prefix —
 * there is no meaningful top-level field name for "the array itself" to hang a dotted path off.
 */
function extractResponseBodyFields(
  responses: Record<string, OpenAPIResponse | undefined> | undefined,
  spec: OpenAPISpec,
): SpecBodyField[] {
  const schema = findJsonSuccessResponseSchema(responses)
  if (!schema) return []

  const resolved = resolveSchema(schema, spec)
  if (!resolved) return []

  const out: SpecBodyField[] = []
  if (primaryType(resolved.type) === 'array') {
    flattenBodySchema(resolved.items, spec, 1, '', out, RESPONSE_BODY_POLICY)
  } else {
    flattenBodySchema(schema, spec, 1, '', out, RESPONSE_BODY_POLICY)
  }
  return out
}

/** Expand only explicitly profiled objects; all other JSON remains opaque. */
function extractProjectedFields(schema: OpenAPISchema | undefined, spec: OpenAPISpec, target: ProjectionTarget, detail: ResponseDetail, prefix = ''): SpecBodyField[] {
  const resolved = resolveSchema(schema, spec)
  const object = target.array ? resolveSchema(resolved?.items, spec) : resolved
  const properties = object?.properties
  if (!properties) return []
  const profile = responseProfiles[target.profile]!
  const fields: SpecBodyField[] = []
  flattenBodySchema(object, spec, 1, prefix, fields, RESPONSE_BODY_POLICY)
  const result = fields.flatMap((field) => {
    const key = prefix ? field.name.slice(prefix.length + 1) : field.name
    if (detail === 'compact' && profile.omit.includes(key)) return []
    const child = profile.children[key]
    if (!child) return [field]
    const childPrefix = field.name + (child.array ? '[]' : '')
    return [field, ...extractProjectedFields(properties[key], spec, child, detail, childPrefix)]
  })
  const name = (key: string) => prefix ? `${prefix}.${key}` : key
  if (detail === 'compact' && profile.derived) {
    for (const level of ['primary', 'detailed']) {
      result.push({ name: name(`effective_pfc_${level}_display`), type: 'string|null', description: `Present only when category_override_kind is not custom and the source display exists (including null). Uses pfc_${level}_override_display for a pfc override, otherwise pfc_${level}_display.` })
    }
    if (profile.derived === 'transaction') {
      result.push({ name: name('reversed'), type: 'boolean', description: 'True when deleted_at is non-null and deleted_reason is provider_removed_posted; false otherwise.' })
      result.push({ name: name('reversed_at'), type: 'string', format: 'date-time', description: 'Present only when reversed is true; the deleted_at timestamp.' })
    }
  }
  return result
}

const HTTP_METHODS = ['get', 'post', 'put', 'patch', 'delete'] as const
export { HTTP_METHODS }

export async function buildSpecIndex(sourceSpec?: OpenAPISpec): Promise<SpecIndex> {
  // Parse YAML
  const rawSpec = sourceSpec ?? parseYaml(readFileSync(OPENAPI_PATH, 'utf-8')) as OpenAPISpec

  // Dereference all $refs (inline — no remote refs in this spec)
  const spec = (await $RefParser.dereference(rawSpec as object)) as OpenAPISpec

  const index = indexFromSpec(spec)
  const errors = validateResponseCatalog(spec, index.entries.filter((entry) => !entry.excluded).map((entry) => entry.operationId))
  if (errors.length) throw new Error(`Invalid MCP response catalog:\n${errors.join('\n')}`)
  return index
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

      // Extract the response body schema — allowed operations ONLY. An
      // excluded operation can never be executed through MCP, so a response schema for one
      // is dead weight in the index (measured: −68 KB of the +56% budget row). Computing it
      // unconditionally would silently regress the size budget the moment a new operation is
      // excluded.
      const policy = responseCatalog[operationId]
      const responseSchema = findJsonSuccessResponseSchema(operation.responses)
      let responseBody: SpecBodyField[] = []
      let compactResponseBody: SpecBodyField[] = []
      if (!capability.excluded) {
        if (policy?.kind === 'project') {
          responseBody = extractProjectedFields(responseSchema, spec, policy.target, 'full')
          compactResponseBody = extractProjectedFields(responseSchema, spec, policy.target, 'compact')
        } else {
          responseBody = extractResponseBodyFields(operation.responses, spec)
        }
      }

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
      if (responseBody.length > 0) entry.responseBody = responseBody
      if (compactResponseBody.length > 0) {
        const fullNames = new Set(responseBody.map((field) => field.name))
        entry.compactResponseBody = compactResponseBody.map((field) => fullNames.has(field.name) ? field.name : field)
      }

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
