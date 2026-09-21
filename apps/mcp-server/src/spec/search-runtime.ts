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

/** Query terms that carry no domain signal and would match almost every operation (#1699). */
const STOPWORDS = new Set([
  'a', 'an', 'the', 'and', 'or', 'for', 'with', 'from', 'to', 'of', 'in', 'on', 'at', 'by', 'as',
  'is', 'are', 'was', 'were', 'be', 'do', 'does', 'did', 'me', 'my', 'our', 'your', 'how', 'much',
  'many', 'what', 'which', 'when', 'last', 'next', 'show', 'list', 'get', 'find', 'all', 'any',
  'some', 'it', 'its', 'this', 'that', 'over', 'under',
])

interface ScoredEntry {
  entry: SpecEntry
  originalIndex: number
  allTermsMatch: boolean
  score: number
}

/** Lowercase and turn `-`/`_` into spaces so a hyphenated query term can bridge camelCase ids. */
function normalizeForSearch(value: string): string {
  return value.toLowerCase().replace(/[-_]/g, ' ')
}

function tokenizeQuery(query: string): string[] {
  const seen = new Set<string>()
  const terms: string[] = []
  for (const raw of normalizeForSearch(query).split(/\s+/)) {
    if (raw.length < 2 || STOPWORDS.has(raw) || seen.has(raw)) continue
    seen.add(raw)
    terms.push(raw)
  }
  return terms
}

/**
 * Weighted searchable text for one entry. Field weights drive ranking: the operationId is the
 * strongest signal, then summary, then path; descriptions, tags, parameters and body field names
 * share the lowest weight. Param descriptions and enum values stay in the haystack (#1609/#1630).
 */
function haystackFields(entry: SpecEntry): Array<{ weight: number; text: string }> {
  return [
    { weight: 4, text: normalizeForSearch(entry.operationId) },
    { weight: 3, text: normalizeForSearch(entry.summary) },
    { weight: 2, text: normalizeForSearch(entry.path) },
    { weight: 1, text: normalizeForSearch(entry.description ?? '') },
    ...entry.tags.map((tag) => ({ weight: 1, text: normalizeForSearch(tag) })),
    ...(entry.parameters?.map((p) => ({
      weight: 1,
      text: normalizeForSearch(`${p.name} ${p.description ?? ''} ${(p.enum ?? []).join(' ')}`),
    })) ?? []),
    ...(entry.requestBody?.map((f) => ({ weight: 1, text: normalizeForSearch(f.name) })) ?? []),
  ]
}

/**
 * Search the spec index by keyword.
 *
 * Tokenized, ranked matching (#1699): the query is split into terms (lowercased, separators
 * normalized to spaces, sub-2-char and stopword terms dropped, duplicates collapsed), then every
 * eligible entry is scored by the highest-weight field each term appears in (operationId 4,
 * summary 3, path 2, any other field 1). Entries matching ALL terms rank first, then by score,
 * then by original index; entries matching at least one term are still surfaced. Excluded entries
 * are omitted unless includeExcluded is set, and the class filter is always preserved.
 */
export function searchSpec(
  index: SpecIndex,
  query: string,
  opts: SearchOptions = {},
): SpecEntry[] {
  const { class: classFilter, includeExcluded = false, limit = 20 } = opts
  const terms = tokenizeQuery(query)

  const scored: ScoredEntry[] = []

  index.entries.forEach((entry, originalIndex) => {
    if (!includeExcluded && entry.excluded) return
    if (classFilter && entry.manifestClass !== classFilter) return

    if (terms.length === 0) {
      scored.push({ entry, originalIndex, allTermsMatch: true, score: 0 })
      return
    }

    const fields = haystackFields(entry)
    let score = 0
    let allTermsMatch = true
    for (const term of terms) {
      let maxWeight = 0
      for (const field of fields) {
        if (field.weight > maxWeight && field.text.includes(term)) maxWeight = field.weight
      }
      if (maxWeight === 0) allTermsMatch = false
      score += maxWeight
    }

    if (allTermsMatch || score > 0) {
      scored.push({ entry, originalIndex, allTermsMatch, score })
    }
  })

  scored.sort((a, b) => {
    if (a.allTermsMatch !== b.allTermsMatch) return a.allTermsMatch ? -1 : 1
    if (a.score !== b.score) return b.score - a.score
    return a.originalIndex - b.originalIndex
  })

  return scored.slice(0, limit).map((item) => item.entry)
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
