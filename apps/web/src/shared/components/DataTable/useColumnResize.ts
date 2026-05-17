import { useState, useCallback, useRef } from 'react'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics'

const MIN_WIDTH = 50
const STEP = 8
const SHIFT_STEP = 32

/**
 * Detect fine pointer capability. Falls back to `true` (fine pointer) in
 * environments without matchMedia or where it throws (SSR, some test runners).
 */
function detectFinePointer(): boolean {
  try {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
      return true
    }
    return window.matchMedia('(pointer: fine)').matches
  } catch {
    return true
  }
}

export interface ColumnResizeState {
  sizing: Record<string, number>
  isFinePointer: boolean
  colRefs: React.RefObject<Map<string, HTMLTableColElement | null>>
  getWidth: (columnId: string, defaultSize?: number) => number
  handleKeyDown: (
    e: React.KeyboardEvent,
    columnId: string,
    defaultSize: number | undefined
  ) => void
  handlePointerDown: (
    e: React.PointerEvent,
    columnId: string,
    colEl: HTMLTableColElement | null
  ) => void
}

export function useColumnResize(
  controlledSizing: Record<string, number> | undefined,
  onControlledChange: ((sizing: Record<string, number>) => void) | undefined,
  tableId?: string
): ColumnResizeState {
  const trackEvent = useTrackEvent()
  const [internalSizing, setInternalSizing] = useState<Record<string, number>>(
    () => controlledSizing ?? {}
  )

  // Fine pointer detection: evaluated lazily on first render.
  // Tests that mock matchMedia before calling render() will see the correct value
  // because the lazy initializer runs synchronously during the first render call.
  const [isFinePointer] = useState<boolean>(() => detectFinePointer())

  const isControlled = controlledSizing !== undefined
  const sizing = isControlled ? controlledSizing : internalSizing

  const setSizing = useCallback(
    (updater: (prev: Record<string, number>) => Record<string, number>) => {
      const next = updater(sizing)
      if (isControlled) {
        onControlledChange?.(next)
      } else {
        setInternalSizing(next)
      }
    },
    [sizing, isControlled, onControlledChange]
  )

  // Map from column id to <col> DOM element for direct style mutations during drag
  const colRefs = useRef<Map<string, HTMLTableColElement | null>>(new Map())

  const getWidth = useCallback(
    (columnId: string, defaultSize?: number): number =>
      sizing[columnId] ?? defaultSize ?? 150,
    [sizing]
  )

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent, columnId: string, defaultSize: number | undefined) => {
      const step = e.shiftKey ? SHIFT_STEP : STEP
      const current = getWidth(columnId, defaultSize)
      let next: number | null = null

      if (e.key === 'ArrowRight') {
        e.preventDefault()
        next = current + step
      } else if (e.key === 'ArrowLeft') {
        e.preventDefault()
        next = Math.max(MIN_WIDTH, current - step)
      } else if (e.key === 'Home') {
        e.preventDefault()
        next = defaultSize ?? 150
      }

      if (next !== null) {
        setSizing((prev) => ({ ...prev, [columnId]: Math.max(MIN_WIDTH, next!) }))
        trackEvent(ANALYTICS_EVENTS.DATATABLE_COLUMN_RESIZED, { tableId, columnId })
      }
    },
    [getWidth, setSizing, trackEvent, tableId]
  )

  const handlePointerDown = useCallback(
    (e: React.PointerEvent, columnId: string, colEl: HTMLTableColElement | null) => {
      if (!isFinePointer) return

      e.preventDefault()
      const startX = e.clientX
      const startWidth = getWidth(columnId)
      const target = e.currentTarget as HTMLElement

      target.setPointerCapture(e.pointerId)

      function onMove(moveEvent: PointerEvent) {
        const newWidth = Math.max(MIN_WIDTH, startWidth + (moveEvent.clientX - startX))
        // Direct DOM mutation during drag for 60fps — React state updated on pointerup
        if (colEl) colEl.style.width = `${newWidth}px`
      }

      function onUp(upEvent: PointerEvent) {
        const newWidth = Math.max(MIN_WIDTH, startWidth + (upEvent.clientX - startX))
        setSizing((prev) => ({ ...prev, [columnId]: newWidth }))
        trackEvent(ANALYTICS_EVENTS.DATATABLE_COLUMN_RESIZED, { tableId, columnId })
        target.releasePointerCapture(upEvent.pointerId)
        target.removeEventListener('pointermove', onMove)
        target.removeEventListener('pointerup', onUp)
      }

      target.addEventListener('pointermove', onMove)
      target.addEventListener('pointerup', onUp)
    },
    [isFinePointer, getWidth, setSizing, trackEvent, tableId]
  )

  return { sizing, isFinePointer, colRefs, getWidth, handleKeyDown, handlePointerDown }
}
