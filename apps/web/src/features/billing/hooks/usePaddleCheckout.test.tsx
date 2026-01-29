import { describe, it, expect, beforeEach } from 'bun:test'
import React from 'react'
import { renderHook } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { usePaddleCheckout } from './usePaddleCheckout'
import {
  mockUseCreatePaddleCheckout,
  mockCreatePaddleCheckoutMutateAsync,
} from '@/test/mocks/subscription-api'
import { mockPaddleCheckoutResponse } from '@/test/fixtures/paddleFixtures'

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  })
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

describe('usePaddleCheckout', () => {
  beforeEach(() => {
    mockUseCreatePaddleCheckout.mockClear()
    mockCreatePaddleCheckoutMutateAsync.mockClear()
  })

  describe('Initialization', () => {
    it('should return correct initial state', () => {
      mockUseCreatePaddleCheckout.mockReturnValue({
        mutateAsync: mockCreatePaddleCheckoutMutateAsync,
        isPending: false,
        isSuccess: false,
        isError: false,
        error: null,
        reset: () => {},
      })

      const { result } = renderHook(() => usePaddleCheckout(), {
        wrapper: createWrapper(),
      })

      expect(result.current.isLoading).toBe(false)
      expect(result.current.isSuccess).toBe(false)
      expect(result.current.isError).toBe(false)
      expect(result.current.error).toBeNull()
      expect(typeof result.current.checkout).toBe('function')
      expect(typeof result.current.reset).toBe('function')
    })

    it('should reflect loading state from mutation', () => {
      mockUseCreatePaddleCheckout.mockReturnValue({
        mutateAsync: mockCreatePaddleCheckoutMutateAsync,
        isPending: true,
        isSuccess: false,
        isError: false,
        error: null,
        reset: () => {},
      })

      const { result } = renderHook(() => usePaddleCheckout(), {
        wrapper: createWrapper(),
      })

      expect(result.current.isLoading).toBe(true)
    })
  })

  describe('Checkout Function', () => {
    it('should call mutateAsync with correct payload for monthly billing', async () => {
      mockCreatePaddleCheckoutMutateAsync.mockResolvedValue(mockPaddleCheckoutResponse)
      mockUseCreatePaddleCheckout.mockReturnValue({
        mutateAsync: mockCreatePaddleCheckoutMutateAsync,
        isPending: false,
        isSuccess: false,
        isError: false,
        error: null,
        reset: () => {},
      })

      const { result } = renderHook(() => usePaddleCheckout(), {
        wrapper: createWrapper(),
      })

      await result.current.checkout({ tier: 'premium', billingInterval: 'month' })

      expect(mockCreatePaddleCheckoutMutateAsync).toHaveBeenCalledWith({
        data: {
          tier: 'premium',
          billing_interval: 'month',
        },
      })
    })

    it('should call mutateAsync with correct payload for yearly billing', async () => {
      mockCreatePaddleCheckoutMutateAsync.mockResolvedValue(mockPaddleCheckoutResponse)
      mockUseCreatePaddleCheckout.mockReturnValue({
        mutateAsync: mockCreatePaddleCheckoutMutateAsync,
        isPending: false,
        isSuccess: false,
        isError: false,
        error: null,
        reset: () => {},
      })

      const { result } = renderHook(() => usePaddleCheckout(), {
        wrapper: createWrapper(),
      })

      await result.current.checkout({ tier: 'household_2', billingInterval: 'year' })

      expect(mockCreatePaddleCheckoutMutateAsync).toHaveBeenCalledWith({
        data: {
          tier: 'household_2',
          billing_interval: 'year',
        },
      })
    })

    it('should return checkout response on success', async () => {
      mockCreatePaddleCheckoutMutateAsync.mockResolvedValue(mockPaddleCheckoutResponse)
      mockUseCreatePaddleCheckout.mockReturnValue({
        mutateAsync: mockCreatePaddleCheckoutMutateAsync,
        isPending: false,
        isSuccess: false,
        isError: false,
        error: null,
        reset: () => {},
      })

      const { result } = renderHook(() => usePaddleCheckout(), {
        wrapper: createWrapper(),
      })

      const response = await result.current.checkout({ tier: 'premium', billingInterval: 'month' })

      expect(response).toEqual(mockPaddleCheckoutResponse)
      expect(response.checkout_url).toContain('checkout.paddle.com')
      expect(response.transaction_id).toBeTruthy()
    })
  })

  describe('Error Normalization', () => {
    it('should normalize 401 error to user-friendly message', () => {
      mockUseCreatePaddleCheckout.mockReturnValue({
        mutateAsync: mockCreatePaddleCheckoutMutateAsync,
        isPending: false,
        isSuccess: false,
        isError: true,
        error: { status: 401, message: 'Unauthorized' },
        reset: () => {},
      })

      const { result } = renderHook(() => usePaddleCheckout(), {
        wrapper: createWrapper(),
      })

      expect(result.current.error).toEqual({
        status: 401,
        message: 'Please sign in to subscribe',
      })
    })

    it('should normalize 403 error to user-friendly message', () => {
      mockUseCreatePaddleCheckout.mockReturnValue({
        mutateAsync: mockCreatePaddleCheckoutMutateAsync,
        isPending: false,
        isSuccess: false,
        isError: true,
        error: { status: 403, message: 'Forbidden' },
        reset: () => {},
      })

      const { result } = renderHook(() => usePaddleCheckout(), {
        wrapper: createWrapper(),
      })

      expect(result.current.error).toEqual({
        status: 403,
        message: 'You do not have permission to subscribe',
      })
    })

    it('should normalize 400 error to user-friendly message', () => {
      mockUseCreatePaddleCheckout.mockReturnValue({
        mutateAsync: mockCreatePaddleCheckoutMutateAsync,
        isPending: false,
        isSuccess: false,
        isError: true,
        error: { status: 400, message: 'Bad Request' },
        reset: () => {},
      })

      const { result } = renderHook(() => usePaddleCheckout(), {
        wrapper: createWrapper(),
      })

      expect(result.current.error).toEqual({
        status: 400,
        message: 'Invalid subscription tier selected',
      })
    })

    it('should normalize 500 error to user-friendly message', () => {
      mockUseCreatePaddleCheckout.mockReturnValue({
        mutateAsync: mockCreatePaddleCheckoutMutateAsync,
        isPending: false,
        isSuccess: false,
        isError: true,
        error: { status: 500, message: 'Internal Server Error' },
        reset: () => {},
      })

      const { result } = renderHook(() => usePaddleCheckout(), {
        wrapper: createWrapper(),
      })

      expect(result.current.error).toEqual({
        status: 500,
        message: 'Payment system error. Please try again later.',
      })
    })

    it('should normalize 503 error to user-friendly message', () => {
      mockUseCreatePaddleCheckout.mockReturnValue({
        mutateAsync: mockCreatePaddleCheckoutMutateAsync,
        isPending: false,
        isSuccess: false,
        isError: true,
        error: { status: 503, message: 'Service Unavailable' },
        reset: () => {},
      })

      const { result } = renderHook(() => usePaddleCheckout(), {
        wrapper: createWrapper(),
      })

      expect(result.current.error).toEqual({
        status: 503,
        message: 'Payment service temporarily unavailable',
      })
    })

    it('should normalize network error to user-friendly message', () => {
      mockUseCreatePaddleCheckout.mockReturnValue({
        mutateAsync: mockCreatePaddleCheckoutMutateAsync,
        isPending: false,
        isSuccess: false,
        isError: true,
        error: { message: 'Network request failed' },
        reset: () => {},
      })

      const { result } = renderHook(() => usePaddleCheckout(), {
        wrapper: createWrapper(),
      })

      expect(result.current.error).toEqual({
        status: 0,
        message: 'Network error. Please check your connection.',
      })
    })

    it('should handle error with response.status', () => {
      mockUseCreatePaddleCheckout.mockReturnValue({
        mutateAsync: mockCreatePaddleCheckoutMutateAsync,
        isPending: false,
        isSuccess: false,
        isError: true,
        error: { response: { status: 401 }, message: 'Auth error' },
        reset: () => {},
      })

      const { result } = renderHook(() => usePaddleCheckout(), {
        wrapper: createWrapper(),
      })

      expect(result.current.error?.status).toBe(401)
      expect(result.current.error?.message).toBe('Please sign in to subscribe')
    })

    it('should return null error when no error exists', () => {
      mockUseCreatePaddleCheckout.mockReturnValue({
        mutateAsync: mockCreatePaddleCheckoutMutateAsync,
        isPending: false,
        isSuccess: true,
        isError: false,
        error: null,
        reset: () => {},
      })

      const { result } = renderHook(() => usePaddleCheckout(), {
        wrapper: createWrapper(),
      })

      expect(result.current.error).toBeNull()
    })

    it('should provide generic error message for unknown errors', () => {
      mockUseCreatePaddleCheckout.mockReturnValue({
        mutateAsync: mockCreatePaddleCheckoutMutateAsync,
        isPending: false,
        isSuccess: false,
        isError: true,
        error: { status: 999, message: '' },
        reset: () => {},
      })

      const { result } = renderHook(() => usePaddleCheckout(), {
        wrapper: createWrapper(),
      })

      expect(result.current.error?.message).toContain('Failed to start checkout')
    })
  })

  describe('Reset Function', () => {
    it('should expose reset function from mutation', () => {
      const mockReset = () => {}
      mockUseCreatePaddleCheckout.mockReturnValue({
        mutateAsync: mockCreatePaddleCheckoutMutateAsync,
        isPending: false,
        isSuccess: false,
        isError: false,
        error: null,
        reset: mockReset,
      })

      const { result } = renderHook(() => usePaddleCheckout(), {
        wrapper: createWrapper(),
      })

      expect(result.current.reset).toBe(mockReset)
    })
  })
})
