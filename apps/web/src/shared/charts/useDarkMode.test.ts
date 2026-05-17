import { describe, it, expect, beforeEach, afterEach } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { useDarkMode } from './useDarkMode'

describe('useDarkMode', () => {
  beforeEach(() => {
    document.documentElement.classList.remove('dark')
  })

  afterEach(() => {
    document.documentElement.classList.remove('dark')
  })

  it('returns false when no dark class on html', () => {
    const { result } = renderHook(() => useDarkMode())
    expect(result.current).toBe(false)
  })

  it('returns true when dark class is present on html at mount', () => {
    document.documentElement.classList.add('dark')
    const { result } = renderHook(() => useDarkMode())
    expect(result.current).toBe(true)
  })

  it('reacts to dark class being added via MutationObserver', async () => {
    const { result } = renderHook(() => useDarkMode())
    expect(result.current).toBe(false)

    await act(async () => {
      document.documentElement.classList.add('dark')
      await new Promise(resolve => setTimeout(resolve, 0))
    })
    expect(result.current).toBe(true)
  })

  it('reacts to dark class being removed', async () => {
    document.documentElement.classList.add('dark')
    const { result } = renderHook(() => useDarkMode())
    expect(result.current).toBe(true)

    await act(async () => {
      document.documentElement.classList.remove('dark')
      await new Promise(resolve => setTimeout(resolve, 0))
    })
    expect(result.current).toBe(false)
  })
})
