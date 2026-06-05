/**
 * useRowSelection — lightweight row selection state for the Review Inbox.
 *
 * Tracks a Set of selected row IDs. Provides helpers for single toggle,
 * select-all (given a list of ids), and clear. Does not touch React Query or
 * the API layer — selection state is ephemeral and resets on navigation.
 */

import { useState, useCallback } from 'react'

export interface UseRowSelectionResult {
  selectedIds: Set<string>
  selectedCount: number
  isSelected: (id: string) => boolean
  toggle: (id: string) => void
  selectAll: (ids: string[]) => void
  clearSelection: () => void
}

export function useRowSelection(): UseRowSelectionResult {
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())

  const isSelected = useCallback((id: string) => selectedIds.has(id), [selectedIds])

  const toggle = useCallback((id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })
  }, [])

  const selectAll = useCallback((ids: string[]) => {
    setSelectedIds(new Set(ids))
  }, [])

  const clearSelection = useCallback(() => {
    setSelectedIds(new Set())
  }, [])

  return {
    selectedIds,
    selectedCount: selectedIds.size,
    isSelected,
    toggle,
    selectAll,
    clearSelection,
  }
}
