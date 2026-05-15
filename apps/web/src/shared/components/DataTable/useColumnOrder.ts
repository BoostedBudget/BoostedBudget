import { useState, useCallback } from 'react'
import type { ColumnDef } from './types'

function moveItem<T>(arr: T[], fromIndex: number, toIndex: number): T[] {
  const next = [...arr]
  const [item] = next.splice(fromIndex, 1)
  next.splice(toIndex, 0, item)
  return next
}

export interface UseColumnOrderResult<T> {
  orderedColumns: ColumnDef<T>[]
  draggingId: string | null
  liveText: string
  handleDragHandleKeyDown: (colId: string) => (e: React.KeyboardEvent) => void
}

export function useColumnOrder<T>(
  columns: ColumnDef<T>[],
  controlledOrder: string[] | undefined,
  onControlledChange: ((order: string[]) => void) | undefined
): UseColumnOrderResult<T> {
  const [internalOrder, setInternalOrder] = useState<string[]>(
    () => controlledOrder ?? columns.map((c) => c.id)
  )
  const [draggingId, setDraggingId] = useState<string | null>(null)
  const [draftOrder, setDraftOrder] = useState<string[] | null>(null)
  const [liveText, setLiveText] = useState('')

  const committedOrder = controlledOrder ?? internalOrder
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
    (colId: string) => (e: React.KeyboardEvent) => {
      if (!columns.find((c) => c.id === colId)?.reorderable) return

      if (e.key === ' ' || (e.key === 'Enter' && draggingId === colId)) {
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
          setOrder(finalOrder)
          setDraggingId(null)
          setDraftOrder(null)
          const label = columns.find((c) => c.id === colId)?.header
          const headerText = typeof label === 'string' ? label : colId
          setLiveText(`Dropped column ${headerText}.`)
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
    [draggingId, draftOrder, committedOrder, columns, setOrder]
  )

  return { orderedColumns, draggingId, liveText, handleDragHandleKeyDown }
}
