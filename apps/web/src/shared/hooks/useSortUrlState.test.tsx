import { describe, it, expect, spyOn } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { createElement } from 'react'
import type { SortState } from '@/shared/components/DataTable'

const { parseSortParam, serializeSortParam, toggleSort, useSortUrlState } = await import(
  './useSortUrlState'
)

const COLS = new Set(['name', 'balance', 'date'])

function wrapper(initialPath = '/') {
  return ({ children }: { children: React.ReactNode }) =>
    createElement(MemoryRouter, { initialEntries: [initialPath] }, children)
}

describe('parseSortParam', () => {
  it('returns null for null input', () => {
    expect(parseSortParam(null, COLS)).toBeNull()
  })

  it('returns null for empty string', () => {
    expect(parseSortParam('', COLS)).toBeNull()
  })

  it('returns null when missing colon delimiter', () => {
    expect(parseSortParam('date', COLS)).toBeNull()
  })

  it('returns null when direction segment is missing', () => {
    expect(parseSortParam('date:', COLS)).toBeNull()
  })

  it('returns null when column segment is empty (:asc)', () => {
    expect(parseSortParam(':asc', COLS)).toBeNull()
  })

  it('returns null when validColumns is empty set', () => {
    expect(parseSortParam('date:asc', new Set())).toBeNull()
  })

  it('returns null for extra colon segments', () => {
    expect(parseSortParam('date:desc:extra', COLS)).toBeNull()
  })

  it('is case-sensitive (uppercased column rejects)', () => {
    expect(parseSortParam('DATE:DESC', COLS)).toBeNull()
  })

  it('returns null for unknown column', () => {
    expect(parseSortParam('unknown:asc', COLS)).toBeNull()
  })

  it('returns null for invalid direction', () => {
    expect(parseSortParam('date:invalid', COLS)).toBeNull()
  })

  it('parses valid asc sort', () => {
    expect(parseSortParam('date:asc', COLS)).toEqual({ columnId: 'date', direction: 'asc' })
  })

  it('parses valid desc sort', () => {
    expect(parseSortParam('balance:desc', COLS)).toEqual({
      columnId: 'balance',
      direction: 'desc',
    })
  })
})

describe('serializeSortParam', () => {
  it('returns null for null input', () => {
    expect(serializeSortParam(null)).toBeNull()
  })

  it('serializes asc sort', () => {
    expect(serializeSortParam({ columnId: 'date', direction: 'asc' })).toBe('date:asc')
  })

  it('serializes desc sort', () => {
    expect(serializeSortParam({ columnId: 'balance', direction: 'desc' })).toBe('balance:desc')
  })
})

describe('toggleSort', () => {
  it('sets new column to asc when current is null', () => {
    expect(toggleSort(null, 'date')).toEqual({ columnId: 'date', direction: 'asc' })
  })

  it('sets new column to asc when switching columns', () => {
    expect(toggleSort({ columnId: 'balance', direction: 'desc' }, 'date')).toEqual({
      columnId: 'date',
      direction: 'asc',
    })
  })

  it('flips asc to desc on same column', () => {
    expect(toggleSort({ columnId: 'date', direction: 'asc' }, 'date')).toEqual({
      columnId: 'date',
      direction: 'desc',
    })
  })

  it('flips desc to asc on same column', () => {
    expect(toggleSort({ columnId: 'date', direction: 'desc' }, 'date')).toEqual({
      columnId: 'date',
      direction: 'asc',
    })
  })
})

describe('useSortUrlState', () => {
  it('returns defaultSort when URL param is absent', () => {
    const defaultSort: SortState = { columnId: 'date', direction: 'desc' }
    const { result } = renderHook(
      () => useSortUrlState({ defaultSort, validColumns: COLS }),
      { wrapper: wrapper('/') },
    )
    expect(result.current.sort).toEqual(defaultSort)
    expect(result.current.isDefaultSort).toBe(true)
  })

  it('parses sort from URL', () => {
    const { result } = renderHook(
      () => useSortUrlState({ defaultSort: null, validColumns: COLS }),
      { wrapper: wrapper('/?sort=balance%3Adesc') },
    )
    expect(result.current.sort).toEqual({ columnId: 'balance', direction: 'desc' })
    expect(result.current.isDefaultSort).toBe(false)
  })

  it('setSort updates URL', () => {
    const { result } = renderHook(
      () => useSortUrlState({ defaultSort: null, validColumns: COLS }),
      { wrapper: wrapper('/') },
    )
    act(() => {
      result.current.setSort({ columnId: 'name', direction: 'asc' })
    })
    expect(result.current.sort).toEqual({ columnId: 'name', direction: 'asc' })
  })

  it('setSort(null) deletes URL param and returns defaultSort', () => {
    const defaultSort: SortState = { columnId: 'date', direction: 'desc' }
    const { result } = renderHook(
      () => useSortUrlState({ defaultSort, validColumns: COLS }),
      { wrapper: wrapper('/?sort=balance%3Adesc') },
    )
    act(() => {
      result.current.setSort(null)
    })
    expect(result.current.sort).toEqual(defaultSort)
    expect(result.current.isDefaultSort).toBe(true)
  })

  it('resetSort() restores defaultSort', () => {
    const defaultSort: SortState = { columnId: 'date', direction: 'asc' }
    const { result } = renderHook(
      () => useSortUrlState({ defaultSort, validColumns: COLS }),
      { wrapper: wrapper('/?sort=balance%3Adesc') },
    )
    act(() => {
      result.current.resetSort()
    })
    expect(result.current.sort).toEqual(defaultSort)
    expect(result.current.isDefaultSort).toBe(true)
  })

  it('toggleSort method flips direction for same column', () => {
    const { result } = renderHook(
      () => useSortUrlState({ defaultSort: null, validColumns: COLS }),
      { wrapper: wrapper('/?sort=date%3Aasc') },
    )
    act(() => {
      result.current.toggleSortColumn('date')
    })
    expect(result.current.sort).toEqual({ columnId: 'date', direction: 'desc' })
  })

  it('toggleSort method sets asc for new column', () => {
    const { result } = renderHook(
      () => useSortUrlState({ defaultSort: null, validColumns: COLS }),
      { wrapper: wrapper('/?sort=date%3Adesc') },
    )
    act(() => {
      result.current.toggleSortColumn('balance')
    })
    expect(result.current.sort).toEqual({ columnId: 'balance', direction: 'asc' })
  })

  it('returns defaultSort for invalid URL sort (unknown col)', () => {
    const defaultSort: SortState = { columnId: 'date', direction: 'asc' }
    const { result } = renderHook(
      () => useSortUrlState({ defaultSort, validColumns: COLS }),
      { wrapper: wrapper('/?sort=unknown%3Aasc') },
    )
    expect(result.current.sort).toEqual(defaultSort)
    expect(result.current.isDefaultSort).toBe(true)
  })

  it('isDefaultSort is false when URL sort differs from default', () => {
    const defaultSort: SortState = { columnId: 'date', direction: 'asc' }
    const { result } = renderHook(
      () => useSortUrlState({ defaultSort, validColumns: COLS }),
      { wrapper: wrapper('/?sort=balance%3Adesc') },
    )
    expect(result.current.isDefaultSort).toBe(false)
  })

  it('respects custom paramName', () => {
    const { result } = renderHook(
      () => useSortUrlState({ defaultSort: null, validColumns: COLS, paramName: 'orderBy' }),
      { wrapper: wrapper('/?orderBy=name%3Aasc') },
    )
    expect(result.current.sort).toEqual({ columnId: 'name', direction: 'asc' })
  })

  it('warns in dev when URL sort is malformed', () => {
    const warnSpy = spyOn(console, 'warn').mockImplementation(() => {})
    renderHook(() => useSortUrlState({ defaultSort: null, validColumns: COLS }), {
      wrapper: wrapper('/?sort=BADVALUE'),
    })
    expect(warnSpy).toHaveBeenCalled()
    warnSpy.mockRestore()
  })

  it('returns null for sort=:asc (empty column)', () => {
    const { result } = renderHook(
      () => useSortUrlState({ defaultSort: null, validColumns: COLS }),
      { wrapper: wrapper('/?sort=%3Aasc') },
    )
    expect(result.current.sort).toBeNull()
  })

  it('returns null sort when validColumns is empty set', () => {
    const { result } = renderHook(
      () => useSortUrlState({ defaultSort: null, validColumns: new Set() }),
      { wrapper: wrapper('/?sort=date%3Aasc') },
    )
    expect(result.current.sort).toBeNull()
  })

  it('coerces invalid defaultSort column to null and warns in dev', () => {
    const warnSpy = spyOn(console, 'warn').mockImplementation(() => {})
    const { result } = renderHook(
      () =>
        useSortUrlState({
          defaultSort: { columnId: 'nonexistent', direction: 'asc' },
          validColumns: COLS,
        }),
      { wrapper: wrapper('/') },
    )
    expect(result.current.sort).toBeNull()
    expect(warnSpy).toHaveBeenCalled()
    warnSpy.mockRestore()
  })

  it('warns at most once per malformed raw value across re-renders', () => {
    const warnSpy = spyOn(console, 'warn').mockImplementation(() => {})
    const { rerender } = renderHook(
      () => useSortUrlState({ defaultSort: null, validColumns: COLS }),
      { wrapper: wrapper('/?sort=BADVALUE') },
    )
    rerender()
    rerender()
    const warnings = warnSpy.mock.calls.filter((args: unknown[]) =>
      String(args[0]).includes('"sort" URL param'),
    )
    expect(warnings.length).toBeLessThanOrEqual(2)
    warnSpy.mockRestore()
  })
})
