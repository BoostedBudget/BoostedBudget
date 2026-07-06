import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { useSessionDismiss } from './useSessionDismiss'

describe('useSessionDismiss', () => {
  beforeEach(() => {
    // Reset sessionStorage before each test
    sessionStorage.clear()
  })

  it('isDismissed is false initially', () => {
    const { result } = renderHook(() => useSessionDismiss('user-1', 'past_due'))
    expect(result.current.isDismissed).toBe(false)
  })

  it('dismiss() sets isDismissed to true', () => {
    const { result } = renderHook(() => useSessionDismiss('user-1', 'past_due'))
    expect(result.current.isDismissed).toBe(false)
    act(() => {
      result.current.dismiss()
    })
    expect(result.current.isDismissed).toBe(true)
  })

  it('key includes userId and status', () => {
    const { result } = renderHook(() => useSessionDismiss('user-abc', 'past_due'))
    act(() => {
      result.current.dismiss()
    })
    const storedValue = sessionStorage.getItem('bb_sub_banner_dismissed_user-abc_past_due')
    expect(storedValue).toBe('true')
  })

  it('different userId produces separate dismiss state', () => {
    const { result: resultA } = renderHook(() => useSessionDismiss('user-1', 'past_due'))
    const { result: resultB } = renderHook(() => useSessionDismiss('user-2', 'past_due'))
    act(() => {
      resultA.current.dismiss()
    })
    expect(resultA.current.isDismissed).toBe(true)
    expect(resultB.current.isDismissed).toBe(false)
  })

  it('different status produces separate dismiss state', () => {
    const { result: resultPastDue } = renderHook(() => useSessionDismiss('user-1', 'past_due'))
    const { result: resultCancelled } = renderHook(() => useSessionDismiss('user-1', 'cancelled'))
    act(() => {
      resultPastDue.current.dismiss()
    })
    expect(resultPastDue.current.isDismissed).toBe(true)
    expect(resultCancelled.current.isDismissed).toBe(false)
  })

  it('reads pre-existing sessionStorage value on mount', () => {
    sessionStorage.setItem('bb_sub_banner_dismissed_user-1_past_due', 'true')
    const { result } = renderHook(() => useSessionDismiss('user-1', 'past_due'))
    expect(result.current.isDismissed).toBe(true)
  })
})
