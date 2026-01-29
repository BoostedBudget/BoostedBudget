import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { PaddleCheckoutButton } from './PaddleCheckoutButton'
import {
  mockUseCreatePaddleCheckout,
  mockCreatePaddleCheckoutMutateAsync,
} from '@/test/mocks/subscription-api'
import { mockPaddleCheckoutResponse } from '@/test/fixtures/paddleFixtures'
import { toast } from 'sonner'

// Mock toast
mock.module('sonner', () => ({
  toast: {
    error: mock(() => {}),
    success: mock(() => {}),
  },
}))

const createTestQueryClient = () =>
  new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  })

const createWrapper = () => {
  const queryClient = createTestQueryClient()
  return ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
}

describe('PaddleCheckoutButton', () => {
  beforeEach(() => {
    mockUseCreatePaddleCheckout.mockClear()
    mockCreatePaddleCheckoutMutateAsync.mockClear()

    // Default mock return
    mockUseCreatePaddleCheckout.mockReturnValue({
      mutateAsync: mockCreatePaddleCheckoutMutateAsync,
      isPending: false,
      isSuccess: false,
      isError: false,
      error: null,
      reset: mock(() => {}),
    })

    // Mock Paddle.js global
    globalThis.window.Paddle = {
      Checkout: {
        open: mock(() => {}),
      },
    }
  })

  describe('Rendering', () => {
    it('should render button with "Subscribe" text', () => {
      const Wrapper = createWrapper()
      render(<PaddleCheckoutButton tier="premium" billingInterval="month" />, { wrapper: Wrapper })

      const button = screen.getByRole('button', { name: /subscribe/i })
      expect(button).toBeTruthy()
    })

    it('should be disabled when disabled prop is true', () => {
      const Wrapper = createWrapper()
      render(<PaddleCheckoutButton tier="premium" billingInterval="month" disabled={true} />, {
        wrapper: Wrapper,
      })

      const button = screen.getByRole('button')
      expect(button.disabled).toBe(true)
    })

    it('should show "Loading..." when processing', async () => {
      mockCreatePaddleCheckoutMutateAsync.mockImplementation(
        () =>
          new Promise((resolve) =>
            setTimeout(() => resolve(mockPaddleCheckoutResponse), 100)
          )
      )

      const Wrapper = createWrapper()
      render(<PaddleCheckoutButton tier="premium" billingInterval="month" />, { wrapper: Wrapper })

      const button = screen.getByRole('button')
      await userEvent.click(button)

      expect(screen.getByText(/loading/i)).toBeTruthy()
    })

    it('should be disabled when isPending is true', () => {
      mockUseCreatePaddleCheckout.mockReturnValue({
        mutateAsync: mockCreatePaddleCheckoutMutateAsync,
        isPending: true,
        isSuccess: false,
        isError: false,
        error: null,
        reset: mock(() => {}),
      })

      const Wrapper = createWrapper()
      render(<PaddleCheckoutButton tier="premium" billingInterval="month" />, { wrapper: Wrapper })

      const button = screen.getByRole('button')
      expect(button.disabled).toBe(true)
    })
  })

  describe('Click Handler', () => {
    it('should create checkout and open Paddle overlay on click', async () => {
      mockCreatePaddleCheckoutMutateAsync.mockResolvedValue(mockPaddleCheckoutResponse)

      const Wrapper = createWrapper()
      render(<PaddleCheckoutButton tier="premium" billingInterval="month" />, { wrapper: Wrapper })

      const button = screen.getByRole('button', { name: /subscribe/i })
      await userEvent.click(button)

      await waitFor(() => {
        expect(mockCreatePaddleCheckoutMutateAsync).toHaveBeenCalledWith({
          data: {
            tier: 'premium',
            billing_interval: 'month',
          },
        })
      })

      await waitFor(() => {
        expect(window.Paddle.Checkout.open).toHaveBeenCalledWith({
          settings: {
            checkoutURL: mockPaddleCheckoutResponse.checkout_url,
          },
        })
      })
    })

    it('should pass correct tier to API', async () => {
      mockCreatePaddleCheckoutMutateAsync.mockResolvedValue(mockPaddleCheckoutResponse)

      const Wrapper = createWrapper()
      render(<PaddleCheckoutButton tier="household_2" billingInterval="month" />, {
        wrapper: Wrapper,
      })

      const button = screen.getByRole('button')
      await userEvent.click(button)

      await waitFor(() => {
        expect(mockCreatePaddleCheckoutMutateAsync).toHaveBeenCalledWith({
          data: {
            tier: 'household_2',
            billing_interval: 'month',
          },
        })
      })
    })

    it('should pass monthly billing interval to API', async () => {
      mockCreatePaddleCheckoutMutateAsync.mockResolvedValue(mockPaddleCheckoutResponse)

      const Wrapper = createWrapper()
      render(<PaddleCheckoutButton tier="premium" billingInterval="month" />, { wrapper: Wrapper })

      const button = screen.getByRole('button')
      await userEvent.click(button)

      await waitFor(() => {
        expect(mockCreatePaddleCheckoutMutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({
            data: expect.objectContaining({
              billing_interval: 'month',
            }),
          })
        )
      })
    })

    it('should pass yearly billing interval to API', async () => {
      mockCreatePaddleCheckoutMutateAsync.mockResolvedValue(mockPaddleCheckoutResponse)

      const Wrapper = createWrapper()
      render(<PaddleCheckoutButton tier="premium" billingInterval="year" />, { wrapper: Wrapper })

      const button = screen.getByRole('button')
      await userEvent.click(button)

      await waitFor(() => {
        expect(mockCreatePaddleCheckoutMutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({
            data: expect.objectContaining({
              billing_interval: 'year',
            }),
          })
        )
      })
    })

    it('should call onSuccess callback after checkout creation', async () => {
      mockCreatePaddleCheckoutMutateAsync.mockResolvedValue(mockPaddleCheckoutResponse)

      const onSuccess = mock(() => {})
      const Wrapper = createWrapper()
      render(
        <PaddleCheckoutButton tier="premium" billingInterval="month" onSuccess={onSuccess} />,
        { wrapper: Wrapper }
      )

      const button = screen.getByRole('button')
      await userEvent.click(button)

      await waitFor(() => {
        expect(onSuccess).toHaveBeenCalled()
      })
    })
  })

  describe('Error Handling', () => {
    it('should show toast error for 403 status', async () => {
      mockCreatePaddleCheckoutMutateAsync.mockRejectedValue({ status: 403 })

      const Wrapper = createWrapper()
      render(<PaddleCheckoutButton tier="premium" billingInterval="month" />, { wrapper: Wrapper })

      const button = screen.getByRole('button')
      await userEvent.click(button)

      await waitFor(() => {
        expect(toast.error).toHaveBeenCalledWith('Please sign in to subscribe')
      })
    })

    it('should show toast error for 400 status', async () => {
      mockCreatePaddleCheckoutMutateAsync.mockRejectedValue({ status: 400 })

      const Wrapper = createWrapper()
      render(<PaddleCheckoutButton tier="premium" billingInterval="month" />, { wrapper: Wrapper })

      const button = screen.getByRole('button')
      await userEvent.click(button)

      await waitFor(() => {
        expect(toast.error).toHaveBeenCalledWith('Invalid subscription tier selected')
      })
    })

    it('should show toast error for 500 status', async () => {
      mockCreatePaddleCheckoutMutateAsync.mockRejectedValue({ status: 500 })

      const Wrapper = createWrapper()
      render(<PaddleCheckoutButton tier="premium" billingInterval="month" />, { wrapper: Wrapper })

      const button = screen.getByRole('button')
      await userEvent.click(button)

      await waitFor(() => {
        expect(toast.error).toHaveBeenCalledWith('Payment system error. Please try again later.')
      })
    })

    it('should show generic error for unknown errors', async () => {
      mockCreatePaddleCheckoutMutateAsync.mockRejectedValue({ status: 999 })

      const Wrapper = createWrapper()
      render(<PaddleCheckoutButton tier="premium" billingInterval="month" />, { wrapper: Wrapper })

      const button = screen.getByRole('button')
      await userEvent.click(button)

      await waitFor(() => {
        expect(toast.error).toHaveBeenCalledWith('Failed to start checkout. Please try again.')
      })
    })
  })

  describe('Paddle.js Integration', () => {
    it('should show error if window.Paddle is not loaded', async () => {
      // @ts-expect-error - Deleting window.Paddle to test error handling
      delete window.Paddle

      const Wrapper = createWrapper()
      render(<PaddleCheckoutButton tier="premium" billingInterval="month" />, { wrapper: Wrapper })

      const button = screen.getByRole('button')
      await userEvent.click(button)

      await waitFor(() => {
        expect(toast.error).toHaveBeenCalledWith('Payment system not loaded. Please refresh the page.')
      })

      expect(mockCreatePaddleCheckoutMutateAsync).not.toHaveBeenCalled()
    })

    it('should pass checkout URL to Paddle.Checkout.open', async () => {
      const customCheckoutURL = 'https://checkout.paddle.com/txn_custom_123'
      mockCreatePaddleCheckoutMutateAsync.mockResolvedValue({
        checkout_url: customCheckoutURL,
        transaction_id: 'txn_custom_123',
      })

      const Wrapper = createWrapper()
      render(<PaddleCheckoutButton tier="premium" billingInterval="month" />, { wrapper: Wrapper })

      const button = screen.getByRole('button')
      await userEvent.click(button)

      await waitFor(() => {
        expect(window.Paddle.Checkout.open).toHaveBeenCalledWith({
          settings: {
            checkoutURL: customCheckoutURL,
          },
        })
      })
    })
  })

  describe('Loading State', () => {
    it('should disable button while processing', async () => {
      mockCreatePaddleCheckoutMutateAsync.mockImplementation(
        () =>
          new Promise((resolve) =>
            setTimeout(() => resolve(mockPaddleCheckoutResponse), 100)
          )
      )

      const Wrapper = createWrapper()
      render(<PaddleCheckoutButton tier="premium" billingInterval="month" />, { wrapper: Wrapper })

      const button = screen.getByRole('button')
      expect(button.disabled).toBe(false)

      await userEvent.click(button)

      // Button should be disabled during processing
      expect(button.disabled).toBe(true)

      await waitFor(() => {
        expect(window.Paddle.Checkout.open).toHaveBeenCalled()
      })
    })

    it('should re-enable button after error', async () => {
      mockCreatePaddleCheckoutMutateAsync.mockRejectedValue({ status: 500 })

      const Wrapper = createWrapper()
      render(<PaddleCheckoutButton tier="premium" billingInterval="month" />, { wrapper: Wrapper })

      const button = screen.getByRole('button')
      await userEvent.click(button)

      await waitFor(() => {
        expect(toast.error).toHaveBeenCalled()
      })

      // Button should be re-enabled after error
      await waitFor(() => {
        expect(button.disabled).toBe(false)
      })
    })
  })
})
