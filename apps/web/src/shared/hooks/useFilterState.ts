/**
 * useFilterState — Generic URL-state filter hook
 * Issue #1129: Generic filter state management for deduplication of per-feature filter hooks
 *
 * Manages filter values in URLSearchParams (react-router-dom).
 * Supports multi-select, range, and scalar field types with full codec support.
 *
 * @example
 * const SPEC = {
 *   institution: { kind: 'multi-select' },
 *   balance: { kind: 'range' },
 *   search: { kind: 'scalar', codec: 'string' as const },
 * } as const satisfies FilterSpec
 * const { filters } = useFilterState(SPEC)
 * // filters.institution: string[]
 * // filters.balance: { min?: number; max?: number } | undefined
 * // filters.search: string | undefined
 *
 * NOTE: Use `as const satisfies FilterSpec` on spec literals (not `: FilterSpec`) to
 * preserve literal types so InferFilters<S> resolves correctly per field.
 */

import { useCallback, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export type ScalarCodec =
  | 'string'
  | 'number'
  | 'integer'
  | 'boolean'
  | { parse: (raw: string) => unknown; serialize: (v: unknown) => string | null }

export type FilterFieldSpec =
  | {
      kind: 'scalar'
      urlKey?: string
      codec: ScalarCodec
      defaultValue?: unknown
      activeWhen?: (v: unknown) => boolean
    }
  | {
      kind: 'multi-select'
      urlKey?: string
      serialize?: 'csv' | 'repeat'
    }
  | {
      kind: 'range'
      urlKeys?: { min: string; max: string }
      codec?: 'float' | 'integer'
    }

export type FilterSpec = Record<string, FilterFieldSpec>

// ---------------------------------------------------------------------------
// InferFilters<S> — maps spec keys to their resolved runtime types
// ---------------------------------------------------------------------------

type InferScalarType<C extends ScalarCodec> = C extends 'string'
  ? string
  : C extends 'number'
    ? number
    : C extends 'integer'
      ? number
      : C extends 'boolean'
        ? boolean
        : unknown

type InferFieldValue<F extends FilterFieldSpec> = F extends { kind: 'multi-select' }
  ? string[]
  : F extends { kind: 'range' }
    ? { min?: number; max?: number } | undefined
    : F extends { kind: 'scalar'; codec: infer C extends ScalarCodec }
      ? InferScalarType<C> | undefined
      : never

export type InferFilters<S extends FilterSpec> = {
  [K in keyof S]: InferFieldValue<S[K]>
}

export interface UseFilterStateOptions {
  clearKeysOnWrite?: string[]
}

export interface UseFilterStateReturn<S extends FilterSpec> {
  filters: InferFilters<S>
  setFilter: <K extends keyof S & string>(key: K, value: InferFilters<S>[K]) => void
  setFilters: (updates: Partial<InferFilters<S>>) => void
  clearFilter: (key: keyof S & string) => void
  clearAllFilters: () => void
  activeFilterCount: number
  hasActiveFilters: boolean
}

// ---------------------------------------------------------------------------
// Codec helpers — pure functions for tree-shaking
// ---------------------------------------------------------------------------

function parseScalar(raw: string | null, codec: ScalarCodec): unknown {
  if (raw === null) return undefined
  if (codec === 'string') return raw
  if (codec === 'number') {
    const v = parseFloat(raw)
    return isNaN(v) ? undefined : v
  }
  if (codec === 'integer') {
    const v = parseInt(raw, 10)
    return isNaN(v) ? undefined : v
  }
  if (codec === 'boolean') {
    if (raw === 'true') return true
    if (raw === 'false') return false
    return undefined
  }
  // custom codec
  return codec.parse(raw)
}

function serializeScalar(value: unknown, codec: ScalarCodec): string | null {
  if (value === undefined || value === null) return null
  if (codec === 'string') return String(value)
  if (codec === 'number' || codec === 'integer') return String(value)
  if (codec === 'boolean') return value ? 'true' : 'false'
  // custom codec
  return codec.serialize(value)
}

// ---------------------------------------------------------------------------
// URL-key derivation helpers
// ---------------------------------------------------------------------------

function getRangeUrlKeys(
  specKey: string,
  fieldSpec: { kind: 'range'; urlKeys?: { min: string; max: string } },
): { min: string; max: string } {
  return fieldSpec.urlKeys ?? { min: `${specKey}_min`, max: `${specKey}_max` }
}

// ---------------------------------------------------------------------------
// Field-level read/write helpers
// ---------------------------------------------------------------------------

function parseFloat_(raw: string | null): number | undefined {
  if (raw === null) return undefined
  const v = parseFloat(raw)
  return isNaN(v) ? undefined : v
}

function parseInt_(raw: string | null): number | undefined {
  if (raw === null) return undefined
  const v = parseInt(raw, 10)
  return isNaN(v) ? undefined : v
}

function readField(
  specKey: string,
  fieldSpec: FilterFieldSpec,
  searchParams: URLSearchParams,
): unknown {
  if (fieldSpec.kind === 'multi-select') {
    const urlKey = fieldSpec.urlKey ?? specKey
    if (fieldSpec.serialize === 'repeat') {
      return searchParams.getAll(urlKey)
    }
    const raw = searchParams.get(urlKey)
    if (!raw) return []
    return raw
      .split(',')
      .filter(Boolean)
      .map((s) => s.trim())
  }

  if (fieldSpec.kind === 'range') {
    const { min: minKey, max: maxKey } = getRangeUrlKeys(specKey, fieldSpec)
    const codec = fieldSpec.codec ?? 'float'
    const minRaw = searchParams.get(minKey)
    const maxRaw = searchParams.get(maxKey)
    const parseFn = codec === 'integer' ? parseInt_ : parseFloat_
    const minVal = parseFn(minRaw)
    const maxVal = parseFn(maxRaw)
    if (minVal === undefined && maxVal === undefined) return undefined
    return { min: minVal, max: maxVal }
  }

  // scalar
  const urlKey = fieldSpec.urlKey ?? specKey
  const raw = searchParams.get(urlKey)
  return parseScalar(raw, fieldSpec.codec)
}

function isFieldActive(fieldSpec: FilterFieldSpec, value: unknown): boolean {
  if (fieldSpec.kind === 'multi-select') {
    return Array.isArray(value) && value.length > 0
  }

  if (fieldSpec.kind === 'range') {
    if (value === undefined || value === null) return false
    const range = value as { min?: number; max?: number }
    return range.min !== undefined || range.max !== undefined
  }

  // scalar
  if (fieldSpec.activeWhen) {
    return fieldSpec.activeWhen(value)
  }
  if (value === undefined) return false
  if (fieldSpec.defaultValue !== undefined && value === fieldSpec.defaultValue) return false
  return true
}

function writeField(
  specKey: string,
  fieldSpec: FilterFieldSpec,
  value: unknown,
  params: URLSearchParams,
): void {
  if (fieldSpec.kind === 'multi-select') {
    const urlKey = fieldSpec.urlKey ?? specKey
    const arr = (value as string[] | undefined) ?? []
    if (arr.length === 0) {
      params.delete(urlKey)
    } else if (fieldSpec.serialize === 'repeat') {
      params.delete(urlKey)
      for (const item of arr) {
        params.append(urlKey, item)
      }
    } else {
      params.set(urlKey, arr.join(','))
    }
    return
  }

  if (fieldSpec.kind === 'range') {
    const { min: minKey, max: maxKey } = getRangeUrlKeys(specKey, fieldSpec)
    if (value === undefined || value === null) {
      params.delete(minKey)
      params.delete(maxKey)
      return
    }
    const range = value as { min?: number; max?: number }
    if (range.min !== undefined) params.set(minKey, String(range.min))
    else params.delete(minKey)
    if (range.max !== undefined) params.set(maxKey, String(range.max))
    else params.delete(maxKey)
    return
  }

  // scalar
  const urlKey = fieldSpec.urlKey ?? specKey
  const serialized = serializeScalar(value, fieldSpec.codec)
  if (serialized === null) {
    params.delete(urlKey)
  } else {
    params.set(urlKey, serialized)
  }
}

function deleteField(
  specKey: string,
  fieldSpec: FilterFieldSpec,
  params: URLSearchParams,
): void {
  if (fieldSpec.kind === 'multi-select') {
    params.delete(fieldSpec.urlKey ?? specKey)
    return
  }
  if (fieldSpec.kind === 'range') {
    const { min: minKey, max: maxKey } = getRangeUrlKeys(specKey, fieldSpec)
    params.delete(minKey)
    params.delete(maxKey)
    return
  }
  params.delete(fieldSpec.urlKey ?? specKey)
}

// ---------------------------------------------------------------------------
// Main hook
// ---------------------------------------------------------------------------

export function useFilterState<S extends FilterSpec>(
  spec: S,
  options?: UseFilterStateOptions,
): UseFilterStateReturn<S> {
  const [searchParams, setSearchParams] = useSearchParams()

  const clearKeysKey = options?.clearKeysOnWrite?.join(',') ?? ''

  const applyClearKeys = useCallback(
    (params: URLSearchParams) => {
      if (clearKeysKey) {
        for (const k of clearKeysKey.split(',')) {
          params.delete(k)
        }
      }
    },
    [clearKeysKey],
  )

  // Parse all fields from current URL
  const filters = useMemo<InferFilters<S>>(() => {
    const result: Record<string, unknown> = {}
    for (const [key, fieldSpec] of Object.entries(spec)) {
      result[key] = readField(key, fieldSpec, searchParams)
    }
    return result as InferFilters<S>
  }, [spec, searchParams])

  const activeFilterCount = useMemo(() => {
    let count = 0
    for (const [key, fieldSpec] of Object.entries(spec)) {
      if (isFieldActive(fieldSpec, filters[key])) count++
    }
    return count
  }, [spec, filters])

  const setFilter = useCallback(
    <K extends keyof S & string>(key: K, value: InferFilters<S>[K]) => {
      const fieldSpec = spec[key]
      if (!fieldSpec) return
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          writeField(key, fieldSpec, value, next)
          applyClearKeys(next)
          return next
        },
        { replace: true },
      )
    },
    [spec, setSearchParams, applyClearKeys],
  )

  const setFilters = useCallback(
    (updates: Partial<InferFilters<S>>) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          for (const [key, value] of Object.entries(updates)) {
            const fieldSpec = spec[key]
            if (!fieldSpec) continue
            writeField(key, fieldSpec, value, next)
          }
          applyClearKeys(next)
          return next
        },
        { replace: true },
      )
    },
    [spec, setSearchParams, applyClearKeys],
  )

  const clearFilter = useCallback(
    (key: string) => {
      const fieldSpec = spec[key]
      if (!fieldSpec) return
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          deleteField(key, fieldSpec, next)
          applyClearKeys(next)
          return next
        },
        { replace: true },
      )
    },
    [spec, setSearchParams, applyClearKeys],
  )

  const clearAllFilters = useCallback(() => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        for (const [key, fieldSpec] of Object.entries(spec)) {
          deleteField(key, fieldSpec, next)
        }
        applyClearKeys(next)
        return next
      },
      { replace: true },
    )
  }, [spec, setSearchParams, applyClearKeys])

  return {
    filters,
    setFilter,
    setFilters,
    clearFilter,
    clearAllFilters,
    activeFilterCount,
    hasActiveFilters: activeFilterCount > 0,
  }
}
