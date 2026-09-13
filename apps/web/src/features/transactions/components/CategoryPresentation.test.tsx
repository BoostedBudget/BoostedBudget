import { describe, expect, it } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { TransactionTable } from './TransactionTable'
import { TransactionCard } from './TransactionCard'
import type { Transaction } from '../types'

const telephone: Transaction = {
  id: 'phone', user_id: 'user', account_id: 'account', linked_item_id: 'item', source: 'plaid',
  amount: 10, date: '2026-09-01', name: 'Phone bill', pending: false,
  created_at: '', updated_at: '', pfc_primary: 'RENT_AND_UTILITIES',
  pfc_detailed: 'RENT_AND_UTILITIES_TELEPHONE',
  pfc_primary_display: 'Rent and Utilities', pfc_detailed_display: 'Telephone',
}

describe('category hierarchy presentation', () => {
  for (const override of [false, true]) {
    it(`separates Telephone category and detail (${override ? 'override' : 'source'})`, () => {
      const transaction: Transaction = override ? {
        ...telephone, pfc_primary: 'FOOD_AND_DRINK', pfc_detailed: 'FOOD_AND_DRINK_COFFEE',
        category_override_kind: 'pfc', pfc_primary_override: 'RENT_AND_UTILITIES',
        pfc_detailed_override: 'RENT_AND_UTILITIES_TELEPHONE',
      } : telephone
      render(<TransactionTable transactions={[{ ...transaction, source: 'banking' }]} accountsMap={{}} />)
      expect(screen.getByRole('cell', { name: 'Rent & Utilities' })).toBeInTheDocument()
      expect(screen.getByRole('cell', { name: 'Telephone' })).toBeInTheDocument()
    })
  }

  it('renders the full masked breadcrumb on cards', () => {
    render(<TransactionCard transaction={telephone} />)
    expect(screen.getByText('Rent & Utilities › Telephone').closest('[data-ph-mask]')).not.toBeNull()
  })

  it('shows a custom override ancestry instead of its bank category and masks every name', () => {
    const customCategories = [
      { id: 'parent', name: 'Private phones', parent_pfc_primary: 'RENT_AND_UTILITIES' },
      { id: 'child', name: 'Private child', parent_id: 'parent' },
    ]
    const transaction: Transaction = { ...telephone, category_override_kind: 'custom', custom_category_override_id: 'child' }
    render(<TransactionTable transactions={[{ ...transaction, source: 'banking' }]} customCategories={customCategories} accountsMap={{}} />)
    expect(screen.getByRole('cell', { name: 'Rent & Utilities' })).toBeInTheDocument()
    expect(screen.getByRole('cell', { name: 'Rent & Utilities' })).toHaveAttribute('data-ph-mask')
    expect(screen.getByRole('cell', { name: 'Private phones › Private child' })).toHaveAttribute('data-ph-mask')
    expect(screen.queryByText('Telephone')).toBeNull()
    render(<TransactionCard transaction={transaction} customCategories={customCategories} />)
    expect(screen.getByText('Rent & Utilities › Private phones › Private child').closest('[data-ph-mask]')).not.toBeNull()
  })

  it('keeps missing custom metadata visible using its saved display', () => {
    const transaction: Transaction = { ...telephone, category_override_kind: 'custom', custom_category_override_id: 'missing', custom_category_override_display: 'Saved category' }
    render(<TransactionTable transactions={[{ ...transaction, source: 'banking' }]} accountsMap={{}} />)
    expect(screen.getByRole('cell', { name: 'Saved category' })).toBeInTheDocument()
    expect(screen.getByRole('cell', { name: '—' })).toBeInTheDocument()
    expect(screen.queryByText('Telephone')).toBeNull()
  })
})
