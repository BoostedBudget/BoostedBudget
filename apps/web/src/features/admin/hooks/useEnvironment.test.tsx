/**
 * useEnvironment Hook Tests
 * Issue #642: Admin feature foundation
 */

import { describe, it, expect, beforeEach, afterEach } from 'bun:test'
import { renderHook } from '@testing-library/react'
import { useEnvironment } from './useEnvironment'

describe('useEnvironment', () => {
  const originalLocation = window.location

  beforeEach(() => {
    // Mock window.location
    Object.defineProperty(window, 'location', {
      value: { hostname: 'localhost' },
      writable: true,
    })
  })

  afterEach(() => {
    // Restore original location
    Object.defineProperty(window, 'location', {
      value: originalLocation,
      writable: true,
    })
  })

  it('should detect development environment for localhost', () => {
    Object.defineProperty(window, 'location', {
      value: { hostname: 'localhost' },
      writable: true,
    })

    const { result } = renderHook(() => useEnvironment())

    expect(result.current.environment).toBe('development')
    expect(result.current.isDevelopment).toBe(true)
    expect(result.current.isQA).toBe(false)
    expect(result.current.isStaging).toBe(false)
    expect(result.current.isProduction).toBe(false)
  })

  it('should detect development environment for 127.0.0.1', () => {
    Object.defineProperty(window, 'location', {
      value: { hostname: '127.0.0.1' },
      writable: true,
    })

    const { result } = renderHook(() => useEnvironment())

    expect(result.current.environment).toBe('development')
    expect(result.current.isDevelopment).toBe(true)
  })

  it('should detect QA environment for qa subdomain', () => {
    Object.defineProperty(window, 'location', {
      value: { hostname: 'qa.boostedbudget.com' },
      writable: true,
    })

    const { result } = renderHook(() => useEnvironment())

    expect(result.current.environment).toBe('qa')
    expect(result.current.isQA).toBe(true)
    expect(result.current.isDevelopment).toBe(false)
    expect(result.current.isStaging).toBe(false)
    expect(result.current.isProduction).toBe(false)
  })

  it('should detect staging environment for staging subdomain', () => {
    Object.defineProperty(window, 'location', {
      value: { hostname: 'staging.boostedbudget.com' },
      writable: true,
    })

    const { result } = renderHook(() => useEnvironment())

    expect(result.current.environment).toBe('staging')
    expect(result.current.isStaging).toBe(true)
    expect(result.current.isDevelopment).toBe(false)
    expect(result.current.isQA).toBe(false)
    expect(result.current.isProduction).toBe(false)
  })

  it('should detect production environment for production domain', () => {
    Object.defineProperty(window, 'location', {
      value: { hostname: 'app.boostedbudget.com' },
      writable: true,
    })

    const { result } = renderHook(() => useEnvironment())

    expect(result.current.environment).toBe('production')
    expect(result.current.isProduction).toBe(true)
    expect(result.current.isDevelopment).toBe(false)
    expect(result.current.isQA).toBe(false)
    expect(result.current.isStaging).toBe(false)
  })

  it('should detect production for www subdomain', () => {
    Object.defineProperty(window, 'location', {
      value: { hostname: 'www.boostedbudget.com' },
      writable: true,
    })

    const { result } = renderHook(() => useEnvironment())

    expect(result.current.environment).toBe('production')
    expect(result.current.isProduction).toBe(true)
  })

  it('should detect production for apex domain', () => {
    Object.defineProperty(window, 'location', {
      value: { hostname: 'boostedbudget.com' },
      writable: true,
    })

    const { result } = renderHook(() => useEnvironment())

    expect(result.current.environment).toBe('production')
    expect(result.current.isProduction).toBe(true)
  })

  it('should detect development for .local domains', () => {
    Object.defineProperty(window, 'location', {
      value: { hostname: 'boostedbudget.local' },
      writable: true,
    })

    const { result } = renderHook(() => useEnvironment())

    expect(result.current.environment).toBe('development')
    expect(result.current.isDevelopment).toBe(true)
  })
})
