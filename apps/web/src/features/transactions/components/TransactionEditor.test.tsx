import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { ReadOnlyProvider } from '@/auth/ReadOnlyContext'
import { TransactionEditor } from './TransactionEditor'
import type { Transaction } from '../types'

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
      // CategoryEditor renders selectedCategory[0] which is pfc_primary
      expect(screen.getByText('FOOD_AND_DRINK')).toBeInTheDocument()
    })

    it('shows pfc_primary when pfc_detailed is absent', () => {
      const txn: Transaction = {
        ...baseTransaction,
        pfc_primary: 'FOOD_AND_DRINK',
      }
      renderEditor(txn)
      expect(screen.getByText('FOOD_AND_DRINK')).toBeInTheDocument()
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
      // Override takes precedence; CategoryEditor shows selectedCategory[0] = pfc_primary_override
      expect(screen.getByText('TRANSPORTATION')).toBeInTheDocument()
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
      expect(screen.getByText('FOOD_AND_DRINK')).toBeInTheDocument()
    })
  })
})
