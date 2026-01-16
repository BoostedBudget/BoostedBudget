/**
 * useAdminMode Hook Tests
 * Issue #642: Admin feature foundation
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { ADMIN_MODE_STORAGE_KEY } from '../constants'
import { useAdminMode } from './useAdminMode'

describe('useAdminMode', () => {
  beforeEach(() => {
    // Clear localStorage before each test
    localStorage.clear()
  })

  it('should return adminModeEnabled as false by default', () => {
    const { result } = renderHook(() => useAdminMode())

    expect(result.current.adminModeEnabled).toBe(false)
  })

  it('should read initial state from localStorage', () => {
    localStorage.setItem(ADMIN_MODE_STORAGE_KEY, 'true')

    const { result } = renderHook(() => useAdminMode())

    expect(result.current.adminModeEnabled).toBe(true)
  })

  it('should toggle admin mode from false to true', () => {
    const { result } = renderHook(() => useAdminMode())

    expect(result.current.adminModeEnabled).toBe(false)

    act(() => {
      result.current.toggleAdminMode()
    })

    expect(result.current.adminModeEnabled).toBe(true)
    expect(localStorage.getItem(ADMIN_MODE_STORAGE_KEY)).toBe('true')
  })

  it('should toggle admin mode from true to false', () => {
    localStorage.setItem(ADMIN_MODE_STORAGE_KEY, 'true')

    const { result } = renderHook(() => useAdminMode())

    expect(result.current.adminModeEnabled).toBe(true)

    act(() => {
      result.current.toggleAdminMode()
    })

    expect(result.current.adminModeEnabled).toBe(false)
    expect(localStorage.getItem(ADMIN_MODE_STORAGE_KEY)).toBe('false')
  })

  it('should set admin mode to specific value', () => {
    const { result } = renderHook(() => useAdminMode())

    act(() => {
      result.current.setAdminMode(true)
    })

    expect(result.current.adminModeEnabled).toBe(true)
    expect(localStorage.getItem(ADMIN_MODE_STORAGE_KEY)).toBe('true')

    act(() => {
      result.current.setAdminMode(false)
    })

    expect(result.current.adminModeEnabled).toBe(false)
    expect(localStorage.getItem(ADMIN_MODE_STORAGE_KEY)).toBe('false')
  })

  it('should provide all expected functions', () => {
    const { result } = renderHook(() => useAdminMode())

    expect(typeof result.current.toggleAdminMode).toBe('function')
    expect(typeof result.current.setAdminMode).toBe('function')
    expect(typeof result.current.adminModeEnabled).toBe('boolean')
  })

  it('should handle invalid localStorage value gracefully', () => {
    localStorage.setItem(ADMIN_MODE_STORAGE_KEY, 'invalid')

    const { result } = renderHook(() => useAdminMode())

    // Should default to false for any value other than 'true'
    expect(result.current.adminModeEnabled).toBe(false)
  })
})
