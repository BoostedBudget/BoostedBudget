import { describe, it, expect } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { useRowSelection } from './useRowSelection'

describe('useRowSelection', () => {
  it('starts with no selection', () => {
    const { result } = renderHook(() => useRowSelection())
    expect(result.current.selectedCount).toBe(0)
    expect(result.current.isSelected('tx-1')).toBe(false)
  })

  it('toggle adds an id when not selected', () => {
    const { result } = renderHook(() => useRowSelection())
    act(() => result.current.toggle('tx-1'))
    expect(result.current.isSelected('tx-1')).toBe(true)
    expect(result.current.selectedCount).toBe(1)
  })

  it('toggle removes an id when already selected', () => {
    const { result } = renderHook(() => useRowSelection())
    act(() => result.current.toggle('tx-1'))
    act(() => result.current.toggle('tx-1'))
    expect(result.current.isSelected('tx-1')).toBe(false)
    expect(result.current.selectedCount).toBe(0)
  })

  it('selectAll selects exactly the provided ids', () => {
    const { result } = renderHook(() => useRowSelection())
    act(() => result.current.selectAll(['a', 'b', 'c']))
    expect(result.current.selectedCount).toBe(3)
    expect(result.current.isSelected('a')).toBe(true)
    expect(result.current.isSelected('b')).toBe(true)
    expect(result.current.isSelected('c')).toBe(true)
    expect(result.current.isSelected('d')).toBe(false)
  })

  it('selectAll replaces prior selection', () => {
    const { result } = renderHook(() => useRowSelection())
    act(() => result.current.toggle('old'))
    act(() => result.current.selectAll(['new-1', 'new-2']))
    expect(result.current.isSelected('old')).toBe(false)
    expect(result.current.selectedCount).toBe(2)
  })

  it('clearSelection removes all ids', () => {
    const { result } = renderHook(() => useRowSelection())
    act(() => result.current.selectAll(['a', 'b']))
    act(() => result.current.clearSelection())
    expect(result.current.selectedCount).toBe(0)
    expect(result.current.isSelected('a')).toBe(false)
  })
})
