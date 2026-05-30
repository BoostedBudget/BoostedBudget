/**
 * useReviewMutations tests (Issue #1203).
 */
import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import {
  mockSetTransactionReviewedMutateAsync,
  mockBulkSetTransactionReviewedMutateAsync,
} from '@/test/mocks/transactions-api'
import { mockPosthogCapture } from '@/test/mocks/posthog'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import { useReviewMutations } from './useReviewMutations'

function wrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  }
}

describe('useReviewMutations', () => {
  beforeEach(() => {
    mockSetTransactionReviewedMutateAsync.mockClear()
    mockBulkSetTransactionReviewedMutateAsync.mockClear()
    mockPosthogCapture.mockClear()
  })

  it('setReviewed sends {id,data} and tracks TRANSACTION_REVIEWED', async () => {
    const { result } = renderHook(() => useReviewMutations(), { wrapper: wrapper() })
    await result.current.setReviewed('tx-1', true)

    expect(mockSetTransactionReviewedMutateAsync).toHaveBeenCalledWith({ id: 'tx-1', data: { reviewed: true } })
    expect(mockPosthogCapture).toHaveBeenCalledWith(
      ANALYTICS_EVENTS.TRANSACTION_REVIEWED,
      expect.objectContaining({ reviewed: true, count: 1 })
    )
  })

  it('bulkSetReviewed sends transaction_ids + reviewed and tracks the count', async () => {
    const { result } = renderHook(() => useReviewMutations(), { wrapper: wrapper() })
    await result.current.bulkSetReviewed(['a', 'b'], false)

    expect(mockBulkSetTransactionReviewedMutateAsync).toHaveBeenCalledWith({
      data: { transaction_ids: ['a', 'b'], reviewed: false },
    })
    expect(mockPosthogCapture).toHaveBeenCalledWith(
      ANALYTICS_EVENTS.TRANSACTION_REVIEWED,
      expect.objectContaining({ reviewed: false, count: 2 })
    )
  })

  it('bulkSetReviewed is a no-op for an empty id list', async () => {
    const { result } = renderHook(() => useReviewMutations(), { wrapper: wrapper() })
    await result.current.bulkSetReviewed([], true)

    expect(mockBulkSetTransactionReviewedMutateAsync).not.toHaveBeenCalled()
    expect(mockPosthogCapture).not.toHaveBeenCalled()
  })
})
