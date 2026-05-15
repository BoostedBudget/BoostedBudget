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

const DEFAULT_PREFS = {
  sizing: {} as Record<string, number>,
  columnOrder: [] as string[],
  columnVisibility: {} as Record<string, boolean>,
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
): { sizing: Record<string, number>; columnOrder: string[]; columnVisibility: Record<string, boolean> } {
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
    const reset: TablePrefs = {
      version: expectedVersion,
      sizing: {},
      columnOrder: [],
      columnVisibility: {},
    }
    try {
      localStorage.setItem(key, JSON.stringify(reset))
    } catch {
      // ignore write errors
    }
    return DEFAULT_PREFS
  }

  // Filter out unknown column ids from all prefs
  const sizing = filterByKnownIds(parsed.sizing ?? {}, knownSet)
  const columnOrder = (parsed.columnOrder ?? []).filter((id) => knownSet.has(id))
  const columnVisibility = filterByKnownIds(parsed.columnVisibility ?? {}, knownSet)

  return { sizing, columnOrder, columnVisibility }
}

function savePrefs(
  userId: string,
  tableId: string,
  knownColumnIds: string[],
  prefs: { sizing: Record<string, number>; columnOrder: string[]; columnVisibility: Record<string, boolean> }
): void {
  const key = storageKey(userId, tableId)
  const data: TablePrefs = {
    version: knownColumnIds.join(','),
    sizing: prefs.sizing,
    columnOrder: prefs.columnOrder,
    columnVisibility: prefs.columnVisibility,
  }
  try {
    localStorage.setItem(key, JSON.stringify(data))
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

  const initialPrefs = userId
    ? loadPrefs(userId, tableId, knownColumnIds)
    : DEFAULT_PREFS

  const [sizing, setSizing] = useState<Record<string, number>>(initialPrefs.sizing)
  const [columnOrder, setColumnOrder] = useState<string[]>(initialPrefs.columnOrder)
  const [columnVisibility, setColumnVisibility] = useState<Record<string, boolean>>(
    initialPrefs.columnVisibility
  )

  const onSizingChange = useCallback(
    (newSizing: Record<string, number>) => {
      if (!userId) return
      setSizing(newSizing)
      savePrefs(userId, tableId, knownColumnIds, {
        sizing: newSizing,
        columnOrder,
        columnVisibility,
      })
    },
    [userId, tableId, knownColumnIds, columnOrder, columnVisibility]
  )

  const onColumnOrderChange = useCallback(
    (newOrder: string[]) => {
      if (!userId) return
      setColumnOrder(newOrder)
      savePrefs(userId, tableId, knownColumnIds, {
        sizing,
        columnOrder: newOrder,
        columnVisibility,
      })
    },
    [userId, tableId, knownColumnIds, sizing, columnVisibility]
  )

  const onColumnVisibilityChange = useCallback(
    (newVisibility: Record<string, boolean>) => {
      if (!userId) return
      setColumnVisibility(newVisibility)
      savePrefs(userId, tableId, knownColumnIds, {
        sizing,
        columnOrder,
        columnVisibility: newVisibility,
      })
    },
    [userId, tableId, knownColumnIds, sizing, columnOrder]
  )

  return {
    sizing,
    onSizingChange,
    columnOrder,
    onColumnOrderChange,
    columnVisibility,
    onColumnVisibilityChange,
  }
}
