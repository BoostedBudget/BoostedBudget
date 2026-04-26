// Issue #1058: TransactionEditor PFC fallback and explicit-uncategorized tests

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
  describe('getEffectiveCategory — PFC fallback', () => {
    it('shows PFC primary_display as initial category when no legacy category or override', () => {
      const txn: Transaction = {
        ...baseTransaction,
        category: null,
        category_override: null,
        personal_finance_category_data: {
          primary: 'FOOD_AND_DRINK',
          primary_display: 'Food And Drink',
          detailed: 'FOOD_AND_DRINK_FAST_FOOD',
          detailed_display: 'Fast Food',
        },
      }
      renderEditor(txn)
      expect(screen.getByText('Food And Drink')).toBeInTheDocument()
    })

    it('shows legacy category when present, ignoring PFC', () => {
      const txn: Transaction = {
        ...baseTransaction,
        category: '["Food and Drink","Restaurants"]',
        category_override: null,
        personal_finance_category_data: {
          primary: 'FOOD_AND_DRINK',
          primary_display: 'Food And Drink',
        },
      }
      renderEditor(txn)
      expect(screen.getByText('Food and Drink')).toBeInTheDocument()
    })

    it('shows category_override when present, ignoring both legacy and PFC', () => {
      const txn: Transaction = {
        ...baseTransaction,
        category: '["Food and Drink","Restaurants"]',
        category_override: '["Dining Out"]',
        personal_finance_category_data: {
          primary: 'FOOD_AND_DRINK',
          primary_display: 'Food And Drink',
        },
      }
      renderEditor(txn)
      expect(screen.getByText('Dining Out')).toBeInTheDocument()
    })
  })

  describe('getEffectiveCategory — explicit-uncategorized guard', () => {
    it('shows Uncategorized (not PFC) when user manually cleared the category', () => {
      // category_source='manual' + category_override=null = user explicitly cleared it
      // Should NOT fall back to PFC even though primary_display is present
      const txn: Transaction = {
        ...baseTransaction,
        category: null,
        category_override: null,
        category_source: 'manual',
        personal_finance_category_data: {
          primary: 'FOOD_AND_DRINK',
          primary_display: 'Food And Drink',
        },
      }
      renderEditor(txn)
      // CategoryEditor renders italic "Uncategorized" when selectedCategory is []
      expect(screen.getByText('Uncategorized')).toBeInTheDocument()
      expect(screen.queryByText('Food And Drink')).not.toBeInTheDocument()
    })

    it('still shows category_override when category_source is manual but override is set', () => {
      const txn: Transaction = {
        ...baseTransaction,
        category: null,
        category_override: '["Groceries"]',
        category_source: 'manual',
        personal_finance_category_data: {
          primary: 'FOOD_AND_DRINK',
          primary_display: 'Food And Drink',
        },
      }
      renderEditor(txn)
      expect(screen.getByText('Groceries')).toBeInTheDocument()
    })
  })
})
