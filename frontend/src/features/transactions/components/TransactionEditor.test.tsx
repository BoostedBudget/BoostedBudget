import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { TransactionEditor } from './TransactionEditor'
import type { Transaction } from '../types'
import type { UserTag } from '../../tags/types'
import type { CustomCategory } from '../../categories/types'

// Mock the API functions
vi.mock('../api/transactionsApi', () => ({
  updateTransactionTags: vi.fn(),
  updateTransactionCategory: vi.fn(),
}))

import { updateTransactionTags, updateTransactionCategory } from '../api/transactionsApi'

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
      render(<TransactionEditor {...defaultProps} />)

      expect(screen.getByText('Edit Transaction')).toBeInTheDocument()
    })

    it('should not render when closed', () => {
      render(<TransactionEditor {...defaultProps} isOpen={false} />)

      expect(screen.queryByText('Edit Transaction')).not.toBeInTheDocument()
    })

    it('should display transaction details', () => {
      render(<TransactionEditor {...defaultProps} />)

      expect(screen.getByText('Test Merchant')).toBeInTheDocument()
      expect(screen.getByText('$50.00')).toBeInTheDocument()
    })

    it('should show Tags section', () => {
      render(<TransactionEditor {...defaultProps} />)

      expect(screen.getByText('Tags')).toBeInTheDocument()
    })

    it('should show Category section', () => {
      render(<TransactionEditor {...defaultProps} />)

      expect(screen.getByText('Category')).toBeInTheDocument()
    })
  })

  describe('Tags editing', () => {
    it('should display existing tags', () => {
      render(<TransactionEditor {...defaultProps} />)

      expect(screen.getByText('Food')).toBeInTheDocument()
    })

    it('should allow removing tags', async () => {
      render(<TransactionEditor {...defaultProps} />)

      const removeButton = screen.getByRole('button', { name: /remove food/i })
      fireEvent.click(removeButton)

      await waitFor(() => {
        expect(screen.queryByText('Food')).not.toBeInTheDocument()
      })
    })
  })

  describe('Category editing', () => {
    it('should display current category', () => {
      render(<TransactionEditor {...defaultProps} />)

      expect(screen.getByText('Food & Dining')).toBeInTheDocument()
    })
  })

  describe('Save functionality', () => {
    it('should call API when save button clicked with tag changes', async () => {
      render(<TransactionEditor {...defaultProps} />)

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
      render(<TransactionEditor {...defaultProps} onSave={onSave} />)

      // Remove a tag to make a change
      const removeButton = screen.getByRole('button', { name: /remove food/i })
      fireEvent.click(removeButton)

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(onSave).toHaveBeenCalled()
      })
    })

    it('should show error message on save failure', async () => {
      vi.mocked(updateTransactionTags).mockRejectedValue(new Error('Save failed'))
      render(<TransactionEditor {...defaultProps} />)

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

      render(<TransactionEditor {...defaultProps} />)

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
      render(<TransactionEditor {...defaultProps} onClose={onClose} />)

      const cancelButton = screen.getByRole('button', { name: /cancel/i })
      fireEvent.click(cancelButton)

      expect(onClose).toHaveBeenCalled()
    })
  })
})
