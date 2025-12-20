/**
 * TransactionsPage Component Tests
 *
 * TDD RED: These tests are written first and will initially fail.
 * Tests the transactions page container component.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { BrowserRouter } from 'react-router-dom'
import { TransactionsPage } from './TransactionsPage'
import * as transactionsApi from './api/transactionsApi'
import * as tagsApi from '../tags/api/tagsApi'
import * as categoriesApi from '../categories/api/categoriesApi'
import * as plaidApi from '../accounts/api/plaidApi'
import type { GetTransactionsResponse } from './types'
import type { UserTag } from '../tags/types'
import type { CustomCategory } from '../categories/types'

// Mock the APIs
vi.mock('./api/transactionsApi')
vi.mock('../tags/api/tagsApi')
vi.mock('../categories/api/categoriesApi')
vi.mock('../accounts/api/plaidApi')

// Wrapper component with Router
function renderWithRouter(ui: React.ReactElement) {
  return render(<BrowserRouter>{ui}</BrowserRouter>)
}

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

  const mockTags: UserTag[] = [
    {
      id: 'tag-1',
      user_id: 'user-123',
      name: 'Food',
      color: '#FF5733',
      usage_count: 5,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
    {
      id: 'tag-2',
      user_id: 'user-123',
      name: 'Shopping',
      color: '#33FF57',
      usage_count: 3,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
  ]

  const mockCategories: CustomCategory[] = [
    {
      id: 'cat-1',
      user_id: 'user-123',
      name: 'Food & Dining',
      color: '#FF5733',
      usage_count: 10,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
    {
      id: 'cat-2',
      user_id: 'user-123',
      name: 'Shopping',
      color: '#33FF57',
      usage_count: 5,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
  ]

  beforeEach(() => {
    vi.clearAllMocks()
    // Default mocks for tags and categories
    vi.mocked(tagsApi.fetchTags).mockResolvedValue(mockTags)
    vi.mocked(categoriesApi.fetchCategories).mockResolvedValue(mockCategories)
    // Mock plaid API for filter panel accounts
    vi.mocked(plaidApi.fetchAccounts).mockResolvedValue({
      accounts: [],
      total_count: 0,
    })
    // Mock Plaid categories for filter panel
    vi.mocked(categoriesApi.fetchPlaidCategories).mockResolvedValue([])
  })

  describe('Initial Loading', () => {
    it('should show loading state initially', async () => {
      vi.mocked(transactionsApi.fetchTransactions).mockImplementation(
        () => new Promise(() => {}) // Never resolves
      )

      renderWithRouter(<TransactionsPage />)
      expect(screen.getByText(/loading/i)).toBeInTheDocument()
    })

    it('should load and display transactions on mount', async () => {
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      renderWithRouter(<TransactionsPage />)

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

      renderWithRouter(<TransactionsPage />)

      await waitFor(() => {
        expect(screen.getByText(/failed to load/i)).toBeInTheDocument()
      })
    })

    it('should have retry button on error', async () => {
      vi.mocked(transactionsApi.fetchTransactions).mockRejectedValue(
        new Error('Network error')
      )

      renderWithRouter(<TransactionsPage />)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument()
      })
    })

    it('should retry loading when retry button clicked', async () => {
      const user = userEvent.setup()

      vi.mocked(transactionsApi.fetchTransactions)
        .mockRejectedValueOnce(new Error('Network error'))
        .mockResolvedValueOnce(mockTransactionsResponse)

      renderWithRouter(<TransactionsPage />)

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

      renderWithRouter(<TransactionsPage />)

      await waitFor(() => {
        expect(screen.getByText(/no transactions/i)).toBeInTheDocument()
      })
    })
  })

  describe('Transaction Display', () => {
    it('should display all transactions in a list', async () => {
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      renderWithRouter(<TransactionsPage />)

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })

      expect(screen.getByText('Salary')).toBeInTheDocument()
    })

    it('should show total count', async () => {
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      renderWithRouter(<TransactionsPage />)

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

      renderWithRouter(<TransactionsPage />)

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

      renderWithRouter(<TransactionsPage />)

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })

      expect(screen.getByRole('button', { name: /next/i })).toBeInTheDocument()
    })

    it('should not show previous button on first page', async () => {
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      renderWithRouter(<TransactionsPage />)

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

      renderWithRouter(<TransactionsPage />)

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

      renderWithRouter(<TransactionsPage />)

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

      renderWithRouter(<TransactionsPage />)

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

      renderWithRouter(<TransactionsPage />)

      await waitFor(() => {
        expect(screen.getByRole('heading', { name: /transactions/i })).toBeInTheDocument()
      })
    })

    it('should have main landmark', async () => {
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      const { container } = renderWithRouter(<TransactionsPage />)

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })

      expect(container.querySelector('main')).toBeInTheDocument()
    })
  })

  describe('Transaction Editing', () => {
    it('should fetch tags and categories on mount', async () => {
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      renderWithRouter(<TransactionsPage />)

      await waitFor(() => {
        expect(tagsApi.fetchTags).toHaveBeenCalled()
        expect(categoriesApi.fetchCategories).toHaveBeenCalled()
      })
    })

    it('should open editor when clicking a transaction', async () => {
      const user = userEvent.setup()
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      renderWithRouter(<TransactionsPage />)

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })

      // Click on the transaction card
      const transactionCard = screen.getByText('Starbucks Coffee').closest('article')
      expect(transactionCard).toBeInTheDocument()
      await user.click(transactionCard!)

      // Editor should open
      await waitFor(() => {
        expect(screen.getByText('Edit Transaction')).toBeInTheDocument()
      })
    })

    it('should display transaction details in editor', async () => {
      const user = userEvent.setup()
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      renderWithRouter(<TransactionsPage />)

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })

      const transactionCard = screen.getByText('Starbucks Coffee').closest('article')
      await user.click(transactionCard!)

      await waitFor(() => {
        expect(screen.getByText('Edit Transaction')).toBeInTheDocument()
        // Check amount is displayed (formatted)
        expect(screen.getByText('$25.50')).toBeInTheDocument()
      })
    })

    it('should close editor when cancel is clicked', async () => {
      const user = userEvent.setup()
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      renderWithRouter(<TransactionsPage />)

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })

      const transactionCard = screen.getByText('Starbucks Coffee').closest('article')
      await user.click(transactionCard!)

      await waitFor(() => {
        expect(screen.getByText('Edit Transaction')).toBeInTheDocument()
      })

      // Click cancel
      const cancelButton = screen.getByRole('button', { name: /cancel/i })
      await user.click(cancelButton)

      // Editor should close
      await waitFor(() => {
        expect(screen.queryByText('Edit Transaction')).not.toBeInTheDocument()
      })
    })

    it('should handle tag/category fetch errors gracefully', async () => {
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)
      vi.mocked(tagsApi.fetchTags).mockRejectedValue(new Error('Failed to fetch tags'))
      vi.mocked(categoriesApi.fetchCategories).mockRejectedValue(new Error('Failed to fetch categories'))

      // Should not crash
      renderWithRouter(<TransactionsPage />)

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })
    })

    it('should make transaction cards clickable', async () => {
      vi.mocked(transactionsApi.fetchTransactions).mockResolvedValue(mockTransactionsResponse)

      renderWithRouter(<TransactionsPage />)

      await waitFor(() => {
        expect(screen.getByText('Starbucks Coffee')).toBeInTheDocument()
      })

      const transactionCard = screen.getByText('Starbucks Coffee').closest('article')
      expect(transactionCard).toHaveAttribute('role', 'button')
      expect(transactionCard).toHaveAttribute('tabIndex', '0')
    })
  })
})
