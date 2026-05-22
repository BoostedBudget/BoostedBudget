import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { mock } from 'bun:test'
import { useKeyboardColumnResize } from './useKeyboardColumnResize'
import { ANALYTICS_EVENTS } from '@/shared/analytics'

// Mock useTrackEvent (avoids posthog module-resolution ordering issues with process-global mock.module calls)
const mockTrackEvent = mock(() => {})

mock.module('@/shared/hooks/useTrackEvent', () => ({
  useTrackEvent: () => mockTrackEvent,
}))

const TABLE_ID = 'test-table'

function makeKeyEvent(key: string, shiftKey = false): React.KeyboardEvent {
  return { key, shiftKey, preventDefault: mock(() => {}) } as unknown as React.KeyboardEvent
}

function setup(sizing: Record<string, number> = {}) {
  const onSizingChange = mock((_: Record<string, number>) => {})
  const { result } = renderHook(() =>
    useKeyboardColumnResize(sizing, onSizingChange, TABLE_ID)
  )
  return { result, onSizingChange }
}

describe('useKeyboardColumnResize', () => {
  beforeEach(() => mockTrackEvent.mockClear())

  describe('ArrowRight', () => {
    it('increments width by 8px (default step)', () => {
      const { result, onSizingChange } = setup({ x: 200 })
      act(() => result.current.handleKeyDown(makeKeyEvent('ArrowRight'), 'x', 200, 50, 400))
      expect(onSizingChange.mock.calls[0][0]).toEqual({ x: 208 })
    })

    it('increments by 32px with Shift', () => {
      const { result, onSizingChange } = setup({ x: 200 })
      act(() => result.current.handleKeyDown(makeKeyEvent('ArrowRight', true), 'x', 200, 50, 400))
      expect(onSizingChange.mock.calls[0][0]).toEqual({ x: 232 })
    })

    it('clamps at maxSize', () => {
      const { result, onSizingChange } = setup({ x: 395 })
      act(() => result.current.handleKeyDown(makeKeyEvent('ArrowRight'), 'x', 200, 50, 400))
      expect(onSizingChange.mock.calls[0][0]).toEqual({ x: 400 })
    })

    it('uses defaultSize as current width when column absent from sizing', () => {
      const { result, onSizingChange } = setup({})
      act(() => result.current.handleKeyDown(makeKeyEvent('ArrowRight'), 'x', 180, 50, 400))
      expect(onSizingChange.mock.calls[0][0]).toEqual({ x: 188 })
    })
  })

  describe('ArrowLeft', () => {
    it('decrements width by 8px', () => {
      const { result, onSizingChange } = setup({ x: 200 })
      act(() => result.current.handleKeyDown(makeKeyEvent('ArrowLeft'), 'x', 200, 50, 400))
      expect(onSizingChange.mock.calls[0][0]).toEqual({ x: 192 })
    })

    it('decrements by 32px with Shift', () => {
      const { result, onSizingChange } = setup({ x: 200 })
      act(() => result.current.handleKeyDown(makeKeyEvent('ArrowLeft', true), 'x', 200, 50, 400))
      expect(onSizingChange.mock.calls[0][0]).toEqual({ x: 168 })
    })

    it('clamps at minSize', () => {
      const { result, onSizingChange } = setup({ x: 52 })
      act(() => result.current.handleKeyDown(makeKeyEvent('ArrowLeft'), 'x', 200, 50))
      expect(onSizingChange.mock.calls[0][0]).toEqual({ x: 50 })
    })

    it('clamps at DEFAULT_MIN_WIDTH (50) when minSize is undefined', () => {
      const { result, onSizingChange } = setup({ x: 53 })
      act(() => result.current.handleKeyDown(makeKeyEvent('ArrowLeft'), 'x', 200))
      expect(onSizingChange.mock.calls[0][0]).toEqual({ x: 50 })
    })
  })

  describe('Home', () => {
    it('resets to defaultSize', () => {
      const { result, onSizingChange } = setup({ x: 350 })
      act(() => result.current.handleKeyDown(makeKeyEvent('Home'), 'x', 200, 50, 400))
      expect(onSizingChange.mock.calls[0][0]).toEqual({ x: 200 })
    })

    it('falls back to 150 when defaultSize is undefined', () => {
      const { result, onSizingChange } = setup({ x: 350 })
      act(() => result.current.handleKeyDown(makeKeyEvent('Home'), 'x', undefined, 50))
      expect(onSizingChange.mock.calls[0][0]).toEqual({ x: 150 })
    })
  })

  describe('non-resize keys', () => {
    it('Tab does not call onSizingChange', () => {
      const { result, onSizingChange } = setup({ x: 200 })
      act(() => result.current.handleKeyDown(makeKeyEvent('Tab'), 'x', 200, 50, 400))
      expect(onSizingChange.mock.calls.length).toBe(0)
    })

    it('Enter does not call onSizingChange', () => {
      const { result, onSizingChange } = setup({ x: 200 })
      act(() => result.current.handleKeyDown(makeKeyEvent('Enter'), 'x', 200, 50, 400))
      expect(onSizingChange.mock.calls.length).toBe(0)
    })

    it('non-resize key does not fire trackEvent', () => {
      const { result } = setup({ x: 200 })
      act(() => result.current.handleKeyDown(makeKeyEvent('Tab'), 'x', 200, 50, 400))
      expect(mockTrackEvent.mock.calls.length).toBe(0)
    })
  })

  describe('PostHog DATATABLE_COLUMN_RESIZED', () => {
    it('fires on ArrowRight resize', () => {
      const { result } = setup({ x: 200 })
      act(() => result.current.handleKeyDown(makeKeyEvent('ArrowRight'), 'x', 200, 50, 400))
      expect(mockTrackEvent.mock.calls.length).toBe(1)
      expect(mockTrackEvent.mock.calls[0][0]).toBe(ANALYTICS_EVENTS.DATATABLE_COLUMN_RESIZED)
      expect(mockTrackEvent.mock.calls[0][1]).toMatchObject({ tableId: TABLE_ID, columnId: 'x' })
    })

    it('fires on ArrowLeft resize', () => {
      const { result } = setup({ x: 200 })
      act(() => result.current.handleKeyDown(makeKeyEvent('ArrowLeft'), 'x', 200, 50, 400))
      expect(mockTrackEvent.mock.calls.length).toBe(1)
      expect(mockTrackEvent.mock.calls[0][0]).toBe(ANALYTICS_EVENTS.DATATABLE_COLUMN_RESIZED)
    })

    it('fires on Home resize', () => {
      const { result } = setup({ x: 350 })
      act(() => result.current.handleKeyDown(makeKeyEvent('Home'), 'x', 200, 50, 400))
      expect(mockTrackEvent.mock.calls.length).toBe(1)
      expect(mockTrackEvent.mock.calls[0][0]).toBe(ANALYTICS_EVENTS.DATATABLE_COLUMN_RESIZED)
    })
  })

  describe('getWidth', () => {
    it('returns columnSizing value when present', () => {
      const { result } = setup({ x: 250 })
      expect(result.current.getWidth('x', 200)).toBe(250)
    })

    it('falls back to defaultSize when column not in sizing', () => {
      const { result } = setup({})
      expect(result.current.getWidth('x', 180)).toBe(180)
    })

    it('falls back to 150 when both sizing and defaultSize are absent', () => {
      const { result } = setup({})
      expect(result.current.getWidth('x', undefined)).toBe(150)
    })
  })

  describe('resize preserves other column sizing', () => {
    it('keeps sibling columns untouched after ArrowRight', () => {
      const { result, onSizingChange } = setup({ x: 200, y: 300 })
      act(() => result.current.handleKeyDown(makeKeyEvent('ArrowRight'), 'x', 200, 50, 400))
      expect(onSizingChange.mock.calls[0][0]).toEqual({ x: 208, y: 300 })
    })
  })
})
