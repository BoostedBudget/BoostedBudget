import { describe, it, expect, beforeEach, afterEach } from 'bun:test'
import { render, screen, fireEvent, within } from '@testing-library/react'
import React from 'react'
import { MemoryRouter } from 'react-router'
import { mockUseGetSpendingByCategory, mockUseGetSpendingByMerchant } from '@/test/mocks/analytics-api'
import { mockUseGetUserCategories } from '@/test/mocks/categories-api'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import type { SpendingByCategoryResponse } from '@boostedbudget/api-client/models'
import type { SpendingByMerchantResponse } from '@boostedbudget/api-client/models'
import type { CustomCategory } from '@boostedbudget/api-client/models'

import '@/test/mocks/shared-charts'

// Issue #1323: toggle-group is centrally mocked via bunfig.toml preload (toggle-group.tsx)

import { createLocationHarness } from '@/test/utils/locationProbe'
import { SpendingReportsPage } from './SpendingReportsPage'

// SpendingReportsPage calls useNavigate() unconditionally, so every render
// needs a real Router ancestor. A location probe rendered alongside the
// page lets tests assert on real navigation instead of a mocked fn.
const { locationRef, LocationProbe, currentUrl, navigationCount, reset: resetLocationProbe } = createLocationHarness()

const renderPage = () =>
  render(
    <MemoryRouter>
      <LocationProbe />
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
    // radiogroup, not group: DateRangePicker renders <ToggleGroup type="single">, and
    // @radix-ui/react-toggle-group 1.1.19 corrected the single-select container role from
    // "group" to "radiogroup" (radio children require a radiogroup owner). This assertion
    // previously passed only because the shared ToggleGroup mock hardcoded role="group";
    // the mock now mirrors real Radix, so this queries what production actually renders.
    expect(screen.getByRole('radiogroup')).toBeDefined()
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

  it('renders Cash Flow section heading', () => {
    renderPage()
    expect(screen.getByText('Cash Flow')).toBeDefined()
  })

  it('renders sankey chart component', () => {
    renderPage()
    expect(screen.getByTestId('sankey-chart')).toBeDefined()
  })

  it('renders privacy toggle button', () => {
    renderPage()
    expect(screen.getByRole('button', { name: /hide amounts/i })).toBeDefined()
  })

  it('toggles privacy mode when privacy button is clicked', () => {
    renderPage()
    const btn = screen.getByRole('button', { name: /hide amounts/i })
    fireEvent.click(btn)
    expect(screen.getByRole('button', { name: /show amounts/i })).toBeDefined()
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
    // Scope to category-chart to avoid matching Sankey trigger buttons
    const categoryChart = screen.getByTestId('category-chart')
    // echarts-point-0 within category-chart = customCategoryData.categories[0] = 'custom:uuid-123'
    fireEvent.click(within(categoryChart).getByTestId('echarts-point-0'))
    // guard should prevent drilldown; description must stay on the top-level prompt
    expect(screen.queryByText('Click a subcategory to view its transactions.')).toBeNull()
    expect(screen.getByText('Click a category to drill into subcategories.')).toBeDefined()
  })

  it('enters drilldown when a PFC category slice is clicked', () => {
    renderPage()
    // Scope to category-chart to avoid matching Sankey trigger buttons
    const categoryChart = screen.getByTestId('category-chart')
    // echarts-point-1 within category-chart = customCategoryData.categories[1] = 'FOOD_AND_DRINK'
    fireEvent.click(within(categoryChart).getByTestId('echarts-point-1'))
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
    resetLocationProbe()
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
    expect(navigationCount()).toBe(1)
    const url = currentUrl()
    expect(url).toContain('merchant_id=uuid-merchant-123')
    expect(url).not.toContain('search=')
    expect(url).not.toContain('Starbucks')
    // Merchant name goes in router state, not URL
    expect((locationRef.current.state as { merchantName: string } | null)?.merchantName).toBe('Starbucks')
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
    const initialUrl = currentUrl()
    fireEvent.click(screen.getByText('Unknown Merchant'))

    // No navigation should happen when merchant_id is absent
    expect(navigationCount()).toBe(0)
    expect(currentUrl()).toBe(initialUrl)
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

describe('SpendingReportsPage privacy toggle analytics (G2)', () => {
  beforeEach(() => {
    mockTrackEvent.mockClear()
  })

  it('fires REPORT_SANKEY_PRIVACY_TOGGLED with enabled:true when toggling from visible to hidden', () => {
    renderPage()
    fireEvent.click(screen.getByRole('button', { name: /hide amounts/i }))
    expect(mockTrackEvent).toHaveBeenCalledWith(
      ANALYTICS_EVENTS.REPORT_SANKEY_PRIVACY_TOGGLED,
      { enabled: true },
    )
  })

  it('fires REPORT_SANKEY_PRIVACY_TOGGLED with enabled:false when toggling back to visible', () => {
    renderPage()
    fireEvent.click(screen.getByRole('button', { name: /hide amounts/i })) // → hidden
    mockTrackEvent.mockClear()
    fireEvent.click(screen.getByRole('button', { name: /show amounts/i })) // → visible
    expect(mockTrackEvent).toHaveBeenCalledWith(
      ANALYTICS_EVENTS.REPORT_SANKEY_PRIVACY_TOGGLED,
      { enabled: false },
    )
  })
})

describe('SpendingReportsPage Sankey drilldown immunity (G3 — Codex fix #4 regression guard)', () => {
  const pfcCategoryData: SpendingByCategoryResponse = {
    start_date: '2024-01-01',
    end_date: '2024-03-31',
    total_spent: 2000,
    categories: [
      {
        category: 'FOOD_AND_DRINK',
        pfc_primary_display: 'Food And Drink',
        total: 1200,
        percentage: 60,
        transaction_count: 20,
      },
      {
        category: 'TRANSPORTATION',
        pfc_primary_display: 'Transportation',
        total: 800,
        percentage: 40,
        transaction_count: 8,
      },
    ],
  }

  beforeEach(() => {
    mockUseGetSpendingByCategory.mockReturnValue({
      data: pfcCategoryData,
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve({}),
    })
  })

  afterEach(() => {
    mockUseGetSpendingByCategory.mockClear()
  })

  it('Sankey top-level fetch remains without category param after a drilldown click', () => {
    renderPage()
    mockUseGetSpendingByCategory.mockClear()

    // Click a PFC category in the category chart to trigger drilldown
    const categoryChart = screen.getByTestId('category-chart')
    fireEvent.click(within(categoryChart).getByTestId('echarts-point-0')) // FOOD_AND_DRINK

    // After re-render: two useSpendingByCategory calls happen simultaneously —
    // one for the Sankey (no category) and one for the category chart (with category).
    type ParamsCall = [{ category?: string } | undefined, unknown]
    const calls = mockUseGetSpendingByCategory.mock.calls as ParamsCall[]

    // At least one call should have category === undefined (the Sankey top-level fetch).
    const hasSankeyTopLevelCall = calls.some(
      ([params]) => params != null && params.category === undefined,
    )
    // And at least one call should have category set (the drilldown chart fetch).
    const hasDrilldownCall = calls.some(
      ([params]) => params?.category === 'FOOD_AND_DRINK',
    )

    expect(hasSankeyTopLevelCall).toBe(true)
    expect(hasDrilldownCall).toBe(true)
  })
})
