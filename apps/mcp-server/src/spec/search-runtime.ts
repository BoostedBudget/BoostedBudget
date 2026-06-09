/**
 * Spec search runtime — runs inside the sandbox isolate.
 * Read-only, operates on the pre-built SpecIndex loaded at Worker startup.
 */

import type { OperationClass } from '../manifest/capability-manifest'

/** A single parameter extracted from the OpenAPI spec for discoverability. */
export interface SpecParameter {
  name: string
  in: string
  required?: boolean
  description?: string
  enum?: string[]
  type?: string
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
