import { useState, useCallback } from 'react'
import { useUser } from '@clerk/react-router'

/**
 * Persistence versioning:
 *
 * `schemaVersion` = knownColumnIds.join(',')
 *   — resets ALL slots (sizing + order + visibility) when the column-id schema changes.
 *
 * `sizingVersion` = SIZING_VERSION constant below
 *   — resets ONLY the sizing slot; preserves user-customized order and visibility.
 *   — Bump SIZING_VERSION whenever defaultSize / minSize / maxSize values change across
 *     a release so stale pixel widths don't persist across deploys.
 *     Convention: use a short semantic tag, e.g. 'v2-fixed-layout', 'v3-new-defaults'.
 *
 * Migration: if the stored object has a `version` field but no `schemaVersion`, treat
 * it as the old shape — `version` maps to `schemaVersion`, and `sizingVersion` is missing
 * (triggers a one-time sizing reset only; preserves order and visibility).
 */
const SIZING_VERSION = 'v2-fixed-layout'

interface TablePrefs {
  schemaVersion: string
  sizingVersion: string
  sizing: Record<string, number>
  columnOrder: string[]
  columnVisibility: Record<string, boolean>
}

// Old shape from before the schemaVersion/sizingVersion split
interface LegacyTablePrefs {
  version: string
  sizing: Record<string, number>
  columnOrder: string[]
  columnVisibility: Record<string, boolean>
}

export interface UseTablePreferencesResult {
  sizing: Record<string, number>
  onSizingChange: (sizing: Record<string, number>) => void
  columnOrder: string[]
  onColumnOrderChange: (order: string[]) => void
  columnVisibility: Record<string, boolean>
  onColumnVisibilityChange: (visibility: Record<string, boolean>) => void
}

const DEFAULT_PREFS: Pick<TablePrefs, 'sizing' | 'columnOrder' | 'columnVisibility'> = {
  sizing: {},
  columnOrder: [],
  columnVisibility: {},
}

function storageKey(userId: string, tableId: string): string {
  return `bb:table-prefs:${userId}:${tableId}`
}

function filterByKnownIds<T>(
  obj: Record<string, T>,
  knownIds: Set<string>
): Record<string, T> {
  return Object.fromEntries(
    Object.entries(obj).filter(([id]) => knownIds.has(id))
  )
}

function loadPrefs(
  userId: string,
  tableId: string,
  knownColumnIds: string[]
): Pick<TablePrefs, 'sizing' | 'columnOrder' | 'columnVisibility'> {
  const key = storageKey(userId, tableId)
  const expectedSchema = knownColumnIds.join(',')
  const knownSet = new Set(knownColumnIds)

  let raw: string | null
  try {
    raw = localStorage.getItem(key)
  } catch {
    return DEFAULT_PREFS
  }

  if (raw === null) return DEFAULT_PREFS

  let parsed: TablePrefs | LegacyTablePrefs
  try {
    parsed = JSON.parse(raw) as TablePrefs | LegacyTablePrefs
  } catch {
    return DEFAULT_PREFS
  }

  // Detect old shape (has `version`, no `schemaVersion`) and migrate.
  // Map `version` → `schemaVersion`; treat sizingVersion as missing.
  let isLegacy = false
  if ('version' in parsed && !('schemaVersion' in parsed)) {
    isLegacy = true
    parsed = {
      schemaVersion: (parsed as LegacyTablePrefs).version,
      sizingVersion: '',
      sizing: (parsed as LegacyTablePrefs).sizing ?? {},
      columnOrder: (parsed as LegacyTablePrefs).columnOrder ?? [],
      columnVisibility: (parsed as LegacyTablePrefs).columnVisibility ?? {},
    } as TablePrefs
  }

  const p = parsed as TablePrefs

  // Schema version mismatch (column ids changed) → reset everything
  if (p.schemaVersion !== expectedSchema) {
    const reset: TablePrefs = {
      schemaVersion: expectedSchema,
      sizingVersion: SIZING_VERSION,
      ...DEFAULT_PREFS,
    }
    try { localStorage.setItem(key, JSON.stringify(reset)) } catch { /* ignore */ }
    return DEFAULT_PREFS
  }

  // Sizing version mismatch (defaults changed) → reset only sizing
  const sizingOk = !isLegacy && p.sizingVersion === SIZING_VERSION
  const sizing = sizingOk
    ? filterByKnownIds(p.sizing ?? {}, knownSet)
    : {}

  if (!sizingOk) {
    // Persist the corrected prefs (reset sizing, keep order + visibility)
    const fixed: TablePrefs = {
      schemaVersion: expectedSchema,
      sizingVersion: SIZING_VERSION,
      sizing: {},
      columnOrder: (p.columnOrder ?? []).filter((id) => knownSet.has(id)),
      columnVisibility: filterByKnownIds(p.columnVisibility ?? {}, knownSet),
    }
    try { localStorage.setItem(key, JSON.stringify(fixed)) } catch { /* ignore */ }
  }

  return {
    sizing,
    columnOrder: (p.columnOrder ?? []).filter((id) => knownSet.has(id)),
    columnVisibility: filterByKnownIds(p.columnVisibility ?? {}, knownSet),
  }
}

function savePrefs(
  userId: string,
  tableId: string,
  knownColumnIds: string[],
  prefs: Pick<TablePrefs, 'sizing' | 'columnOrder' | 'columnVisibility'>
): void {
  const key = storageKey(userId, tableId)
  try {
    localStorage.setItem(
      key,
      JSON.stringify({
        schemaVersion: knownColumnIds.join(','),
        sizingVersion: SIZING_VERSION,
        ...prefs,
      })
    )
  } catch {
    // ignore storage quota errors
  }
}

/**
 * Persists column sizing, ordering, and visibility to localStorage,
 * namespaced by Clerk user ID + table ID.
 *
 * When the user is not authenticated, returns defaults and all setters are no-ops.
 * Unknown column IDs (not in knownColumnIds) are filtered out on read.
 * If the stored schemaVersion (knownColumnIds.join(',')) doesn't match, all prefs reset.
 * If only sizingVersion mismatches, only sizing resets (order + visibility preserved).
 */
export function useTablePreferences(
  tableId: string,
  knownColumnIds: string[]
): UseTablePreferencesResult {
  const { user } = useUser()
  const userId = user?.id ?? null

  const [prefs, setPrefs] = useState<Pick<TablePrefs, 'sizing' | 'columnOrder' | 'columnVisibility'>>(() =>
    userId ? loadPrefs(userId, tableId, knownColumnIds) : DEFAULT_PREFS
  )

  const [lastLoadedUserId, setLastLoadedUserId] = useState(userId)
  if (userId !== lastLoadedUserId) {
    setLastLoadedUserId(userId)
    setPrefs(userId ? loadPrefs(userId, tableId, knownColumnIds) : DEFAULT_PREFS)
  }

  const onSizingChange = useCallback(
    (newSizing: Record<string, number>) => {
      if (!userId) return
      setPrefs((prev) => {
        const next = { ...prev, sizing: newSizing }
        savePrefs(userId, tableId, knownColumnIds, next)
        return next
      })
    },
    [userId, tableId, knownColumnIds]
  )

  const onColumnOrderChange = useCallback(
    (newOrder: string[]) => {
      if (!userId) return
      setPrefs((prev) => {
        const next = { ...prev, columnOrder: newOrder }
        savePrefs(userId, tableId, knownColumnIds, next)
        return next
      })
    },
    [userId, tableId, knownColumnIds]
  )

  const onColumnVisibilityChange = useCallback(
    (newVisibility: Record<string, boolean>) => {
      if (!userId) return
      setPrefs((prev) => {
        const next = { ...prev, columnVisibility: newVisibility }
        savePrefs(userId, tableId, knownColumnIds, next)
        return next
      })
    },
    [userId, tableId, knownColumnIds]
  )

  return {
    sizing: prefs.sizing,
    onSizingChange,
    columnOrder: prefs.columnOrder,
    onColumnOrderChange,
    columnVisibility: prefs.columnVisibility,
    onColumnVisibilityChange,
  }
}
