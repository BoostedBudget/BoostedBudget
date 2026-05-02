import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { ReadOnlyProvider } from '@/auth/ReadOnlyContext'
import { TransactionEditor } from './TransactionEditor'
import type { Transaction } from '../types'
import { mockUpdateTransactionCategoryMutateAsync } from '@/test/mocks/transactions-api'

function createWrapper() {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        {children}
      </QueryClientProvider>
    )
  }
}

const baseTransaction: Transaction = {
  id: 'txn-1',
  user_id: 'user-1',
  account_id: 'acc-1',
  linked_item_id: 'item-1',
  source: 'plaid',
  amount: -12.45,
  date: '2024-01-15',
  name: 'KFC',
  pending: false,
  created_at: '2024-01-15T00:00:00Z',
  updated_at: '2024-01-15T00:00:00Z',
}

function renderEditor(transaction: Transaction) {
  return render(
    <ReadOnlyProvider isReadOnly={false}>
      <TransactionEditor
        transaction={transaction}
        availableTags={[]}
        availableCategories={[]}
        isOpen={true}
        onClose={() => {}}
        onSave={() => {}}
      />
    </ReadOnlyProvider>
  )
}

describe('TransactionEditor', () => {
  describe('getEffectiveCategory — PFC v2 fields', () => {
    it('shows pfc_primary as initial category (CategoryEditor shows selectedCategory[0])', () => {
      const txn: Transaction = {
        ...baseTransaction,
        pfc_primary: 'FOOD_AND_DRINK',
        pfc_detailed: 'FOOD_AND_DRINK_FAST_FOOD',
      }
      renderEditor(txn)
      // CategoryEditor resolves raw enum to display label via formatPFCEnum
      expect(screen.getByText('Food And Drink')).toBeInTheDocument()
    })

    it('shows pfc_primary when pfc_detailed is absent', () => {
      const txn: Transaction = {
        ...baseTransaction,
        pfc_primary: 'FOOD_AND_DRINK',
      }
      renderEditor(txn)
      expect(screen.getByText('Food And Drink')).toBeInTheDocument()
    })

    it('shows pfc_primary_override when category_override_kind is pfc', () => {
      const txn: Transaction = {
        ...baseTransaction,
        pfc_primary: 'FOOD_AND_DRINK',
        pfc_detailed: 'FOOD_AND_DRINK_RESTAURANTS',
        category_override_kind: 'pfc',
        pfc_primary_override: 'TRANSPORTATION',
        pfc_detailed_override: 'TRANSPORTATION_GAS',
      }
      renderEditor(txn)
      // Override takes precedence; CategoryEditor resolves override enum to display label
      expect(screen.getByText('Transportation')).toBeInTheDocument()
    })
  })

  describe('getEffectiveCategory — explicit-uncategorized guard', () => {
    it('shows Uncategorized when no pfc_primary and no override', () => {
      const txn: Transaction = {
        ...baseTransaction,
        pfc_primary: null,
        category_override_kind: null,
      }
      renderEditor(txn)
      expect(screen.getByText('Uncategorized')).toBeInTheDocument()
    })

    it('shows pfc_primary when category_source is manual but pfc_primary is set', () => {
      // In PFC v2, category_source='manual' does NOT suppress pfc_primary display.
      // The transaction still has its Plaid-assigned pfc_primary.
      const txn: Transaction = {
        ...baseTransaction,
        pfc_primary: 'FOOD_AND_DRINK',
        category_override_kind: null,
        category_source: 'manual',
      }
      renderEditor(txn)
      expect(screen.getByText('Food And Drink')).toBeInTheDocument()
    })
  })

  describe('save category — Plaid primary encoding', () => {
    const plaidPrimaries = [
      { enum: 'FOOD_AND_DRINK', display: 'Food And Drink' },
      { enum: 'TRANSPORTATION', display: 'Transportation' },
    ]

    beforeEach(() => {
      mockUpdateTransactionCategoryMutateAsync.mockClear()
      mockUpdateTransactionCategoryMutateAsync.mockResolvedValue({ ...baseTransaction })
    })

    it('sends PFC enum not display name when selecting a Plaid primary', async () => {
      const txn: Transaction = { ...baseTransaction, pfc_primary: 'FOOD_AND_DRINK' }
      const Wrapper = createWrapper()
      render(
        <ReadOnlyProvider isReadOnly={false}>
          <TransactionEditor
            transaction={txn}
            availableTags={[]}
            availableCategories={[]}
            plaidPrimaries={plaidPrimaries}
            isOpen={true}
            onClose={() => {}}
            onSave={() => {}}
          />
        </ReadOnlyProvider>,
        { wrapper: Wrapper }
      )

      fireEvent.click(screen.getByRole('button', { name: /change category/i }))
      await waitFor(() => expect(screen.getByRole('option', { name: 'Transportation' })).toBeInTheDocument())
      fireEvent.click(screen.getByRole('option', { name: 'Transportation' }))

      await waitFor(() => expect(screen.getByRole('button', { name: /save/i })).not.toBeDisabled())
      fireEvent.click(screen.getByRole('button', { name: /save/i }))

      await waitFor(() => {
        expect(mockUpdateTransactionCategoryMutateAsync).toHaveBeenCalledWith({
          id: 'txn-1',
          data: { kind: 'pfc', pfc_primary: 'TRANSPORTATION', pfc_detailed: undefined },
        })
      })
    })
  })
})
