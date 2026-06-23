import { describe, it, expect, mock, beforeEach, afterEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import React from 'react'
import { MemoryRouter } from 'react-router-dom'
import { mockUseGetSpendingByCategory, mockUseGetSpendingByMerchant } from '@/test/mocks/analytics-api'
import { mockUseGetUserCategories } from '@/test/mocks/categories-api'
import { mockNavigate } from '@/test/mocks/react-router'
import type { SpendingByCategoryResponse } from '@boostedbudget/api-client/models'
import type { SpendingByMerchantResponse } from '@boostedbudget/api-client/models'
import type { CustomCategory } from '@boostedbudget/api-client/models'

import '@/test/mocks/shared-charts'
import '@/test/mocks/react-router'
import { mockTrackEvent } from '@/test/mocks/track-event'

// Mock toggle-group for DateRangePicker.
// Uses event-delegation so onValueChange fires when a ToggleGroupItem is clicked —
// this makes the mock functional (not a hollow stub) so tests in other files that rely
// on ToggleGroup value-change behavior (e.g. PortfolioPage.test.tsx) are not broken
// when this mock is the last registered in the shared bun worker process.
mock.module('@/components/ui/toggle-group', () => ({
  ToggleGroup: ({ children, onValueChange }: { children: React.ReactNode; onValueChange?: (v: string) => void }) =>
    React.createElement('div', {
      role: 'group',
      'data-testid': 'toggle-group',
      onClick: (e: React.MouseEvent<HTMLDivElement>) => {
        const btn = (e.target as HTMLElement).closest('[data-value]') as HTMLElement | null
        if (btn?.dataset.value) onValueChange?.(btn.dataset.value)
      },
    }, children),
  ToggleGroupItem: ({ children, value }: { children: React.ReactNode; value: string }) =>
    React.createElement('button', { 'data-value': value }, children),
}))

import { SpendingReportsPage } from './SpendingReportsPage'

const renderPage = () =>
  render(
    <MemoryRouter>
      <SpendingReportsPage />
    </MemoryRouter>
  )

describe('SpendingReportsPage', () => {
  it('renders page heading', () => {
    renderPage()
    expect(screen.getByText('Spending Reports')).toBeDefined()
  })

  it('renders date range picker', () => {
    renderPage()
    expect(screen.getByRole('group')).toBeDefined()
  })

  it('renders category breakdown section', () => {
    renderPage()
    expect(screen.getByText('Spending by Category')).toBeDefined()
  })

  it('renders spending trends section', () => {
    renderPage()
    expect(screen.getByText('Spending Trends')).toBeDefined()
  })

  it('renders top merchants section', () => {
    renderPage()
    expect(screen.getByText('Top Merchants')).toBeDefined()
  })

  it('renders all three chart/table components', () => {
    renderPage()
    expect(screen.getByTestId('category-chart')).toBeDefined()
    expect(screen.getByTestId('trends-chart')).toBeDefined()
    expect(screen.getByTestId('merchants-table')).toBeDefined()
  })
})

describe('SpendingReportsPage drill-down guard', () => {
  const customCategoryData: SpendingByCategoryResponse = {
    start_date: '2024-01-01',
    end_date: '2024-03-31',
    total_spent: 150,
    categories: [
      {
        category: 'custom:uuid-123',
        pfc_primary_display: 'My Custom Cat',
        total: 100,
        percentage: 67,
        transaction_count: 5,
      },
      {
        category: 'FOOD_AND_DRINK',
        pfc_primary_display: 'Food And Drink',
        total: 50,
        percentage: 33,
        transaction_count: 3,
      },
    ],
  }

  beforeEach(() => {
    mockUseGetSpendingByCategory.mockReturnValue({
      data: customCategoryData,
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve({}),
    })
  })

  afterEach(() => {
    mockUseGetSpendingByCategory.mockClear()
  })

  it('does not enter drilldown when a custom-category slice is clicked', () => {
    renderPage()
    // echarts-point-0 corresponds to customCategoryData.categories[0] = 'custom:uuid-123'
    fireEvent.click(screen.getByTestId('echarts-point-0'))
    // guard should prevent drilldown; description must stay on the top-level prompt
    expect(screen.queryByText('Click a subcategory to view its transactions.')).toBeNull()
    expect(screen.getByText('Click a category to drill into subcategories.')).toBeDefined()
  })

  it('enters drilldown when a PFC category slice is clicked', () => {
    renderPage()
    // echarts-point-1 corresponds to customCategoryData.categories[1] = 'FOOD_AND_DRINK'
    fireEvent.click(screen.getByTestId('echarts-point-1'))
    expect(screen.getByText('Click a subcategory to view its transactions.')).toBeDefined()
  })
})

describe('SpendingReportsPage merchant click navigation', () => {
  const merchantWithId: SpendingByMerchantResponse = {
    start_date: '2024-01-01',
    end_date: '2024-01-31',
    top_n: 10,
    merchants: [
      { merchant_id: 'uuid-merchant-123', merchant_name: 'Starbucks', total: 150, transaction_count: 5, percentage: 100 },
    ],
  }

  const merchantWithoutId: SpendingByMerchantResponse = {
    start_date: '2024-01-01',
    end_date: '2024-01-31',
    top_n: 10,
    merchants: [
      { merchant_name: 'Unknown Merchant', total: 50, transaction_count: 2, percentage: 100 },
    ],
  }

  beforeEach(() => {
    mockNavigate.mockClear()
    // Ensure category and trends mocks return valid (non-null) values
    mockUseGetSpendingByCategory.mockReturnValue({
      data: null,
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve({}),
    })
  })

  afterEach(() => {
    mockUseGetSpendingByMerchant.mockClear()
    mockUseGetSpendingByCategory.mockClear()
  })

  it('navigates using merchant_id (not merchant name) when id is present', () => {
    mockUseGetSpendingByMerchant.mockReturnValue({
      data: merchantWithId,
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve({}),
    })

    renderPage()

    // Click the merchant row
    fireEvent.click(screen.getByText('Starbucks'))

    // Must navigate with merchant_id param, NOT merchant name
    expect(mockNavigate).toHaveBeenCalledTimes(1)
    const [url, opts] = mockNavigate.mock.calls[0] as [string, { state: { merchantName: string } }]
    expect(url).toContain('merchant_id=uuid-merchant-123')
    expect(url).not.toContain('search=')
    expect(url).not.toContain('Starbucks')
    // Merchant name goes in router state, not URL
    expect(opts?.state?.merchantName).toBe('Starbucks')
  })

  it('skips navigation when merchant has no id', () => {
    mockUseGetSpendingByMerchant.mockReturnValue({
      data: merchantWithoutId,
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve({}),
    })

    renderPage()

    // Click the merchant row
    fireEvent.click(screen.getByText('Unknown Merchant'))

    // No navigation should happen when merchant_id is absent
    expect(mockNavigate).not.toHaveBeenCalled()
  })
})

describe('SpendingReportsPage custom category filter', () => {
  const customCats = [
    { id: 'cc-1', user_id: 'u1', name: 'Travel', usage_count: 0, created_at: '', updated_at: '' },
    { id: 'cc-2', user_id: 'u1', name: 'Pets', usage_count: 0, created_at: '', updated_at: '' },
  ] as CustomCategory[]

  beforeEach(() => {
    mockUseGetUserCategories.mockReturnValue({
      data: customCats,
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve({}),
    })
    mockUseGetSpendingByCategory.mockReturnValue({
      data: null,
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve({}),
    })
    // The merchant block's afterEach resets this mock to return undefined; restore a safe
    // value so the full page renders (it always calls the merchant hook).
    mockUseGetSpendingByMerchant.mockReturnValue({
      data: null,
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve({}),
    })
  })

  afterEach(() => {
    mockUseGetUserCategories.mockClear()
    mockUseGetSpendingByCategory.mockClear()
    mockUseGetSpendingByMerchant.mockClear()
  })

  it('passes custom_category_ids to the spending query when a custom category is selected', () => {
    renderPage()

    fireEvent.click(screen.getByRole('button', { name: /Travel/i }))

    const calls = mockUseGetSpendingByCategory.mock.calls as unknown as Array<[{ custom_category_ids?: string } | undefined]>
    const lastParams = calls[calls.length - 1]?.[0]
    expect(lastParams?.custom_category_ids).toBe('cc-1')
  })
})
