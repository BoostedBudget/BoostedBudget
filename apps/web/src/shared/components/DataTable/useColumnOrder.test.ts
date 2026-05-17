import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { useColumnOrder, arrayMoveById } from './useColumnOrder'
import type { ColumnDef } from './types'

// posthog is mocked via src/test/mocks/posthog.ts (preloaded in bunfig.toml).
// useTrackEvent calls posthog.capture which is a no-op mock in tests.
// We don't assert analytics firing here — that's manual verification in staging.

type Row = { id: string; name: string; amount: number }

const columns: ColumnDef<Row>[] = [
  { id: 'name', header: 'Name', key: 'name', reorderable: true },
  { id: 'amount', header: 'Amount', key: 'amount', reorderable: true },
  { id: 'date', header: 'Date', key: 'id', reorderable: true },
]

// Minimal DragEndEvent shape — only fields used by handleDragEnd
type MinimalDragEnd = {
  active: { id: string | number }
  over: { id: string | number } | null
}

function makeDragEvent(activeId: string, overId: string | null): MinimalDragEnd {
  return {
    active: { id: activeId },
    over: overId != null ? { id: overId } : null,
  }
}

// ── arrayMoveById pure helper ────────────────────────────────────────────────
describe('arrayMoveById', () => {
  it('swaps two adjacent items when active and over differ', () => {
    expect(arrayMoveById(['name', 'amount'], 'name', 'amount')).toEqual(['amount', 'name'])
  })

  it('returns array unchanged when activeId equals overId', () => {
    const arr = ['name', 'amount']
    expect(arrayMoveById(arr, 'name', 'name')).toEqual(['name', 'amount'])
  })

  it('moves first item to last in a 3-element array', () => {
    expect(arrayMoveById(['a', 'b', 'c'], 'a', 'c')).toEqual(['b', 'c', 'a'])
  })

  it('moves last item to first in a 3-element array', () => {
    expect(arrayMoveById(['a', 'b', 'c'], 'c', 'a')).toEqual(['c', 'a', 'b'])
  })

  it('returns original array when activeId not found', () => {
    expect(arrayMoveById(['name', 'amount'], 'unknown', 'amount')).toEqual(['name', 'amount'])
  })

  it('returns original array when overId not found', () => {
    expect(arrayMoveById(['name', 'amount'], 'name', 'unknown')).toEqual(['name', 'amount'])
  })
})

// ── useColumnOrder — handleDragEnd ───────────────────────────────────────────
describe('useColumnOrder — handleDragEnd', () => {
  it('hook returns handleDragEnd function', () => {
    const { result } = renderHook(() =>
      useColumnOrder(columns, undefined, undefined, 'test-table')
    )
    expect(typeof result.current.handleDragEnd).toBe('function')
  })

  it('handleDragEnd calls onControlledChange with reordered ids on drop', () => {
    const onControlledChange = mock((_: string[]) => {})
    const { result } = renderHook(() =>
      useColumnOrder(
        columns,
        ['name', 'amount', 'date'],
        onControlledChange,
        'test-table'
      )
    )
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    act(() => { result.current.handleDragEnd(makeDragEvent('name', 'amount') as any) })
    expect(onControlledChange).toHaveBeenCalledTimes(1)
    expect(onControlledChange.mock.calls[0][0]).toEqual(['amount', 'name', 'date'])
  })

  it('handleDragEnd does NOT call onControlledChange when active.id === over.id', () => {
    const onControlledChange = mock((_: string[]) => {})
    const { result } = renderHook(() =>
      useColumnOrder(
        columns,
        ['name', 'amount', 'date'],
        onControlledChange,
        'test-table'
      )
    )
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    act(() => { result.current.handleDragEnd(makeDragEvent('name', 'name') as any) })
    expect(onControlledChange).not.toHaveBeenCalled()
  })

  it('handleDragEnd does NOT call onControlledChange when over is null', () => {
    const onControlledChange = mock((_: string[]) => {})
    const { result } = renderHook(() =>
      useColumnOrder(
        columns,
        ['name', 'amount', 'date'],
        onControlledChange,
        'test-table'
      )
    )
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    act(() => { result.current.handleDragEnd(makeDragEvent('name', null) as any) })
    expect(onControlledChange).not.toHaveBeenCalled()
  })

  it('handleDragEnd updates internal order when no onControlledChange is provided', () => {
    const { result } = renderHook(() =>
      useColumnOrder(columns, undefined, undefined, 'test-table')
    )
    const initialOrder = result.current.orderedColumns.map((c) => c.id)
    expect(initialOrder).toEqual(['name', 'amount', 'date'])
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    act(() => { result.current.handleDragEnd(makeDragEvent('name', 'amount') as any) })
    const newOrder = result.current.orderedColumns.map((c) => c.id)
    expect(newOrder).toEqual(['amount', 'name', 'date'])
  })

  it('handleDragEnd fires ONLY ONCE on single drop (not per drag-move frame)', () => {
    const onControlledChange = mock((_: string[]) => {})
    const { result } = renderHook(() =>
      useColumnOrder(
        columns,
        ['name', 'amount', 'date'],
        onControlledChange,
        'test-table'
      )
    )
    act(() => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      result.current.handleDragEnd(makeDragEvent('name', 'amount') as any)
    })
    expect(onControlledChange).toHaveBeenCalledTimes(1)
  })
})

// ── useColumnOrder — fine-pointer gate ──────────────────────────────────────
describe('useColumnOrder — isFinePointer', () => {
  const originalMatchMedia = window.matchMedia

  beforeEach(() => {
    window.matchMedia = originalMatchMedia
  })

  it('returns isFinePointer=true in a fine-pointer environment', () => {
    window.matchMedia = mock((query: string) => ({
      matches: query === '(pointer: fine)',
      media: query,
      onchange: null,
      addListener: mock(() => {}),
      removeListener: mock(() => {}),
      addEventListener: mock(() => {}),
      removeEventListener: mock(() => {}),
      dispatchEvent: mock(() => true),
    })) as unknown as typeof window.matchMedia

    const { result } = renderHook(() =>
      useColumnOrder(columns, undefined, undefined, 'test-table')
    )
    expect(result.current.isFinePointer).toBe(true)
  })

  it('returns isFinePointer=false in a coarse-pointer environment', () => {
    window.matchMedia = mock((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: mock(() => {}),
      removeListener: mock(() => {}),
      addEventListener: mock(() => {}),
      removeEventListener: mock(() => {}),
      dispatchEvent: mock(() => true),
    })) as unknown as typeof window.matchMedia

    const { result } = renderHook(() =>
      useColumnOrder(columns, undefined, undefined, 'test-table')
    )
    expect(result.current.isFinePointer).toBe(false)
  })

  it('handleDragEnd is a no-op when isFinePointer=false (coarse pointer)', () => {
    window.matchMedia = mock((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: mock(() => {}),
      removeListener: mock(() => {}),
      addEventListener: mock(() => {}),
      removeEventListener: mock(() => {}),
      dispatchEvent: mock(() => true),
    })) as unknown as typeof window.matchMedia

    const onControlledChange = mock((_: string[]) => {})
    const { result } = renderHook(() =>
      useColumnOrder(
        columns,
        ['name', 'amount', 'date'],
        onControlledChange,
        'test-table'
      )
    )
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    act(() => { result.current.handleDragEnd(makeDragEvent('name', 'amount') as any) })
    expect(onControlledChange).not.toHaveBeenCalled()
  })
})
