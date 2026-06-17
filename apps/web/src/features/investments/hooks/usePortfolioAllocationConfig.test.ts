/**
 * usePortfolioAllocationConfig Hook Tests
 *
 * Issue #1296: localStorage-persisted allocation config for portfolio view.
 * Tests default values, setConfig, localStorage persistence, and unauthenticated state.
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { usePortfolioAllocationConfig } from './usePortfolioAllocationConfig'
import type { AllocationConfig } from './usePortfolioAllocationConfig'
import { mockUseUser, mockClerkUser } from '@/test/mocks/clerk'

// useUser is provided by the preloaded clerk.tsx mock.
// Tests that need an authenticated user must call mockUseUser.mockReturnValue()
// exactly like useTablePreferences.test.ts does.

const USER_ID = mockClerkUser.id // 'user_123456'
const STORAGE_KEY = `bb:portfolio-alloc:${USER_ID}`

describe('usePortfolioAllocationConfig', () => {
  beforeEach(() => {
    localStorage.clear()
    mockUseUser.mockReturnValue({ user: mockClerkUser, isLoaded: true, isSignedIn: true })
  })

  it('returns defaults when no stored config exists', () => {
    const { result } = renderHook(() => usePortfolioAllocationConfig())
    expect(result.current.config.include).toEqual([])
    expect(result.current.config.group_by).toBe('security_type')
    expect(result.current.config.basis).toBe('assets')
  })

  it('setConfig updates the config state', () => {
    const { result } = renderHook(() => usePortfolioAllocationConfig())

    act(() => {
      result.current.setConfig({ include: ['cash'], group_by: 'asset_class', basis: 'net' })
    })

    expect(result.current.config.include).toEqual(['cash'])
    expect(result.current.config.group_by).toBe('asset_class')
    expect(result.current.config.basis).toBe('net')
  })

  it('persists config to localStorage on setConfig', () => {
    const { result } = renderHook(() => usePortfolioAllocationConfig())

    act(() => {
      result.current.setConfig({ include: ['cash', 'liabilities'], group_by: 'account', basis: 'net' })
    })

    const raw = localStorage.getItem(STORAGE_KEY)
    expect(raw).not.toBeNull()

    const parsed = JSON.parse(raw!) as AllocationConfig
    expect(parsed.include).toEqual(['cash', 'liabilities'])
    expect(parsed.group_by).toBe('account')
    expect(parsed.basis).toBe('net')
  })

  it('loads previously persisted config on mount', () => {
    const stored: AllocationConfig = { include: ['cash'], group_by: 'asset_class', basis: 'assets' }
    localStorage.setItem(STORAGE_KEY, JSON.stringify(stored))

    const { result } = renderHook(() => usePortfolioAllocationConfig())

    expect(result.current.config.include).toEqual(['cash'])
    expect(result.current.config.group_by).toBe('asset_class')
    expect(result.current.config.basis).toBe('assets')
  })

  it('falls back to defaults when stored JSON is malformed', () => {
    localStorage.setItem(STORAGE_KEY, '{ invalid json')

    const { result } = renderHook(() => usePortfolioAllocationConfig())

    expect(result.current.config.include).toEqual([])
    expect(result.current.config.group_by).toBe('security_type')
    expect(result.current.config.basis).toBe('assets')
  })

  it('supports partial setConfig via functional updater pattern', () => {
    const { result } = renderHook(() => usePortfolioAllocationConfig())

    act(() => {
      result.current.setConfig({ include: ['cash'], group_by: 'security_type', basis: 'assets' })
    })

    act(() => {
      // Update only basis
      result.current.setConfig({ ...result.current.config, basis: 'net' })
    })

    expect(result.current.config.include).toEqual(['cash'])
    expect(result.current.config.basis).toBe('net')
  })
})
