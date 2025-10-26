/**
 * TransactionsPage Component Tests
 *
 * TDD RED: These tests are written first and will initially fail.
 * Tests the transactions page container component.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { TransactionsPage } from './TransactionsPage'
import * as transactionsApi from './api/transactionsApi'
import type { GetTransactionsResponse } from './types'

// Mock the API
vi.mock('./api/transactionsApi')

describe('TransactionsPage', () => {
  const mockTransactionsResponse: GetTransactionsResponse = {
    transactions: [
      {
        id: 'txn-1',
        user_id: 'user-123',
        account_id: 'account-456',
        linked_item_id: 'item-789',
        source: 'plaid',
        external_transaction_id: 'plaid-txn-abc',
        amount: -25.50,
        date: '2025-10-25',
        name: 'Starbucks',
        merchant_name: 'Starbucks Coffee',
        category: '["Food and Drink", "Coffee"]',
        pending: false,
        created_at: '2025-10-25T10:00:00Z',
        updated_at: '2025-10-25T10:00:00Z',
      },
      {
        id: 'txn-2',
        user_id: 'user-123',
        account_id: 'account-456',
        linked_item_id: 'item-789',
        source: 'plaid',
        external_transaction_id: 'plaid-txn-def',
        amount: 1000.00,
        date: '2025-10-24',
        name: 'Salary',
        merchant_name: null,
        category: '["Income", "Payroll"]',
        pending: false,
        created_at: '2025-10-24T10:00:00Z',
        updated_at: '2025-10-24T10:00:00Z',
      },
    ],
    total_count: 25,
    limit: 50,
    offset: 0,
    has_more: false,
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Initial Loading', () => {
    it('should show loading state initially', async () => {
      vi.mocked(transactionsApi.fetchTransactions).mockImplementation(
        () => new Promise(() => {}) // Never resolves
      )

      render(<TransactionsPage />)
      expect(screen.getByText(/loading/i)).toBeInTheDocument()
    })

    it('should load and display transactions on mount', async () => {
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      render(<TransactionsPage />)

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })

      expect(screen.getByText('Salary')).toBeInTheDocument()
    })
  })

  describe('Error Handling', () => {
    it('should display error message when fetch fails', async () => {
      vi.mocked(transactionsApi.fetchTransactions).mockRejectedValue(
        new Error('Network error')
      )

      render(<TransactionsPage />)

      await waitFor(() => {
        expect(screen.getByText(/failed to load/i)).toBeInTheDocument()
      })
    })

    it('should have retry button on error', async () => {
      vi.mocked(transactionsApi.fetchTransactions).mockRejectedValue(
        new Error('Network error')
      )

      render(<TransactionsPage />)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument()
      })
    })

    it('should retry loading when retry button clicked', async () => {
      const user = userEvent.setup()

      vi.mocked(transactionsApi.fetchTransactions)
        .mockRejectedValueOnce(new Error('Network error'))
        .mockResolvedValueOnce(mockTransactionsResponse)

      render(<TransactionsPage />)

      await waitFor(() => {
        expect(screen.getByText(/failed to load/i)).toBeInTheDocument()
      })

      const retryButton = screen.getByRole('button', { name: /retry/i })
      await user.click(retryButton)

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })
    })
  })

  describe('Empty State', () => {
    it('should show empty state when no transactions', async () => {
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue({
        transactions: [],
        total_count: 0,
        limit: 50,
        offset: 0,
        has_more: false,
      })

      render(<TransactionsPage />)

      await waitFor(() => {
        expect(screen.getByText(/no transactions/i)).toBeInTheDocument()
      })
    })
  })

  describe('Transaction Display', () => {
    it('should display all transactions in a list', async () => {
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      render(<TransactionsPage />)

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })

      expect(screen.getByText('Salary')).toBeInTheDocument()
    })

    it('should show total count', async () => {
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      render(<TransactionsPage />)

      await waitFor(() => {
        expect(screen.getByText(/25.*transaction/i)).toBeInTheDocument()
      })
    })
  })

  describe('Pagination', () => {
    it('should not show next button when has_more is false', async () => {
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue({
        ...mockTransactionsResponse,
        has_more: false,
      })

      render(<TransactionsPage />)

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })

      expect(screen.queryByRole('button', { name: /next/i })).not.toBeInTheDocument()
    })

    it('should show next button when has_more is true', async () => {
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue({
        ...mockTransactionsResponse,
        has_more: true,
      })

      render(<TransactionsPage />)

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })

      expect(screen.getByRole('button', { name: /next/i })).toBeInTheDocument()
    })

    it('should not show previous button on first page', async () => {
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      render(<TransactionsPage />)

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })

      expect(screen.queryByRole('button', { name: /previous/i })).not.toBeInTheDocument()
    })

    it('should load next page when next button clicked', async () => {
      const user = userEvent.setup()

      vi.mocked(transactionsApi.fetchTransactions)
        .mockResolvedValueOnce({
          ...mockTransactionsResponse,
          has_more: true,
        })
        .mockResolvedValueOnce({
          ...mockTransactionsResponse,
          transactions: [
            {
              ...mockTransactionsResponse.transactions[0],
              id: 'txn-3',
              name: 'Next Page Transaction',
              merchant_name: 'Next Page Merchant', // Fix: use merchant_name instead
            },
          ],
          offset: 50,
          has_more: false,
        })

      render(<TransactionsPage />)

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })

      const nextButton = screen.getByRole('button', { name: /next/i })
      await user.click(nextButton)

      await waitFor(() => {
        expect(screen.getByText('Next Page Merchant')).toBeInTheDocument()
      })
    })
  })

  describe('Search', () => {
    it('should have search input', async () => {
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      render(<TransactionsPage />)

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })

      expect(screen.getByPlaceholderText(/search/i)).toBeInTheDocument()
    })

    it('should filter transactions when search input changes', async () => {
      const user = userEvent.setup()

      vi.mocked(transactionsApi.fetchTransactions)
        .mockResolvedValueOnce(mockTransactionsResponse)
        .mockResolvedValueOnce({
          ...mockTransactionsResponse,
          transactions: [mockTransactionsResponse.transactions[0]], // Only Starbucks
          total_count: 1,
        })

      render(<TransactionsPage />)

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })

      const searchInput = screen.getByPlaceholderText(/search/i)
      await user.type(searchInput, 'starbucks')

      // Wait for debounce (300ms) + fetch
      await waitFor(
        () => {
          expect(transactionsApi.fetchTransactions).toHaveBeenCalledWith(
            expect.objectContaining({
              filters: expect.objectContaining({
                search: 'starbucks',
              }),
            })
          )
        },
        { timeout: 1000 }
      )
    })
  })

  describe('Accessibility', () => {
    it('should have proper heading structure', async () => {
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      render(<TransactionsPage />)

      await waitFor(() => {
        expect(screen.getByRole('heading', { name: /transactions/i })).toBeInTheDocument()
      })
    })

    it('should have main landmark', async () => {
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      const { container } = render(<TransactionsPage />)

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })

      expect(container.querySelector('main')).toBeInTheDocument()
    })
  })
})
