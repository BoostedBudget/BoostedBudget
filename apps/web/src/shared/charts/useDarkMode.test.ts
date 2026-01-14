/**
 * Dark Mode Hook Tests
 * Issue #445: Recharts to Plotly Migration
 */

import { describe, it, expect, mock, beforeEach, afterEach } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { useDarkMode } from './useDarkMode'

describe('useDarkMode', () => {
  let matchMediaMock: {
    matches: boolean
    addEventListener: ReturnType<typeof mock>
    removeEventListener: ReturnType<typeof mock>
  }
  const originalMatchMedia = window.matchMedia

  beforeEach(() => {
    matchMediaMock = {
      matches: false,
      addEventListener: mock(() => {}),
      removeEventListener: mock(() => {}),
    }
    // @ts-expect-error - mocking matchMedia
    window.matchMedia = mock(() => matchMediaMock)
  })

  afterEach(() => {
    window.matchMedia = originalMatchMedia
  })

  it('returns false when system prefers light mode', () => {
    matchMediaMock.matches = false
    const { result } = renderHook(() => useDarkMode())
    expect(result.current).toBe(false)
  })

  it('returns true when system prefers dark mode', () => {
    matchMediaMock.matches = true
    const { result } = renderHook(() => useDarkMode())
    expect(result.current).toBe(true)
  })

  it('queries the correct media feature', () => {
    renderHook(() => useDarkMode())
    expect(window.matchMedia).toHaveBeenCalledWith('(prefers-color-scheme: dark)')
  })

  it('adds event listener on mount', () => {
    renderHook(() => useDarkMode())
    expect(matchMediaMock.addEventListener).toHaveBeenCalledWith(
      'change',
      expect.any(Function)
    )
  })

  it('removes event listener on unmount', () => {
    const { unmount } = renderHook(() => useDarkMode())
    unmount()
    expect(matchMediaMock.removeEventListener).toHaveBeenCalledWith(
      'change',
      expect.any(Function)
    )
  })

  it('updates when preference changes', () => {
    matchMediaMock.matches = false
    const { result } = renderHook(() => useDarkMode())
    expect(result.current).toBe(false)

    act(() => {
      const changeHandler = matchMediaMock.addEventListener.mock.calls[0][1]
      changeHandler({ matches: true } as MediaQueryListEvent)
    })

    expect(result.current).toBe(true)
  })

  it('updates when preference changes back', () => {
    matchMediaMock.matches = true
    const { result } = renderHook(() => useDarkMode())
    expect(result.current).toBe(true)

    act(() => {
      const changeHandler = matchMediaMock.addEventListener.mock.calls[0][1]
      changeHandler({ matches: false } as MediaQueryListEvent)
    })

    expect(result.current).toBe(false)
  })
})
