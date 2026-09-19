import { describe, it, expect, beforeEach, afterEach, mock, spyOn } from 'bun:test'
import { renderHook } from '@testing-library/react'
import { useRevenueCatCheckout } from './useRevenueCatCheckout'
import { mockUseAuth, createMockClerkAuth } from '@/test/mocks/clerk'
import { revenueCatCheckout } from '../revenueCatRuntime'

const mockStart = mock(async (_tier: string, _billingInterval: 'month' | 'year') => {})
const mockSetIdentity = mock((_userId: string | null) => {})
let restoreStart = () => {}
let restoreSetIdentity = () => {}

beforeEach(() => {
  mockUseAuth.mockReturnValue(createMockClerkAuth({ userId: 'user_hook', isSignedIn: true }))
  const startSpy = spyOn(revenueCatCheckout, 'start').mockImplementation(mockStart)
  const identitySpy = spyOn(revenueCatCheckout, 'setIdentity').mockImplementation(mockSetIdentity)
  restoreStart = () => { startSpy.mockRestore() }
  restoreSetIdentity = () => { identitySpy.mockRestore() }
  mockStart.mockClear()
  mockSetIdentity.mockClear()
})

afterEach(() => {
  restoreStart()
  restoreSetIdentity()
})

describe('useRevenueCatCheckout', () => {
  it('forwards the selected tier and billing interval to the checkout controller', () => {
    const { result } = renderHook(() => useRevenueCatCheckout())

    result.current.checkout('premium', 'year')

    expect(mockSetIdentity).toHaveBeenCalledWith('user_hook')
    expect(mockStart).toHaveBeenCalledWith('premium', 'year')
  })

  it('clears the checkout identity for a signed-out visitor', () => {
    mockUseAuth.mockReturnValue(createMockClerkAuth({ userId: null, isSignedIn: false }))
    const { result } = renderHook(() => useRevenueCatCheckout())

    result.current.checkout('premium', 'month')

    expect(mockSetIdentity).toHaveBeenCalledWith(null)
    expect(mockStart).toHaveBeenCalledWith('premium', 'month')
  })
})
