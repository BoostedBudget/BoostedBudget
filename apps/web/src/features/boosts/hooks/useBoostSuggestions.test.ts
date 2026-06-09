/**
 * useBoostSuggestions Hook Tests
 * Issue #1267: Boosts page — per-tab transfer suggestion fetcher
 *
 * TDD RED: Tests written before implementation.
 */

import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { renderHook } from '@testing-library/react'
import type { TransferSuggestionListResponse } from '@boostedbudget/api-client/models'
import { mockUseGetTransferSuggestions } from '@/test/mocks/transfer-suggestions-api'
import { createQueryWrapper } from '@/test/utils/queryTestUtils'
import { useBoostSuggestions } from './useBoostSuggestions'

describe('useBoostSuggestions', () => {
  beforeEach(() => {
    mockUseGetTransferSuggestions.mockClear()
    mockUseGetTransferSuggestions.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
    })
  })

  it('calls useGetTransferSuggestions with the provided status, limit, and offset', () => {
    renderHook(() => useBoostSuggestions({ status: 'pending', limit: 20, offset: 0 }), {
      wrapper: createQueryWrapper(),
    })

    expect(mockUseGetTransferSuggestions).toHaveBeenCalledTimes(1)
    const callArgs = mockUseGetTransferSuggestions.mock.calls[0][0]
    expect(callArgs).toMatchObject({ status: 'pending', limit: 20, offset: 0 })
  })

  it('passes multi-status CSV string to useGetTransferSuggestions', () => {
    renderHook(() => useBoostSuggestions({ status: 'completed,rejected,expired', limit: 25, offset: 50 }), {
      wrapper: createQueryWrapper(),
    })

    const callArgs = mockUseGetTransferSuggestions.mock.calls[0][0]
    expect(callArgs).toMatchObject({ status: 'completed,rejected,expired', limit: 25, offset: 50 })
  })

  it('returns items from the envelope response', () => {
    const mockData: TransferSuggestionListResponse = {
      items: [
        { id: 'ts-1', status: 'pending', amount: 100, currency_code: 'USD', source: 'rule', user_id: 'u1', created_at: '2025-01-01', updated_at: '2025-01-01' },
      ],
      total: 1,
      limit: 20,
      offset: 0,
    }
    mockUseGetTransferSuggestions.mockReturnValue({ data: mockData, isLoading: false, error: null })

    const { result } = renderHook(() => useBoostSuggestions({ status: 'pending', limit: 20, offset: 0 }), {
      wrapper: createQueryWrapper(),
    })

    expect(result.current.items).toHaveLength(1)
    expect(result.current.items[0].id).toBe('ts-1')
  })

  it('returns total from the envelope response', () => {
    const mockData: TransferSuggestionListResponse = {
      items: [],
      total: 42,
      limit: 20,
      offset: 0,
    }
    mockUseGetTransferSuggestions.mockReturnValue({ data: mockData, isLoading: false, error: null })

    const { result } = renderHook(() => useBoostSuggestions({ status: 'pending', limit: 20, offset: 0 }), {
      wrapper: createQueryWrapper(),
    })

    expect(result.current.total).toBe(42)
  })

  it('returns empty items array when data is null or undefined', () => {
    mockUseGetTransferSuggestions.mockReturnValue({ data: undefined, isLoading: false, error: null })

    const { result } = renderHook(() => useBoostSuggestions({ status: 'pending', limit: 20, offset: 0 }), {
      wrapper: createQueryWrapper(),
    })

    expect(result.current.items).toEqual([])
    expect(result.current.total).toBe(0)
  })

  it('returns null items as empty array', () => {
    const mockData: TransferSuggestionListResponse = { items: null, total: 0, limit: 20, offset: 0 }
    mockUseGetTransferSuggestions.mockReturnValue({ data: mockData, isLoading: false, error: null })

    const { result } = renderHook(() => useBoostSuggestions({ status: 'pending', limit: 20, offset: 0 }), {
      wrapper: createQueryWrapper(),
    })

    expect(result.current.items).toEqual([])
  })

  it('propagates isLoading state', () => {
    mockUseGetTransferSuggestions.mockReturnValue({ data: undefined, isLoading: true, error: null })

    const { result } = renderHook(() => useBoostSuggestions({ status: 'pending', limit: 20, offset: 0 }), {
      wrapper: createQueryWrapper(),
    })

    expect(result.current.isLoading).toBe(true)
  })

  it('propagates error state', () => {
    const err = new Error('network error')
    mockUseGetTransferSuggestions.mockReturnValue({ data: undefined, isLoading: false, error: err })

    const { result } = renderHook(() => useBoostSuggestions({ status: 'pending', limit: 20, offset: 0 }), {
      wrapper: createQueryWrapper(),
    })

    expect(result.current.error).toBe(err)
  })

  // R7-2: refetch passthrough
  it('passes through refetch from the underlying query', () => {
    const mockRefetch = mock(() => Promise.resolve({ data: undefined }))
    mockUseGetTransferSuggestions.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
      refetch: mockRefetch,
    })

    const { result } = renderHook(() => useBoostSuggestions({ status: 'pending', limit: 20, offset: 0 }), {
      wrapper: createQueryWrapper(),
    })

    expect(result.current.refetch).toBe(mockRefetch)
  })
})
