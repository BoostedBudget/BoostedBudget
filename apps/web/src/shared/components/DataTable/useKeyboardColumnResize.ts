import { useState, useCallback, useRef } from 'react'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics'

const DEFAULT_MIN_WIDTH = 50
const STEP = 8
const SHIFT_STEP = 32

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

export interface KeyboardColumnResizeState {
  isFinePointer: boolean
  colRefs: React.RefObject<Map<string, HTMLTableColElement | null>>
  getWidth: (columnId: string, defaultSize?: number) => number
  handleKeyDown: (
    e: React.KeyboardEvent,
    columnId: string,
    defaultSize: number | undefined,
    minSize?: number,
    maxSize?: number
  ) => void
}

export function useKeyboardColumnResize(
  columnSizing: Record<string, number>,
  onSizingChange: (sizing: Record<string, number>) => void,
  tableId?: string
): KeyboardColumnResizeState {
  const trackEvent = useTrackEvent()
  const [isFinePointer] = useState<boolean>(() => detectFinePointer())
  const colRefs = useRef<Map<string, HTMLTableColElement | null>>(new Map())

  const getWidth = useCallback(
    (columnId: string, defaultSize?: number): number =>
      columnSizing[columnId] ?? defaultSize ?? 150,
    [columnSizing]
  )

  const handleKeyDown = useCallback(
    (
      e: React.KeyboardEvent,
      columnId: string,
      defaultSize: number | undefined,
      minSize?: number,
      maxSize?: number
    ) => {
      const step = e.shiftKey ? SHIFT_STEP : STEP
      const current = getWidth(columnId, defaultSize)
      const lo = minSize ?? DEFAULT_MIN_WIDTH
      let next: number | null = null

      if (e.key === 'ArrowRight') {
        e.preventDefault()
        next = current + step
      } else if (e.key === 'ArrowLeft') {
        e.preventDefault()
        next = Math.max(lo, current - step)
      } else if (e.key === 'Home') {
        e.preventDefault()
        next = defaultSize ?? 150
      }

      if (next !== null) {
        const clamped = Math.max(lo, maxSize !== undefined ? Math.min(maxSize, next) : next)
        onSizingChange({ ...columnSizing, [columnId]: clamped })
        trackEvent(ANALYTICS_EVENTS.DATATABLE_COLUMN_RESIZED, { tableId, columnId })
      }
    },
    [getWidth, columnSizing, onSizingChange, trackEvent, tableId]
  )

  return { isFinePointer, colRefs, getWidth, handleKeyDown }
}
