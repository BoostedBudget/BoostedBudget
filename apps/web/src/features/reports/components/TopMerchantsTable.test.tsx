import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import React from 'react'

import '@/test/mocks/shared-charts'
import { TopMerchantsTable } from './TopMerchantsTable'
import type { SpendingByMerchantResponse } from '@boostedbudget/api-client/models'

const mockData: SpendingByMerchantResponse = {
  start_date: '2024-01-01',
  end_date: '2024-01-31',
  top_n: 10,
  merchants: [
    { merchant_name: 'Starbucks', total: 150, transaction_count: 15, percentage: 30 },
    { merchant_name: 'Amazon', total: 350, transaction_count: 5, percentage: 70 },
  ],
}

describe('TopMerchantsTable', () => {
  it('renders loading state', () => {
    render(<TopMerchantsTable data={null} loading={true} />)
    expect(screen.getByLabelText('Loading merchants')).toBeDefined()
  })

  it('renders empty state when no merchants', () => {
    const emptyData: SpendingByMerchantResponse = {
      start_date: '2024-01-01',
      end_date: '2024-01-31',
      top_n: 10,
      merchants: [],
    }
    render(<TopMerchantsTable data={emptyData} loading={false} />)
    expect(screen.getByText(/No merchant data/)).toBeDefined()
  })

  it('renders all merchants', () => {
    render(<TopMerchantsTable data={mockData} loading={false} />)
    expect(screen.getByText('Starbucks')).toBeDefined()
    expect(screen.getByText('Amazon')).toBeDefined()
  })

  it('shows column headers', () => {
    render(<TopMerchantsTable data={mockData} loading={false} />)
    expect(screen.getByText('Merchant')).toBeDefined()
    // "Total" has sort indicator appended since it is the default sort column
    expect(screen.getByText(/Total/)).toBeDefined()
    expect(screen.getByText('Transactions')).toBeDefined()
  })

  it('sorts by merchant name when header clicked twice (ascending)', () => {
    render(<TopMerchantsTable data={mockData} loading={false} />)
    const merchantHeader = screen.getByText('Merchant')
    // First click: sort by merchant desc (S > A), Starbucks first
    fireEvent.click(merchantHeader)
    // Second click: sort by merchant asc (A < S), Amazon first
    fireEvent.click(merchantHeader)
    const rows = screen.getAllByRole('row')
    expect(rows[1].textContent).toContain('Amazon')
  })

  it('calls onMerchantClick when row is clicked', () => {
    const onMerchantClick = mock((name: string) => name)
    render(
      <TopMerchantsTable
        data={mockData}
        loading={false}
        onMerchantClick={onMerchantClick}
      />
    )
    fireEvent.click(screen.getByText('Starbucks'))
    expect(onMerchantClick).toHaveBeenCalledWith('Starbucks')
  })
})
