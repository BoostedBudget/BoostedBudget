import { describe, it, expect } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { createElement } from 'react'

const { useUrlState } = await import('./useUrlState')

function wrapper(initialPath = '/') {
  return ({ children }: { children: React.ReactNode }) =>
    createElement(MemoryRouter, { initialEntries: [initialPath] }, children)
}

const numericOpts = {
  key: 'page',
  parse: (raw: string | null) => (raw !== null && !isNaN(Number(raw)) ? Number(raw) : null),
  serialize: (v: number | null) => (v !== null ? String(v) : null),
  defaultValue: null as number | null,
}

describe('useUrlState', () => {
  it('returns defaultValue when URL param is absent', () => {
    const { result } = renderHook(() => useUrlState({ ...numericOpts, defaultValue: 42 }), {
      wrapper: wrapper('/'),
    })
    expect(result.current.value).toBe(42)
    expect(result.current.isDefault).toBe(true)
  })

  it('parses value from URL param', () => {
    const { result } = renderHook(() => useUrlState({ ...numericOpts, defaultValue: null }), {
      wrapper: wrapper('/?page=7'),
    })
    expect(result.current.value).toBe(7)
    expect(result.current.isDefault).toBe(false)
  })

  it('setValue updates the value', () => {
    const { result } = renderHook(() => useUrlState({ ...numericOpts, defaultValue: null }), {
      wrapper: wrapper('/'),
    })
    act(() => {
      result.current.setValue(3)
    })
    expect(result.current.value).toBe(3)
  })

  it('serialize returning null deletes the URL key', () => {
    const { result } = renderHook(() => useUrlState({ ...numericOpts, defaultValue: null }), {
      wrapper: wrapper('/?page=5'),
    })
    expect(result.current.value).toBe(5)
    act(() => {
      result.current.setValue(null)
    })
    expect(result.current.value).toBeNull()
    expect(result.current.isDefault).toBe(true)
  })

  it('reset() restores defaultValue and deletes the URL key', () => {
    const { result } = renderHook(() => useUrlState({ ...numericOpts, defaultValue: 1 }), {
      wrapper: wrapper('/?page=9'),
    })
    expect(result.current.value).toBe(9)
    act(() => {
      result.current.reset()
    })
    expect(result.current.value).toBe(1)
    expect(result.current.isDefault).toBe(true)
  })

  it('preserves unrelated URL params when setting a value', () => {
    const stringOpts = {
      key: 'tab',
      parse: (raw: string | null) => raw,
      serialize: (v: string | null) => v,
      defaultValue: null as string | null,
    }
    const { result } = renderHook(() => useUrlState({ ...stringOpts }), {
      wrapper: wrapper('/?other=keep&tab=old'),
    })
    act(() => {
      result.current.setValue('new')
    })
    expect(result.current.value).toBe('new')
  })

  it('preserves unrelated URL params when deleting a key via reset()', () => {
    const stringOpts = {
      key: 'tab',
      parse: (raw: string | null) => raw,
      serialize: (v: string | null) => v,
      defaultValue: null as string | null,
    }
    const { result } = renderHook(() => useUrlState({ ...stringOpts }), {
      wrapper: wrapper('/?other=keep&tab=hello'),
    })
    act(() => {
      result.current.reset()
    })
    expect(result.current.value).toBeNull()
  })

  it('two instances with different keys are independent', () => {
    const makeOpts = (key: string) => ({
      key,
      parse: (raw: string | null) => raw,
      serialize: (v: string | null) => v,
      defaultValue: null as string | null,
    })
    const { result: r1 } = renderHook(() => useUrlState(makeOpts('foo')), {
      wrapper: wrapper('/?foo=a&bar=b'),
    })
    const { result: r2 } = renderHook(() => useUrlState(makeOpts('bar')), {
      wrapper: wrapper('/?foo=a&bar=b'),
    })
    expect(r1.current.value).toBe('a')
    expect(r2.current.value).toBe('b')
    act(() => {
      r1.current.setValue('x')
    })
    expect(r1.current.value).toBe('x')
    expect(r2.current.value).toBe('b')
  })

  it('parse returning defaultValue keeps isDefault true', () => {
    const { result } = renderHook(() => useUrlState({ ...numericOpts, defaultValue: null }), {
      wrapper: wrapper('/?page=notanumber'),
    })
    expect(result.current.value).toBeNull()
    expect(result.current.isDefault).toBe(true)
  })

  it('isDefault is computed via custom isDefault option', () => {
    const opts = {
      key: 'sort',
      parse: (raw: string | null) => raw ?? 'asc',
      serialize: (v: string) => (v === 'asc' ? null : v),
      defaultValue: 'asc',
      isDefault: (v: string) => v === 'asc',
    }
    const { result } = renderHook(() => useUrlState(opts), {
      wrapper: wrapper('/?sort=desc'),
    })
    expect(result.current.isDefault).toBe(false)
    act(() => {
      result.current.setValue('asc')
    })
    expect(result.current.isDefault).toBe(true)
  })
})
