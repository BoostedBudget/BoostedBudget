import { useState, useCallback, useMemo } from 'react'
import type { ColumnDef } from './types'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics'

function moveItem<T>(arr: T[], fromIndex: number, toIndex: number): T[] {
  const next = [...arr]
  const [item] = next.splice(fromIndex, 1)
  next.splice(toIndex, 0, item)
  return next
}

/** Pure helper: move `activeId` to the position of `overId` in `ids`. */
export function arrayMoveById(ids: string[], activeId: string, overId: string): string[] {
  const fromIndex = ids.indexOf(activeId)
  const toIndex = ids.indexOf(overId)
  if (fromIndex === -1 || toIndex === -1 || fromIndex === toIndex) return ids
  return moveItem(ids, fromIndex, toIndex)
}

function detectFinePointer(): boolean {
  try {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return true
    return window.matchMedia('(pointer: fine)').matches
  } catch {
    return true
  }
}

// Minimal type covering only what handleDragEnd uses — avoids importing all of @dnd-kit/core.
type DragEndEventMinimal = {
  active: { id: string | number }
  over: { id: string | number } | null
}

export interface UseColumnOrderResult<T> {
  orderedColumns: ColumnDef<T>[]
  draggingId: string | null
  liveText: string
  isFinePointer: boolean
  handleDragHandleKeyDown: (colId: string, e: React.KeyboardEvent) => void
  handleDragEnd: (event: DragEndEventMinimal) => void
}

export function useColumnOrder<T>(
  columns: ColumnDef<T>[],
  controlledOrder: string[] | undefined,
  onControlledChange: ((order: string[]) => void) | undefined,
  tableId?: string
): UseColumnOrderResult<T> {
  const trackEvent = useTrackEvent()
  const [internalOrder, setInternalOrder] = useState<string[]>(
    () => controlledOrder ?? columns.map((c) => c.id)
  )
  const [draggingId, setDraggingId] = useState<string | null>(null)
  const [draftOrder, setDraftOrder] = useState<string[] | null>(null)
  const [liveText, setLiveText] = useState('')
  const [isFinePointer] = useState<boolean>(() => detectFinePointer())

  // Reconcile committed order when columns change: prune removed ids, append new ones.
  // Done during render (not in an effect) to avoid cascading re-renders.
  const committedOrder = useMemo(() => {
    const base = controlledOrder ?? internalOrder
    const colIds = new Set(columns.map((c) => c.id))
    const pruned = base.filter((id) => colIds.has(id))
    const newIds = columns.map((c) => c.id).filter((id) => !base.includes(id))
    if (pruned.length === base.length && newIds.length === 0) return base
    return [...pruned, ...newIds]
  }, [controlledOrder, internalOrder, columns])
  const displayOrder = draftOrder ?? committedOrder

  const orderedColumns = displayOrder
    .map((id) => columns.find((c) => c.id === id))
    .filter((c): c is ColumnDef<T> => c !== undefined)

  const setOrder = useCallback(
    (newOrder: string[]) => {
      if (onControlledChange) {
        onControlledChange(newOrder)
      } else {
        setInternalOrder(newOrder)
      }
    },
    [onControlledChange]
  )

  const handleDragHandleKeyDown = useCallback(
    (colId: string, e: React.KeyboardEvent) => {
      if (!columns.find((c) => c.id === colId)?.reorderable) return

      if (e.key === ' ' || e.key === 'Enter') {
        e.preventDefault()
        if (draggingId === null) {
          setDraggingId(colId)
          setDraftOrder([...committedOrder])
          const label = columns.find((c) => c.id === colId)?.header
          const headerText = typeof label === 'string' ? label : colId
          setLiveText(
            `Picked up column ${headerText}. Use arrow keys to move, Space or Enter to drop, Escape to cancel.`
          )
        } else if (draggingId === colId) {
          const finalOrder = draftOrder ?? committedOrder
          const fromIndex = committedOrder.indexOf(colId)
          const toIndex = finalOrder.indexOf(colId)
          setOrder(finalOrder)
          setDraggingId(null)
          setDraftOrder(null)
          const label = columns.find((c) => c.id === colId)?.header
          const headerText = typeof label === 'string' ? label : colId
          setLiveText(`Dropped column ${headerText}.`)
          trackEvent(ANALYTICS_EVENTS.DATATABLE_COLUMN_REORDERED, {
            tableId,
            columnId: colId,
            fromIndex,
            toIndex,
          })
        }
        return
      }

      if (draggingId !== colId) return

      if (e.key === 'Escape') {
        e.preventDefault()
        setDraggingId(null)
        setDraftOrder(null)
        const label = columns.find((c) => c.id === colId)?.header
        const headerText = typeof label === 'string' ? label : colId
        setLiveText(`Cancelled reorder of column ${headerText}.`)
        return
      }

      const currentOrder = draftOrder ?? committedOrder
      const idx = currentOrder.indexOf(colId)

      if (e.key === 'ArrowRight' && idx < currentOrder.length - 1) {
        e.preventDefault()
        const newOrder = moveItem(currentOrder, idx, idx + 1)
        setDraftOrder(newOrder)
        const label = columns.find((c) => c.id === colId)?.header
        const headerText = typeof label === 'string' ? label : colId
        setLiveText(
          `Column ${headerText} moved to position ${idx + 2} of ${newOrder.length}.`
        )
        return
      }

      if (e.key === 'ArrowLeft' && idx > 0) {
        e.preventDefault()
        const newOrder = moveItem(currentOrder, idx, idx - 1)
        setDraftOrder(newOrder)
        const label = columns.find((c) => c.id === colId)?.header
        const headerText = typeof label === 'string' ? label : colId
        setLiveText(
          `Column ${headerText} moved to position ${idx} of ${newOrder.length}.`
        )
      }
    },
    [draggingId, draftOrder, committedOrder, columns, setOrder, trackEvent, tableId]
  )

  const handleDragEnd = useCallback(
    (event: DragEndEventMinimal) => {
      if (!isFinePointer) return
      const { active, over } = event
      if (!over || active.id === over.id) return
      const activeId = String(active.id)
      const overId = String(over.id)
      const newOrder = arrayMoveById(committedOrder, activeId, overId)
      if (newOrder === committedOrder) return
      setOrder(newOrder)
      const fromIndex = committedOrder.indexOf(activeId)
      const toIndex = newOrder.indexOf(activeId)
      trackEvent(ANALYTICS_EVENTS.DATATABLE_COLUMN_REORDERED, {
        tableId,
        columnId: activeId,
        fromIndex,
        toIndex,
      })
    },
    [isFinePointer, committedOrder, setOrder, trackEvent, tableId]
  )

  return { orderedColumns, draggingId, liveText, isFinePointer, handleDragHandleKeyDown, handleDragEnd }
}
