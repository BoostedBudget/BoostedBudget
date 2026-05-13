import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import React from 'react'

import '@/test/mocks/shared-charts'
import { TopMerchantsTable } from './TopMerchantsTable'
import type { SpendingByMerchantResponse } from '@boostedbudget/api-client/models'
import type { SortState } from '@/shared/components/DataTable'

const mockData: SpendingByMerchantResponse = {
  start_date: '2024-01-01',
  end_date: '2024-01-31',
  top_n: 10,
  merchants: [
    { merchant_name: 'Starbucks', total: 150, transaction_count: 15, percentage: 30 },
    { merchant_name: 'Amazon', total: 350, transaction_count: 5, percentage: 70 },
  ],
}

const defaultMocks = {
  onSortChange: mock((_: SortState) => {}),
  onMerchantClick: mock((name: string) => name),
}

function renderTable(overrides: Partial<{
  data: SpendingByMerchantResponse | null
  loading: boolean
  sort: SortState
  onSortChange: ReturnType<typeof mock>
  onMerchantClick: ReturnType<typeof mock>
}> = {}) {
  return render(
    <TopMerchantsTable
      data={mockData}
      loading={false}
      sort={null}
      onSortChange={overrides.onSortChange ?? defaultMocks.onSortChange}
      onMerchantClick={overrides.onMerchantClick}
      {...overrides}
    />
  )
}

describe('TopMerchantsTable', () => {
  it('renders loading state', () => {
    render(<TopMerchantsTable data={null} loading={true} sort={null} onSortChange={defaultMocks.onSortChange} />)
    expect(screen.getByLabelText('Loading merchants')).toBeDefined()
  })

  it('renders empty state when no merchants', () => {
    const emptyData: SpendingByMerchantResponse = {
      start_date: '2024-01-01',
      end_date: '2024-01-31',
      top_n: 10,
      merchants: [],
    }
    renderTable({ data: emptyData })
    expect(screen.getByText(/No merchant data/)).toBeDefined()
  })

  it('renders all merchants', () => {
    renderTable()
    expect(screen.getByText('Starbucks')).toBeDefined()
    expect(screen.getByText('Amazon')).toBeDefined()
  })

  it('shows column headers', () => {
    renderTable()
    expect(screen.getByText('Merchant')).toBeDefined()
    expect(screen.getByText(/Total/)).toBeDefined()
    expect(screen.getByText('Transactions')).toBeDefined()
  })

  it('calls onSortChange with ascending sort when Merchant header clicked from null', () => {
    const onSortChange = mock((_: SortState) => {})
    renderTable({ sort: null, onSortChange })
    const merchantBtn = screen
      .getAllByRole('button')
      .find((b) => b.textContent?.includes('Merchant'))!
    fireEvent.click(merchantBtn)
    expect(onSortChange.mock.calls[0][0]).toEqual({ columnId: 'merchant_name', direction: 'asc' })
  })

  it('calls onSortChange with null when clicking already-sorted-desc column (3-state cycle: desc → null)', () => {
    const onSortChange = mock((_: SortState) => {})
    renderTable({ sort: { columnId: 'total', direction: 'desc' }, onSortChange })
    const totalBtn = screen
      .getAllByRole('button')
      .find((b) => b.textContent?.includes('Total'))!
    fireEvent.click(totalBtn)
    expect(onSortChange.mock.calls[0][0]).toBeNull()
  })

  it('calls onSortChange with asc for new column when a different column is already sorted', () => {
    const onSortChange = mock((_: SortState) => {})
    renderTable({ sort: { columnId: 'total', direction: 'desc' }, onSortChange })
    const merchantBtn = screen
      .getAllByRole('button')
      .find((b) => b.textContent?.includes('Merchant'))!
    fireEvent.click(merchantBtn)
    expect(onSortChange.mock.calls[0][0]).toEqual({ columnId: 'merchant_name', direction: 'asc' })
  })

  it('renders rows in the order provided by sorted data', () => {
    // Data pre-sorted ascending by merchant_name: Amazon first
    const sortedData: SpendingByMerchantResponse = {
      ...mockData,
      merchants: [
        { merchant_name: 'Amazon', total: 350, transaction_count: 5, percentage: 70 },
        { merchant_name: 'Starbucks', total: 150, transaction_count: 15, percentage: 30 },
      ],
    }
    renderTable({ data: sortedData, sort: { columnId: 'merchant_name', direction: 'asc' } })
    const rows = screen.getAllByRole('row')
    expect(rows[1].textContent).toContain('Amazon')
  })

  it('calls onMerchantClick when row is clicked', () => {
    renderTable({ onMerchantClick: defaultMocks.onMerchantClick })
    fireEvent.click(screen.getByText('Starbucks'))
    expect(defaultMocks.onMerchantClick).toHaveBeenCalledWith('Starbucks')
  })
})
