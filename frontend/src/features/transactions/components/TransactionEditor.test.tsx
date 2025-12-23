/**
 * TransactionEditor Component Tests
 * Issue #564: Added mocks for generated React Query hooks used by CategoryEditor
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { TransactionEditor } from './TransactionEditor'
import type { Transaction } from '../types'
import type { UserTag } from '../../tags/types'
import type { CustomCategory } from '../../categories/types'

// Mock the useCreateCategory hook that CategoryEditor uses
vi.mock('@/api/generated/endpoints/categories/categories', () => ({
  useCreateCategory: vi.fn(() => ({
    mutateAsync: vi.fn(),
    isPending: false,
  })),
}))

// Mock the API functions
vi.mock('../api/transactionsApi', () => ({
  updateTransactionTags: vi.fn(),
  updateTransactionCategory: vi.fn(),
}))

import { updateTransactionTags, updateTransactionCategory } from '../api/transactionsApi'

// Helper to create wrapper with QueryClient
function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

describe('TransactionEditor', () => {
  const mockTransaction: Transaction = {
    id: 'txn-1',
    user_id: 'user-1',
    account_id: 'acc-1',
    linked_item_id: 'item-1',
    source: 'plaid',
    amount: -50.0,
    date: '2025-01-15',
    name: 'Test Transaction',
    merchant_name: 'Test Merchant',
    category: '["Food & Dining"]',
    pending: false,
    tags: ['Food'],
    created_at: '2025-01-15T00:00:00Z',
    updated_at: '2025-01-15T00:00:00Z',
  }

  const mockTags: UserTag[] = [
    {
      id: 'tag-1',
      user_id: 'user-1',
      name: 'Food',
      color: '#FF5733',
      usage_count: 5,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
    {
      id: 'tag-2',
      user_id: 'user-1',
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
      user_id: 'user-1',
      name: 'Food & Dining',
      color: '#FF5733',
      usage_count: 10,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
    {
      id: 'cat-2',
      user_id: 'user-1',
      name: 'Shopping',
      color: '#33FF57',
      usage_count: 5,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
  ]

  const defaultProps = {
    transaction: mockTransaction,
    availableTags: mockTags,
    availableCategories: mockCategories,
    onClose: vi.fn(),
    onSave: vi.fn(),
    isOpen: true,
  }

  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(updateTransactionTags).mockResolvedValue(mockTransaction)
    vi.mocked(updateTransactionCategory).mockResolvedValue(mockTransaction)
  })

  describe('Rendering', () => {
    it('should render modal when open', () => {
      const Wrapper = createWrapper()
      render(<TransactionEditor {...defaultProps} />, { wrapper: Wrapper })

      expect(screen.getByText('Edit Transaction')).toBeInTheDocument()
    })

    it('should not render when closed', () => {
      const Wrapper = createWrapper()
      render(<TransactionEditor {...defaultProps} isOpen={false} />, { wrapper: Wrapper })

      expect(screen.queryByText('Edit Transaction')).not.toBeInTheDocument()
    })

    it('should display transaction details', () => {
      const Wrapper = createWrapper()
      render(<TransactionEditor {...defaultProps} />, { wrapper: Wrapper })

      expect(screen.getByText('Test Merchant')).toBeInTheDocument()
      expect(screen.getByText('$50.00')).toBeInTheDocument()
    })

    it('should show Tags section', () => {
      const Wrapper = createWrapper()
      render(<TransactionEditor {...defaultProps} />, { wrapper: Wrapper })

      expect(screen.getByText('Tags')).toBeInTheDocument()
    })

    it('should show Category section', () => {
      const Wrapper = createWrapper()
      render(<TransactionEditor {...defaultProps} />, { wrapper: Wrapper })

      expect(screen.getByText('Category')).toBeInTheDocument()
    })
  })

  describe('Tags editing', () => {
    it('should display existing tags', () => {
      const Wrapper = createWrapper()
      render(<TransactionEditor {...defaultProps} />, { wrapper: Wrapper })

      expect(screen.getByText('Food')).toBeInTheDocument()
    })

    it('should allow removing tags', async () => {
      const Wrapper = createWrapper()
      render(<TransactionEditor {...defaultProps} />, { wrapper: Wrapper })

      const removeButton = screen.getByRole('button', { name: /remove food/i })
      fireEvent.click(removeButton)

      await waitFor(() => {
        expect(screen.queryByText('Food')).not.toBeInTheDocument()
      })
    })
  })

  describe('Category editing', () => {
    it('should display current category', () => {
      const Wrapper = createWrapper()
      render(<TransactionEditor {...defaultProps} />, { wrapper: Wrapper })

      expect(screen.getByText('Food & Dining')).toBeInTheDocument()
    })
  })

  describe('Save functionality', () => {
    it('should call API when save button clicked with tag changes', async () => {
      const Wrapper = createWrapper()
      render(<TransactionEditor {...defaultProps} />, { wrapper: Wrapper })

      // Remove a tag
      const removeButton = screen.getByRole('button', { name: /remove food/i })
      fireEvent.click(removeButton)

      // Click save
      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(updateTransactionTags).toHaveBeenCalledWith('txn-1', { tags: [] })
      })
    })

    it('should call onSave callback after successful save', async () => {
      const onSave = vi.fn()
      const Wrapper = createWrapper()
      render(<TransactionEditor {...defaultProps} onSave={onSave} />, { wrapper: Wrapper })

      // Remove a tag to make a change
      const removeButton = screen.getByRole('button', { name: /remove food/i })
      fireEvent.click(removeButton)

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(onSave).toHaveBeenCalled()
      })
    })

    it('should pass updated transaction to onSave with tag changes', async () => {
      const onSave = vi.fn()
      const updatedTxn: Transaction = {
        ...mockTransaction,
        tags: [],
        updated_at: '2025-01-15T12:00:00Z',
      }
      vi.mocked(updateTransactionTags).mockResolvedValue(updatedTxn)

      const Wrapper = createWrapper()
      render(<TransactionEditor {...defaultProps} onSave={onSave} />, { wrapper: Wrapper })

      // Remove tag
      const removeButton = screen.getByRole('button', { name: /remove food/i })
      fireEvent.click(removeButton)

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(onSave).toHaveBeenCalledWith(updatedTxn)
      })
    })

    it('should pass updated transaction to onSave with category changes', async () => {
      const onSave = vi.fn()
      const txnNoTags: Transaction = {
        ...mockTransaction,
        tags: [],
        category: '["Food & Dining"]',
      }
      const updatedTxn: Transaction = {
        ...txnNoTags,
        category_override: '["Shopping"]',
        updated_at: '2025-01-15T12:00:00Z',
      }
      vi.mocked(updateTransactionCategory).mockResolvedValue(updatedTxn)

      const Wrapper = createWrapper()
      render(
        <TransactionEditor
          {...defaultProps}
          transaction={txnNoTags}
          onSave={onSave}
        />,
        { wrapper: Wrapper }
      )

      // Change category
      const changeButton = screen.getByRole('button', { name: /change category/i })
      fireEvent.click(changeButton)

      await waitFor(() => {
        const option = screen.getByRole('option', { name: 'Shopping' })
        fireEvent.click(option)
      })

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(onSave).toHaveBeenCalledWith(updatedTxn)
      })
    })

    it('should pass final transaction state when both tags and category change', async () => {
      const onSave = vi.fn()
      const afterTagUpdate: Transaction = {
        ...mockTransaction,
        tags: [],
      }
      const afterCategoryUpdate: Transaction = {
        ...afterTagUpdate,
        category_override: '["Shopping"]',
        updated_at: '2025-01-15T12:00:00Z',
      }
      vi.mocked(updateTransactionTags).mockResolvedValue(afterTagUpdate)
      vi.mocked(updateTransactionCategory).mockResolvedValue(afterCategoryUpdate)

      const Wrapper = createWrapper()
      render(<TransactionEditor {...defaultProps} onSave={onSave} />, { wrapper: Wrapper })

      // Remove tag
      const removeButton = screen.getByRole('button', { name: /remove food/i })
      fireEvent.click(removeButton)

      // Change category
      const changeButton = screen.getByRole('button', { name: /change category/i })
      fireEvent.click(changeButton)

      await waitFor(() => {
        const option = screen.getByRole('option', { name: 'Shopping' })
        fireEvent.click(option)
      })

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        // Should receive the final state after both updates
        expect(onSave).toHaveBeenCalledWith(afterCategoryUpdate)
      })
    })

    it('should show error message on save failure', async () => {
      vi.mocked(updateTransactionTags).mockRejectedValue(new Error('Save failed'))
      const Wrapper = createWrapper()
      render(<TransactionEditor {...defaultProps} />, { wrapper: Wrapper })

      // Remove a tag to make a change
      const removeButton = screen.getByRole('button', { name: /remove food/i })
      fireEvent.click(removeButton)

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/failed to save/i)).toBeInTheDocument()
      })
    })

    it('should show loading state while saving', async () => {
      // Create a promise that we control
      let resolvePromise: (value: Transaction) => void = () => {}
      vi.mocked(updateTransactionTags).mockReturnValue(
        new Promise((resolve) => {
          resolvePromise = resolve
        })
      )

      const Wrapper = createWrapper()
      render(<TransactionEditor {...defaultProps} />, { wrapper: Wrapper })

      // Remove a tag
      const removeButton = screen.getByRole('button', { name: /remove food/i })
      fireEvent.click(removeButton)

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/saving/i)).toBeInTheDocument()
      })

      // Resolve the promise
      resolvePromise(mockTransaction)
    })
  })

  describe('Cancel functionality', () => {
    it('should call onClose when cancel button clicked', () => {
      const onClose = vi.fn()
      const Wrapper = createWrapper()
      render(<TransactionEditor {...defaultProps} onClose={onClose} />, { wrapper: Wrapper })

      const cancelButton = screen.getByRole('button', { name: /cancel/i })
      fireEvent.click(cancelButton)

      expect(onClose).toHaveBeenCalled()
    })
  })

  describe('Category changes', () => {
    it('should save only category changes without calling updateTransactionTags', async () => {
      // Transaction with no tags to ensure only category changes
      const txnNoTags: Transaction = {
        ...mockTransaction,
        tags: [],
        category: '["Food & Dining"]',
      }

      const Wrapper = createWrapper()
      render(
        <TransactionEditor
          {...defaultProps}
          transaction={txnNoTags}
        />,
        { wrapper: Wrapper }
      )

      // Change category
      const changeButton = screen.getByRole('button', { name: /change category/i })
      fireEvent.click(changeButton)

      await waitFor(() => {
        const option = screen.getByRole('option', { name: 'Shopping' })
        fireEvent.click(option)
      })

      // Click save
      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(updateTransactionCategory).toHaveBeenCalledWith('txn-1', { category: ['Shopping'] })
        expect(updateTransactionTags).not.toHaveBeenCalled()
      })
    })

    it('should save both tag and category changes', async () => {
      const Wrapper = createWrapper()
      render(<TransactionEditor {...defaultProps} />, { wrapper: Wrapper })

      // Remove a tag
      const removeButton = screen.getByRole('button', { name: /remove food/i })
      fireEvent.click(removeButton)

      // Change category
      const changeButton = screen.getByRole('button', { name: /change category/i })
      fireEvent.click(changeButton)

      await waitFor(() => {
        const option = screen.getByRole('option', { name: 'Shopping' })
        fireEvent.click(option)
      })

      // Click save
      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(updateTransactionTags).toHaveBeenCalled()
        expect(updateTransactionCategory).toHaveBeenCalled()
      })
    })
  })

  describe('parseCategory edge cases', () => {
    it('should handle transaction with null category', () => {
      const txnNullCategory: Transaction = {
        ...mockTransaction,
        category: null,
      }

      const Wrapper = createWrapper()
      render(
        <TransactionEditor
          {...defaultProps}
          transaction={txnNullCategory}
        />,
        { wrapper: Wrapper }
      )

      // Should show Uncategorized
      expect(screen.getByText(/uncategorized/i)).toBeInTheDocument()
    })

    it('should handle transaction with category_override', () => {
      const txnWithOverride: Transaction = {
        ...mockTransaction,
        category: '["Food & Dining"]',
        category_override: '["Shopping"]',
      }

      const Wrapper = createWrapper()
      render(
        <TransactionEditor
          {...defaultProps}
          transaction={txnWithOverride}
        />,
        { wrapper: Wrapper }
      )

      // Should display the override category
      expect(screen.getByText('Shopping')).toBeInTheDocument()
    })
  })

  describe('Uncategorized option', () => {
    it('should show Uncategorized as first option in category dropdown', async () => {
      const Wrapper = createWrapper()
      render(<TransactionEditor {...defaultProps} />, { wrapper: Wrapper })

      // Open category dropdown
      const changeButton = screen.getByRole('button', { name: /change category/i })
      fireEvent.click(changeButton)

      await waitFor(() => {
        const uncategorizedOption = screen.getByRole('option', { name: /uncategorized/i })
        expect(uncategorizedOption).toBeInTheDocument()
      })
    })

    it('should have a clear button to remove category', () => {
      const Wrapper = createWrapper()
      render(<TransactionEditor {...defaultProps} />, { wrapper: Wrapper })

      const clearButton = screen.getByRole('button', { name: /clear category/i })
      expect(clearButton).toBeInTheDocument()
    })

    it('should set category to empty array when clear button is clicked', async () => {
      const Wrapper = createWrapper()
      render(<TransactionEditor {...defaultProps} />, { wrapper: Wrapper })

      const clearButton = screen.getByRole('button', { name: /clear category/i })
      fireEvent.click(clearButton)

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(updateTransactionCategory).toHaveBeenCalledWith('txn-1', { category: [] })
      })
    })

    it('should set category to empty when Uncategorized option is selected', async () => {
      const Wrapper = createWrapper()
      render(<TransactionEditor {...defaultProps} />, { wrapper: Wrapper })

      // Open category dropdown
      const changeButton = screen.getByRole('button', { name: /change category/i })
      fireEvent.click(changeButton)

      await waitFor(() => {
        const uncategorizedOption = screen.getByRole('option', { name: /uncategorized/i })
        fireEvent.click(uncategorizedOption)
      })

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(updateTransactionCategory).toHaveBeenCalledWith('txn-1', { category: [] })
      })
    })

    it('should show Reset to Original option when original_category exists', async () => {
      const txnWithOriginal: Transaction = {
        ...mockTransaction,
        category: '["Food & Dining"]',
        category_override: '[]',
        category_source: 'manual',
        original_category: '["Food & Dining"]',
      }

      const Wrapper = createWrapper()
      render(
        <TransactionEditor
          {...defaultProps}
          transaction={txnWithOriginal}
        />,
        { wrapper: Wrapper }
      )

      // Should see a reset option
      const resetButton = screen.getByRole('button', { name: /reset to original/i })
      expect(resetButton).toBeInTheDocument()
    })

    it('should reset to original category when reset button is clicked', async () => {
      const txnWithOriginal: Transaction = {
        ...mockTransaction,
        category: '["Food & Dining"]',
        category_override: '[]',
        category_source: 'manual',
        original_category: '["Food & Dining"]',
      }

      const Wrapper = createWrapper()
      render(
        <TransactionEditor
          {...defaultProps}
          transaction={txnWithOriginal}
        />,
        { wrapper: Wrapper }
      )

      const resetButton = screen.getByRole('button', { name: /reset to original/i })
      fireEvent.click(resetButton)

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(updateTransactionCategory).toHaveBeenCalledWith('txn-1', { category: ['Food & Dining'] })
      })
    })
  })
})
