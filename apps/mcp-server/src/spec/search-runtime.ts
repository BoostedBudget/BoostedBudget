/**
 * Spec search runtime — runs inside the sandbox isolate.
 * Read-only, operates on the pre-built SpecIndex loaded at Worker startup.
 */

import type { OperationClass } from '../manifest/capability-manifest'
import type { ResponseDetail } from '../responses/projection'

/** A single parameter extracted from the OpenAPI spec for discoverability. */
export interface SpecParameter {
  name: string
  in: string
  required?: boolean
  description?: string
  enum?: string[]
  type?: string
}

/**
 * A single field extracted from an operation's request body schema (issue #1609 Task 6).
 * Flattened from JSON Schema (with `$ref` resolved) to a token-frugal shape: name, type,
 * required flag, enum, description — never the full schema. Nested objects/array-of-object
 * fields are flattened to a shallow depth using dotted names (`action.custom_category_id`)
 * or bracket-dotted array item names (`conditions[].field`).
 */
export interface SpecBodyField {
  name: string
  type?: string
  /**
   * OpenAPI `format` (e.g. `date`, `date-time`, `uuid`) — issue #1630. A field validated by an
   * `anyOf` of same-typed branches with different formats (e.g. `domain.FlexibleDate`'s
   * `anyOf: [{format: date}, {format: date-time}]`) carries every distinct branch format,
   * joined `|` (`"date|date-time"`) — see flattenBodySchema's anyOf handling in index-builder.ts.
   */
  format?: string
  required?: boolean
  enum?: string[]
  description?: string
}

/** Explicit JSON response-root container, preserving nullability at every array level. */
export type SpecResponseShape =
  | {
      type: 'array'
      items: SpecResponseShape
      nullable?: true
    }
  | {
      type: 'object' | 'string' | 'number' | 'integer' | 'boolean' | 'null'
      nullable?: true
    }

export interface SpecEntry {
  operationId: string
  method: string
  path: string
  summary: string
  /** Full OpenAPI description — documents filters, enums, pagination details. */
  description?: string
  tags: string[]
  /** Class from the capability manifest */
  manifestClass: OperationClass
  excluded: boolean
  /** Query and path parameters extracted from the OpenAPI spec for AI discoverability. */
  parameters?: SpecParameter[]
  /** Request body fields extracted from the OpenAPI spec, flattened and token-frugal. */
  requestBody?: SpecBodyField[]
  /**
   * Response body fields extracted from the operation's JSON 200/201 success response
   * as full API fields, shallow except for explicitly profiled child targets, allowed-operations-only —
   * see index-builder.ts. Deliberately NOT included in searchSpec's haystack (search-runtime.ts):
   * matching on response field names would make almost every query match almost everything.
   */
  responseBody?: SpecBodyField[]
  /** Recursive JSON response-root shape for an eligible 200/201 success response. */
  responseShape?: SpecResponseShape
  /** Compact projection fields, including conditional derived values. Absent for passthrough operations. */
  compactResponseBody?: (string | SpecBodyField)[]
}

/** Compact metadata references full fields by name to avoid duplicating descriptions. */
export function responseFields(entry: SpecEntry, detail: ResponseDetail): SpecBodyField[] | undefined {
  if (detail === 'full' || !entry.compactResponseBody) return entry.responseBody
  const full = new Map(entry.responseBody?.map((field) => [field.name, field]))
  return entry.compactResponseBody.flatMap((field) => {
    if (typeof field !== 'string') return [field]
    const descriptor = full.get(field)
    return descriptor ? [descriptor] : []
  })
}

export interface SpecIndex {
  version: string
  entries: SpecEntry[]
}

export interface SearchOptions {
  /** Only return entries with this class */
  class?: OperationClass
  /** Include excluded entries in results */
  includeExcluded?: boolean
  /** Max results to return (default: 20) */
  limit?: number
}

/**
 * Search the spec index by keyword.
 * Matches against operationId, summary, path, and tags (case-insensitive).
 * Excluded entries are omitted unless includeExcluded is set.
 */
export function searchSpec(
  index: SpecIndex,
  query: string,
  opts: SearchOptions = {},
): SpecEntry[] {
  const { class: classFilter, includeExcluded = false, limit = 20 } = opts
  const q = query.toLowerCase().replace(/[-_]/g, '')

  return index.entries
    .filter((entry) => {
      if (!includeExcluded && entry.excluded) return false
      if (classFilter && entry.manifestClass !== classFilter) return false
      if (!q) return true

      const haystack = [
        entry.operationId,
        entry.summary,
        entry.description ?? '',
        entry.path,
        ...entry.tags,
        ...(entry.parameters?.map((p) => `${p.name} ${p.description ?? ''} ${(p.enum ?? []).join(' ')}`) ?? []),
        // Body field names (not descriptions — kept minimal for index size) so "how do I
        // create a rule with conditions" matches createRule even when "conditions" never
        // appears in its summary/description. See issue #1609 Task 6.
        ...(entry.requestBody?.map((f) => f.name) ?? []),
      ]
        .join(' ')
        .toLowerCase()
        .replace(/[-_]/g, '')

      return haystack.includes(q)
    })
    .slice(0, limit)
}

/**
 * Look up a single spec entry by exact operationId.
 */
export function getSpecEntry(index: SpecIndex, operationId: string): SpecEntry | undefined {
  return index.entries.find((e) => e.operationId === operationId)
}

export interface ListOptions {
  tag?: string
  class?: OperationClass
  includeExcluded?: boolean
}

/**
 * List spec entries, optionally filtered by tag and/or class.
 * Excluded entries are omitted unless includeExcluded is set.
 */
export function listSpecEntries(index: SpecIndex, opts: ListOptions = {}): SpecEntry[] {
  const { tag, class: classFilter, includeExcluded = false } = opts

  return index.entries.filter((entry) => {
    if (!includeExcluded && entry.excluded) return false
    if (classFilter && entry.manifestClass !== classFilter) return false
    if (tag && !entry.tags.includes(tag)) return false
    return true
  })
}
