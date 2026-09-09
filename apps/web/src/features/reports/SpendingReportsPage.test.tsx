import { describe, it, expect, beforeEach, afterEach, mock } from 'bun:test'
import { render, screen, fireEvent, within } from '@testing-library/react'
import React from 'react'
import { MemoryRouter } from 'react-router'
import {
  mockUseGetSpendingByCategory,
  mockUseGetSpendingByMerchant,
  mockUseGetSpendingTrends,
  mockUseGetIncomeSourceBreakdown,
  mockUseGetCategoryHierarchy,
} from '@/test/mocks/analytics-api'
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
    // radiogroup, not group: DateRangePicker renders <ToggleGroup type="single">.
    // See src/test/mocks/toggle-group.tsx for why the role changed.
    //
    // Identified by its own preset, not by role: #1626 added a SECOND ToggleGroup (the Sankey's
    // view-mode control), so `getAllByRole('radiogroup').length > 0` would pass even with the date
    // picker gone entirely — which is the one thing this test is named for.
    expect(screen.getByText('Last 3 Months')).toBeDefined()
    expect(screen.getAllByRole('radiogroup').length).toBeGreaterThanOrEqual(2)
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

  it('a focused category is cleared when the date range changes', () => {
    // The Sankey needs real data for there to be a node to focus.
    mockUseGetCategoryHierarchy.mockReturnValue({
      data: {
        levels: [{
          category: 'FOOD_AND_DRINK', display: 'Food And Drink', total: 300, direct_total: 100,
          percentage: 100, transaction_count: 5,
          children: [{
            category: 'FOOD_AND_DRINK_FAST_FOOD', display: 'Fast Food', total: 200,
            direct_total: 200, percentage: 66, transaction_count: 3, children: [],
          }],
        }],
        total: 300, direction: 'expense', start_date: '2024-01-01', end_date: '2024-01-31',
      },
      isLoading: false,
      error: null,
      refetch: mock(() => Promise.resolve({})),
    } as never)

    renderPage()

    // Focus a category through the chart's own click path. Targeted by label rather than by
    // position: the first button in the sr-only table is an INCOME row, which navigates.
    const sankey = screen.getByTestId('sankey-chart')
    fireEvent.click(within(sankey).getByText(/Total Income → Food And Drink/))
    expect(screen.queryByText(/Back to all categories/)).not.toBeNull()

    // Changing the date range changes the expense universe: the focused category may not exist in
    // the new period. Leaving the focus set made the control claim a focus the chart had silently
    // abandoned, since buildSankeyData falls back to a full view for an unknown key.
    fireEvent.click(screen.getByText('This Month'))
    expect(screen.queryByText(/Back to all categories/)).toBeNull()

    mockUseGetCategoryHierarchy.mockReturnValue({
      data: null, isLoading: false, error: null, refetch: mock(() => Promise.resolve({})),
    } as never)
  })

  it('the Sankey reads a different endpoint entirely, so a drilldown cannot narrow it', () => {
    renderPage()
    mockUseGetSpendingByCategory.mockClear()
    mockUseGetCategoryHierarchy.mockClear()

    // Click a PFC category in the category chart to trigger drilldown.
    const categoryChart = screen.getByTestId('category-chart')
    fireEvent.click(within(categoryChart).getByTestId('echarts-point-0')) // FOOD_AND_DRINK

    // #1626 strengthened this guard. It used to rest on the Sankey issuing a SECOND
    // spending-by-category call that merely omitted `category` — a convention two calls to one
    // endpoint had to keep. The Sankey now reads category-hierarchy instead, so the immunity is
    // structural: the drilldown param does not exist on that endpoint's input at all.
    type ParamsCall = [Record<string, unknown> | undefined, unknown]

    const drilldownCalls = mockUseGetSpendingByCategory.mock.calls as unknown as ParamsCall[]
    expect(drilldownCalls.some(([p]) => p?.category === 'FOOD_AND_DRINK')).toBe(true)

    const sankeyCalls = mockUseGetCategoryHierarchy.mock.calls as unknown as ParamsCall[]
    expect(sankeyCalls.length).toBeGreaterThan(0)
    for (const [params] of sankeyCalls) {
      expect(params?.category).toBeUndefined()
    }
    // And it still asks for both directions, which is what makes the chart's two fans consistent.
    expect(new Set(sankeyCalls.map(([p]) => p?.direction))).toEqual(new Set(['income', 'expense']))
  })
})

// Issue #1569 T9.5 — a failed read must never be rendered as "there is nothing here".
// Every section on this page reads financial data; when the read fails the page must say so
// rather than fall through to the chart's own no-data copy, which asserts the user has no
// spending in the period. All five hooks are wired through one shared render path.
describe('SpendingReportsPage failed reads', () => {
  // The mocks are typed from their default return value (`error: null`), so the failure value
  // has to be widened at the call site; the runtime shape is what react-query really returns.
  const READ_FAILED = new Error('network down') as unknown as null
  const OK = { data: null, isLoading: false, error: null, refetch: mock(() => Promise.resolve({})) }
  const FAILED = { ...OK, error: READ_FAILED }

  const restoreDefaults = () => {
    mockUseGetIncomeSourceBreakdown.mockReturnValue(OK)
    // #1626: without this the Sankey's failure leaks into the next test, which then sees two
    // alerts and cannot tell which section actually degraded.
    mockUseGetCategoryHierarchy.mockReturnValue(OK)
    mockUseGetSpendingByCategory.mockReturnValue(OK)
    mockUseGetSpendingByMerchant.mockReturnValue(OK)
    mockUseGetSpendingTrends.mockReturnValue(OK)
  }

  beforeEach(restoreDefaults)
  afterEach(restoreDefaults)

  it('reports a failed cash-flow read instead of "No cash flow data"', () => {
    // #1626: the Sankey's reads are the category-hierarchy pair, not income-sources.
    mockUseGetCategoryHierarchy.mockReturnValue(FAILED)
    renderPage()
    expect(screen.getAllByRole('alert').length).toBeGreaterThan(0)
    expect(screen.queryByText(/No cash flow data/i)).toBeNull()
    expect(screen.queryByTestId('sankey-chart')).toBeNull()
  })

  it('reports a failed category read instead of "No spending data"', () => {
    mockUseGetSpendingByCategory.mockReturnValue(FAILED)
    renderPage()
    expect(screen.getAllByRole('alert').length).toBeGreaterThan(0)
    expect(screen.queryByText(/No spending data/i)).toBeNull()
    expect(screen.queryByTestId('category-chart')).toBeNull()
  })

  it('reports a failed merchant read instead of "No merchant data"', () => {
    mockUseGetSpendingByMerchant.mockReturnValue(FAILED)
    renderPage()
    expect(screen.getAllByRole('alert').length).toBeGreaterThan(0)
    expect(screen.queryByText(/No merchant data/i)).toBeNull()
    expect(screen.queryByTestId('merchants-table')).toBeNull()
  })

  it('reports a failed trends read instead of "No trend data"', () => {
    mockUseGetSpendingTrends.mockReturnValue(FAILED)
    renderPage()
    expect(screen.getAllByRole('alert').length).toBeGreaterThan(0)
    expect(screen.queryByText(/No trend data/i)).toBeNull()
    expect(screen.queryByTestId('trends-chart')).toBeNull()
  })

  it('leaves the sections whose reads succeeded intact', () => {
    mockUseGetSpendingTrends.mockReturnValue(FAILED)
    renderPage()
    // Only the trends section degrades; the others still render their own charts.
    expect(screen.getByTestId('category-chart')).toBeDefined()
    expect(screen.getByTestId('merchants-table')).toBeDefined()
    expect(screen.getAllByRole('alert').length).toBe(1)
  })
})
