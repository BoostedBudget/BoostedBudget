/**
 * useBoostSuggestionActions Hook Tests
 * Issue #1267: Boosts page — accept/reject/confirm-match/decline-match actions
 *
 * TDD: Tests written before implementation.
 * Analytics tests pattern mirrors NotificationAcceptReject.test.tsx (working):
 *   - mutate mock calls capturedOnSuccess inline when invoked
 *   - trigger via result.current.accept/reject/confirmMatch (not direct capturedOnSuccess call)
 */

import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { renderHook } from '@testing-library/react'
import {
  mockUseAcceptTransferSuggestion,
  mockUseRejectTransferSuggestion,
  mockUseConfirmTransferSuggestionMatch,
  mockUseDeclineTransferSuggestionMatch,
  mockAcceptTransferSuggestionMutateAsync,
  mockRejectTransferSuggestionMutateAsync,
  mockConfirmTransferSuggestionMatchMutateAsync,
  mockDeclineTransferSuggestionMatchMutate,
  getGetTransferSuggestionsQueryKey,
} from '@/test/mocks/transfer-suggestions-api'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { createQueryWrapper } from '@/test/utils/queryTestUtils'
import { useBoostSuggestionActions } from './useBoostSuggestionActions'

// Mirror NotificationAcceptReject.test.tsx: declare mock.module in the test file too
// to ensure it applies before the hook module is resolved in this file's scope.
mock.module('@/shared/hooks/useTrackEvent', () => ({
  useTrackEvent: () => mockTrackEvent,
}))

describe('useBoostSuggestionActions', () => {
  beforeEach(() => {
    mockUseAcceptTransferSuggestion.mockClear()
    mockUseRejectTransferSuggestion.mockClear()
    mockUseConfirmTransferSuggestionMatch.mockClear()
    mockUseDeclineTransferSuggestionMatch.mockClear()
    mockAcceptTransferSuggestionMutateAsync.mockClear()
    mockRejectTransferSuggestionMutateAsync.mockClear()
    mockConfirmTransferSuggestionMatchMutateAsync.mockClear()
    mockDeclineTransferSuggestionMatchMutate.mockClear()
    mockTrackEvent.mockClear()

    mockUseAcceptTransferSuggestion.mockReturnValue({
      mutate: mockAcceptTransferSuggestionMutateAsync,
      mutateAsync: mockAcceptTransferSuggestionMutateAsync,
      isPending: false,
    })
    mockUseRejectTransferSuggestion.mockReturnValue({
      mutate: mockRejectTransferSuggestionMutateAsync,
      mutateAsync: mockRejectTransferSuggestionMutateAsync,
      isPending: false,
    })
    mockUseConfirmTransferSuggestionMatch.mockReturnValue({
      mutate: mockConfirmTransferSuggestionMatchMutateAsync,
      mutateAsync: mockConfirmTransferSuggestionMatchMutateAsync,
      isPending: false,
    })
    mockUseDeclineTransferSuggestionMatch.mockReturnValue({
      mutate: mockDeclineTransferSuggestionMatchMutate,
      mutateAsync: mockDeclineTransferSuggestionMatchMutate,
      isPending: false,
    })
  })

  it('returns accept, reject, confirmMatch, declineMatch action functions', () => {
    const { result } = renderHook(() => useBoostSuggestionActions(), { wrapper: createQueryWrapper() })

    expect(typeof result.current.accept).toBe('function')
    expect(typeof result.current.reject).toBe('function')
    expect(typeof result.current.confirmMatch).toBe('function')
    expect(typeof result.current.declineMatch).toBe('function')
  })

  it('fires accept mutation with suggestion id', () => {
    const { result } = renderHook(() => useBoostSuggestionActions(), { wrapper: createQueryWrapper() })
    result.current.accept('ts-1')

    expect(mockAcceptTransferSuggestionMutateAsync).toHaveBeenCalledWith(
      expect.objectContaining({ id: 'ts-1' })
    )
  })

  it('fires reject mutation with suggestion id', () => {
    const { result } = renderHook(() => useBoostSuggestionActions(), { wrapper: createQueryWrapper() })
    result.current.reject('ts-2')

    expect(mockRejectTransferSuggestionMutateAsync).toHaveBeenCalledWith(
      expect.objectContaining({ id: 'ts-2' })
    )
  })

  it('fires confirmMatch mutation with suggestion id and actual amount', () => {
    const { result } = renderHook(() => useBoostSuggestionActions(), { wrapper: createQueryWrapper() })
    result.current.confirmMatch('ts-3', 500)

    expect(mockConfirmTransferSuggestionMatchMutateAsync).toHaveBeenCalledWith(
      expect.objectContaining({ id: 'ts-3', data: expect.objectContaining({ actual_amount: 500 }) })
    )
  })

  it('fires declineMatch mutation with suggestion id', () => {
    const { result } = renderHook(() => useBoostSuggestionActions(), { wrapper: createQueryWrapper() })
    result.current.declineMatch('ts-4')

    expect(mockDeclineTransferSuggestionMatchMutate).toHaveBeenCalledWith(
      expect.objectContaining({ id: 'ts-4' })
    )
  })

  // Analytics: mutate mock calls capturedOnSuccess inline (exact NotificationAcceptReject.test.tsx pattern).
  // Then trigger via result.current.accept/reject/confirmMatch — NOT by calling capturedOnSuccess directly,
  // which would run outside React context where queryClient.invalidateQueries can behave unexpectedly.

  it('onSuccess for accept fires TRANSFER_SUGGESTION_ACCEPTED with source boosts_page', () => {
    let capturedOnSuccess: ((data: { suggestion?: { amount?: number } }) => void) | undefined
    mockUseAcceptTransferSuggestion.mockImplementation(
      (opts?: { mutation?: { onSuccess?: (data: unknown) => void } }) => {
        capturedOnSuccess = opts?.mutation?.onSuccess as typeof capturedOnSuccess
        return {
          mutate: mock((_args: unknown) => {
            capturedOnSuccess?.({ suggestion: { amount: 300 } })
          }),
          mutateAsync: mockAcceptTransferSuggestionMutateAsync,
          isPending: false,
        }
      }
    )

    const { result } = renderHook(() => useBoostSuggestionActions(), { wrapper: createQueryWrapper() })
    result.current.accept('ts-analytics')

    expect(mockTrackEvent).toHaveBeenCalledWith(
      'transfer_suggestion_accepted',
      expect.objectContaining({ source: 'boosts_page' })
    )
  })

  it('onSuccess for reject fires TRANSFER_SUGGESTION_REJECTED with source boosts_page', () => {
    let capturedOnSuccess: (() => void) | undefined
    mockUseRejectTransferSuggestion.mockImplementation(
      (opts?: { mutation?: { onSuccess?: () => void } }) => {
        capturedOnSuccess = opts?.mutation?.onSuccess
        return {
          mutate: mock((_args: unknown) => {
            capturedOnSuccess?.()
          }),
          mutateAsync: mockRejectTransferSuggestionMutateAsync,
          isPending: false,
        }
      }
    )

    const { result } = renderHook(() => useBoostSuggestionActions(), { wrapper: createQueryWrapper() })
    result.current.reject('ts-analytics')

    expect(mockTrackEvent).toHaveBeenCalledWith(
      'transfer_suggestion_rejected',
      expect.objectContaining({ source: 'boosts_page' })
    )
  })

  it('onSuccess for confirmMatch fires TRANSFER_SUGGESTION_MATCH_CONFIRMED with source boosts_page', () => {
    let capturedOnSuccess: (() => void) | undefined
    mockUseConfirmTransferSuggestionMatch.mockImplementation(
      (opts?: { mutation?: { onSuccess?: () => void } }) => {
        capturedOnSuccess = opts?.mutation?.onSuccess
        return {
          mutate: mock((_args: unknown) => {
            capturedOnSuccess?.()
          }),
          mutateAsync: mockConfirmTransferSuggestionMatchMutateAsync,
          isPending: false,
        }
      }
    )

    const { result } = renderHook(() => useBoostSuggestionActions(), { wrapper: createQueryWrapper() })
    result.current.confirmMatch('ts-analytics', 500)

    expect(mockTrackEvent).toHaveBeenCalledWith(
      'transfer_suggestion_match_confirmed',
      expect.objectContaining({ source: 'boosts_page' })
    )
  })

  it('invalidation uses the base transfer-suggestions query key (key shape sanity)', () => {
    // Verify the base key returned by getGetTransferSuggestionsQueryKey() is a non-empty array.
    // The hook calls queryClient.invalidateQueries({ queryKey: getGetTransferSuggestionsQueryKey() })
    // so any tab's cached results are invalidated after an action (TanStack partial-match).
    const baseKey = getGetTransferSuggestionsQueryKey()
    expect(Array.isArray(baseKey)).toBe(true)
    expect(baseKey.length).toBeGreaterThan(0)
  })

  it('exposes isPending as true when accept mutation is pending', () => {
    mockUseAcceptTransferSuggestion.mockReturnValue({ mutate: mock(() => {}), isPending: true })

    const { result } = renderHook(() => useBoostSuggestionActions(), { wrapper: createQueryWrapper() })

    expect(result.current.isPending).toBe(true)
  })

  it('exposes isPending as true when confirmMatch mutation is pending', () => {
    mockUseConfirmTransferSuggestionMatch.mockReturnValue({ mutate: mock(() => {}), isPending: true })

    const { result } = renderHook(() => useBoostSuggestionActions(), { wrapper: createQueryWrapper() })

    expect(result.current.isPending).toBe(true)
  })

  it('exposes isPending as false when no mutation is pending', () => {
    const { result } = renderHook(() => useBoostSuggestionActions(), { wrapper: createQueryWrapper() })

    expect(result.current.isPending).toBe(false)
  })
})
