import { useState, useCallback } from 'react'
import { useUser } from '@clerk/react-router'

interface TablePrefs {
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
  const expectedVersion = knownColumnIds.join(',')
  const knownSet = new Set(knownColumnIds)

  let raw: string | null
  try {
    raw = localStorage.getItem(key)
  } catch {
    return DEFAULT_PREFS
  }

  if (raw === null) return DEFAULT_PREFS

  let parsed: TablePrefs
  try {
    parsed = JSON.parse(raw) as TablePrefs
  } catch {
    return DEFAULT_PREFS
  }

  // Version mismatch → reset to defaults and persist
  if (parsed.version !== expectedVersion) {
    const reset: TablePrefs = { version: expectedVersion, ...DEFAULT_PREFS }
    try {
      localStorage.setItem(key, JSON.stringify(reset))
    } catch {
      // ignore write errors
    }
    return DEFAULT_PREFS
  }

  return {
    sizing: filterByKnownIds(parsed.sizing ?? {}, knownSet),
    columnOrder: (parsed.columnOrder ?? []).filter((id) => knownSet.has(id)),
    columnVisibility: filterByKnownIds(parsed.columnVisibility ?? {}, knownSet),
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
      JSON.stringify({ version: knownColumnIds.join(','), ...prefs })
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
 * If the stored version (knownColumnIds.join(',')) doesn't match, prefs are reset.
 */
export function useTablePreferences(
  tableId: string,
  knownColumnIds: string[]
): UseTablePreferencesResult {
  const { user } = useUser()
  const userId = user?.id ?? null

  // Single state atom — prevents stale-closure cross-field overwrites when two setters
  // fire in the same render cycle. Lazy initializer runs only once (on mount).
  const [prefs, setPrefs] = useState<Pick<TablePrefs, 'sizing' | 'columnOrder' | 'columnVisibility'>>(
    () => (userId ? loadPrefs(userId, tableId, knownColumnIds) : DEFAULT_PREFS)
  )

  // Derived state: when Clerk resolves userId after the initial null render, reload prefs.
  // Calling setState during render (React's recommended pattern for derived state) avoids
  // an extra commit cycle compared to useEffect.
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
