import { describe, it, expect, mock, beforeEach, afterEach } from 'bun:test'
import { renderHook } from '@testing-library/react'
import { mockUseGetRegions } from '@/test/mocks/regions-api'

const mockRegionsData = {
  regions: {
    iad: {
      code: 'iad',
      display_name: 'USA (Virginia)',
      country: 'USA',
      city: 'Virginia',
      flag: '🇺🇸',
    },
  },
  default_region: 'iad',
  server_region: 'iad',
}

// useRegions is loaded after the global mock (from regions-api preload) is in place
const { useRegions } = await import('./useRegions')

describe('useRegions', () => {
  beforeEach(() => {
    mockUseGetRegions.mockClear()
    mockUseGetRegions.mockImplementation(() => ({
      data: mockRegionsData,
      isLoading: false,
      error: null,
      refetch: mock(() => {}),
    }))
  })

  afterEach(() => {
    // Reset to valid default so other test files in the same worker are not affected
    mockUseGetRegions.mockImplementation(() => ({
      data: mockRegionsData,
      isLoading: false,
      error: null,
      refetch: mock(() => {}),
    }))
  })

  it('returns regions map from API', () => {
    const { result } = renderHook(() => useRegions())
    expect(result.current.regions).toEqual(mockRegionsData.regions)
  })

  it('returns defaultRegion from API', () => {
    const { result } = renderHook(() => useRegions())
    expect(result.current.defaultRegion).toBe('iad')
  })

  it('returns loading: false when data is loaded', () => {
    const { result } = renderHook(() => useRegions())
    expect(result.current.loading).toBe(false)
  })

  it('returns loading: true while fetching', () => {
    mockUseGetRegions.mockImplementation(() => ({
      data: undefined,
      isLoading: true,
      error: null,
      refetch: mock(() => {}),
    }))
    const { result } = renderHook(() => useRegions())
    expect(result.current.loading).toBe(true)
    expect(result.current.regions).toBeNull()
  })

  it('getRegionDisplay returns display name for known region', () => {
    const { result } = renderHook(() => useRegions())
    expect(result.current.getRegionDisplay('iad')).toBe('USA (Virginia)')
  })

  it('getRegionDisplay returns code as fallback for unknown region', () => {
    const { result } = renderHook(() => useRegions())
    expect(result.current.getRegionDisplay('unknown')).toBe('unknown')
  })

  it('getRegionWithFlag returns flag + display name', () => {
    const { result } = renderHook(() => useRegions())
    expect(result.current.getRegionWithFlag('iad')).toBe('🇺🇸 USA (Virginia)')
  })

  it('getRegionWithFlag returns code as fallback for unknown region', () => {
    const { result } = renderHook(() => useRegions())
    expect(result.current.getRegionWithFlag('lhr')).toBe('lhr')
  })

  it('returns null regions when data is not available', () => {
    mockUseGetRegions.mockImplementation(() => ({
      data: undefined,
      isLoading: false,
      error: null,
      refetch: mock(() => {}),
    }))
    const { result } = renderHook(() => useRegions())
    expect(result.current.regions).toBeNull()
  })
})
