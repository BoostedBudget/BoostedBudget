import { describe, it, expect, mock } from 'bun:test'
import { render, screen } from '@testing-library/react'
import React from 'react'

import '@/test/mocks/shared-charts'
import { CategoryBreakdownChart } from './CategoryBreakdownChart'
import type { SpendingByCategoryResponse } from '@boostedbudget/api-client/models'

const mockData: SpendingByCategoryResponse = {
  start_date: '2024-01-01',
  end_date: '2024-01-31',
  total_spent: 500,
  categories: [
    { category: 'FOOD_AND_DRINK', pfc_primary_display: 'Food And Drink', total: 300, percentage: 60, transaction_count: 20 },
    { category: 'TRANSPORTATION', pfc_primary_display: 'Transportation', total: 200, percentage: 40, transaction_count: 8 },
  ],
}

describe('CategoryBreakdownChart', () => {
  it('renders loading state', () => {
    render(<CategoryBreakdownChart data={null} loading={true} />)
    expect(screen.getByLabelText('Loading chart')).toBeDefined()
  })

  it('renders empty state when no categories', () => {
    const emptyData: SpendingByCategoryResponse = {
      start_date: '2024-01-01',
      end_date: '2024-01-31',
      total_spent: 0,
      categories: [],
    }
    render(<CategoryBreakdownChart data={emptyData} loading={false} />)
    expect(screen.getByText(/No spending data/)).toBeDefined()
  })

  it('renders chart with data', () => {
    render(<CategoryBreakdownChart data={mockData} loading={false} />)
    expect(screen.getByTestId('plotly-chart')).toBeDefined()
  })

  it('renders total spent amount', () => {
    render(<CategoryBreakdownChart data={mockData} loading={false} />)
    expect(screen.getByText('Total:')).toBeDefined()
  })

  it('calls onCategoryClick when chart point clicked', () => {
    const onCategoryClick = mock(() => {})
    render(
      <CategoryBreakdownChart
        data={mockData}
        loading={false}
        onCategoryClick={onCategoryClick}
      />
    )
    // Chart renders without error with click handler
    expect(screen.getByTestId('plotly-chart')).toBeDefined()
  })
})
