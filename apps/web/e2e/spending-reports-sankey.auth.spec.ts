/**
 * Cash Flow Sankey Chart E2E Tests — Issue #1630
 *
 * `sankey` appears in no testid anywhere else under e2e/ (verified twice per the task brief) —
 * spending-reports-table.auth.spec.ts visits /reports but asserts only the category and trends
 * charts. This spec closes that gap and pins the three rendering defects Round 2 fixed:
 *
 *   - 7016bff3: ECharts read our analytics `depth` field as its own layout LAYER, inverting the
 *     diagram (income leaves scattered among expense columns instead of on the left).
 *   - 99387dcf: ECharts' default `nodeAlign: 'justify'` stretched every leaf expense parent to the
 *     chart's rightmost column instead of letting it terminate at its own depth.
 *   - 2575d8f5: the deficit node is derived from ONE signed `surplus` value, so "Savings" and
 *     "Shortfall" can never both render for the same period.
 *
 * Route-mocked (page.route), matching its neighbour spending-reports-table.auth.spec.ts — every
 * assertion here (column position, mutual exclusivity, toggle-driven depth) is observable in the
 * real rendered SVG (EChartsChart forces `renderer: 'svg'` specifically so PostHog can mask it and
 * so markup like this is inspectable) without a live backend.
 */

import { test, expect } from '@playwright/test'
import { acceptCookieConsent } from './helpers/auth'
import { mockAuthDependencies } from './helpers/api-mocks'
import type { Page } from '@playwright/test'

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

/**
 * A category-hierarchy tree with enough shape to exercise every assertion:
 *   - two income leaves (so "left of Total Income" is checkable against more than one node)
 *   - one expense parent WITH children (Food -> Groceries, Restaurants) so the "Subcategories"
 *     toggle has somewhere to expand into
 *   - one expense parent with NO children (Shopping) so its terminal column can be compared
 *     against a deeper terminal node (Groceries/Restaurants) to prove nodeAlign: 'left'
 */
function incomeTree(surplus: 'positive' | 'negative') {
  // Total income is fixed at 5000; expenses (below) sum to 1500, leaving a surplus.
  // For the negative case, expenses instead sum to 6000, producing a shortfall.
  return [
    {
      category: 'INCOME_WAGES',
      display: 'Wages',
      total: surplus === 'positive' ? 4500 : 4500,
      direct_total: surplus === 'positive' ? 4500 : 4500,
      percentage: 90,
      transaction_count: 2,
      children: null,
    },
    {
      category: 'INCOME_DIVIDENDS',
      display: 'Dividends',
      total: 500,
      direct_total: 500,
      percentage: 10,
      transaction_count: 1,
      children: null,
    },
  ]
}

function expenseTree(surplus: 'positive' | 'negative') {
  // 'positive': Food 900 (Groceries 600 + Restaurants 300) + Shopping 600 = 1500 total (< 5000 income)
  // 'negative': Food 4500 (Groceries 3000 + Restaurants 1500) + Shopping 1500 = 6000 (> 5000 income)
  const foodTotal = surplus === 'positive' ? 900 : 4500
  const groceries = surplus === 'positive' ? 600 : 3000
  const restaurants = surplus === 'positive' ? 300 : 1500
  const shopping = surplus === 'positive' ? 600 : 1500

  return [
    {
      category: 'FOOD_AND_DRINK',
      display: 'Food and Drink',
      total: foodTotal,
      direct_total: 0,
      percentage: 60,
      transaction_count: 10,
      children: [
        {
          category: 'FOOD_AND_DRINK_GROCERIES',
          display: 'Groceries',
          total: groceries,
          direct_total: groceries,
          percentage: 40,
          transaction_count: 6,
          children: null,
        },
        {
          category: 'FOOD_AND_DRINK_RESTAURANTS',
          display: 'Restaurants',
          total: restaurants,
          direct_total: restaurants,
          percentage: 20,
          transaction_count: 4,
          children: null,
        },
      ],
    },
    {
      // A leaf PARENT — no children at all — so it must terminate at ITS OWN column (depth 1)
      // rather than being stretched to the rightmost column the deeper Food chain occupies.
      category: 'GENERAL_MERCHANDISE',
      display: 'Shopping',
      total: shopping,
      direct_total: shopping,
      percentage: 40,
      transaction_count: 5,
      children: null,
    },
  ]
}

async function mockSankeyDependencies(page: Page, surplus: 'positive' | 'negative') {
  await mockAuthDependencies(page)

  await page.route('**/api/v1/analytics/category-hierarchy*', (route) => {
    const url = new URL(route.request().url())
    const direction = url.searchParams.get('direction')
    const levels = direction === 'income' ? incomeTree(surplus) : expenseTree(surplus)
    const total = levels.reduce((s, n) => s + n.total, 0)
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        direction,
        start_date: '2026-02-12',
        end_date: '2026-05-12',
        total,
        levels,
      }),
    })
  })

  // Other reports-page reads — irrelevant to the Sankey but must be mocked or the page's other
  // sections fall through to readOrError and never settle into a stable state.
  await page.route('**/api/v1/analytics/spending-by-merchant*', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ start_date: '2026-02-12', end_date: '2026-05-12', top_n: 10, merchants: [] }),
    })
  })
  await page.route('**/api/v1/analytics/spending-by-category*', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ start_date: '2026-02-12', end_date: '2026-05-12', total_spent: 0, categories: [] }),
    })
  })
  await page.route('**/api/v1/analytics/spending-trends*', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ start_date: '2026-02-12', end_date: '2026-05-12', top_categories: [], periods: [] }),
    })
  })
}

/** The sr-only data table gives us reliable text content without depending on ECharts' SVG
 *  internals — used for existence/exclusivity checks. Position checks (below) need the real SVG
 *  text nodes ECharts renders, since the sr-only table's DOM order is link order, not layout
 *  column. */
async function sankeyChart(page: Page) {
  const chart = page.getByTestId('sankey-chart')
  await expect(chart).toBeVisible({ timeout: 10000 })
  await expect(chart).not.toHaveAttribute('aria-busy', 'true')
  return chart
}

/** Bounding-box x of the first SVG <text> node whose content matches `label` inside the chart. */
async function nodeX(page: Page, label: string): Promise<number> {
  const chart = page.getByTestId('sankey-chart')
  const text = chart.locator('svg text', { hasText: label }).first()
  await expect(text).toBeVisible({ timeout: 10000 })
  const box = await text.boundingBox()
  expect(box, `no bounding box for Sankey label "${label}"`).toBeTruthy()
  return box!.x
}

test('Uncategorized remains visible and navigates to posted uncategorized transactions (#1627)', async ({ page }) => {
  await acceptCookieConsent(page)
  await mockSankeyDependencies(page, 'positive')
  await page.route('**/api/v1/analytics/category-hierarchy*', async (route) => {
    const direction = new URL(route.request().url()).searchParams.get('direction')
    const levels = direction === 'income' ? incomeTree('positive') : [{
      category: 'UNCATEGORIZED', display: 'Uncategorized', total: 120, direct_total: 120,
      percentage: 100, transaction_count: 2, children: null,
    }]
    await route.fulfill({ json: { direction, start_date: '2026-02-12', end_date: '2026-05-12', total: levels.reduce((sum, node) => sum + node.total, 0), levels } })
  })
  await page.goto('/reports')
  const chart = await sankeyChart(page)
  await expect(chart.locator('svg text', { hasText: 'Uncategorized' }).first()).toBeVisible()
  await chart.getByRole('button', { name: /Total Income → Uncategorized/ }).focus()
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/\/transactions\?/)
  const params = new URL(page.url()).searchParams
  expect(params.get('uncategorized')).toBe('true')
  expect(params.get('pending')).toBe('false')
  expect(params.has('pfc_primaries')).toBe(false)
  expect(params.has('pfc_detaileds')).toBe(false)
  expect(params.has('custom_category_ids')).toBe(false)
})

// ---------------------------------------------------------------------------
// Layout direction: income -> Total Income -> expenses, left to right
// ---------------------------------------------------------------------------

test.describe('Cash Flow Sankey — layout direction (#1630, defect 7016bff3)', () => {
  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
  })

  test('income nodes render left of Total Income, which renders left of expense parents', async ({ page }) => {
    await mockSankeyDependencies(page, 'positive')
    await page.goto('/reports')
    await page.waitForLoadState('networkidle')
    await sankeyChart(page)

    const wagesX = await nodeX(page, 'Wages')
    const hubX = await nodeX(page, 'Total Income')
    const foodX = await nodeX(page, 'Food and Drink')

    expect(wagesX, 'income leaf must render left of the Total Income hub').toBeLessThan(hubX)
    expect(hubX, 'Total Income hub must render left of an expense parent').toBeLessThan(foodX)
  })
})

// ---------------------------------------------------------------------------
// Terminal column: a leaf expense parent stops at its own depth (99387dcf, nodeAlign: 'left')
// ---------------------------------------------------------------------------

test.describe('Cash Flow Sankey — terminal column (#1630, defect 99387dcf)', () => {
  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
  })

  test('a leaf expense parent terminates at its own column, not stretched to the deepest column', async ({ page }) => {
    await mockSankeyDependencies(page, 'positive')
    await page.goto('/reports')
    await page.waitForLoadState('networkidle')
    await sankeyChart(page)

    // Switch to Subcategories so Food's children (deeper, depth 2) are actually rendered —
    // otherwise there is no deeper terminal column to compare Shopping (depth 1) against.
    await page.getByTestId('sankey-view-mode-control').getByText('Subcategories').click()
    await sankeyChart(page)

    const shoppingX = await nodeX(page, 'Shopping') // leaf parent, terminal at depth 1
    const groceriesX = await nodeX(page, 'Groceries') // terminal at depth 2, under Food and Drink

    expect(
      shoppingX,
      'a leaf expense parent (no children) must sit at an earlier column than a node one level ' +
        "deeper — nodeAlign: 'left' terminates it at its own depth instead of justifying it to " +
        'the rightmost occupied column'
    ).toBeLessThan(groceriesX)
  })
})

// ---------------------------------------------------------------------------
// Saved vs Shortfall mutual exclusivity (2575d8f5)
// ---------------------------------------------------------------------------

test.describe('Cash Flow Sankey — Saved/Shortfall exclusivity (#1630, defect 2575d8f5)', () => {
  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
  })

  test('a surplus period renders Savings and never Shortfall', async ({ page }) => {
    await mockSankeyDependencies(page, 'positive')
    await page.goto('/reports')
    await page.waitForLoadState('networkidle')
    const chart = await sankeyChart(page)

    await expect(chart.locator('svg text', { hasText: 'Savings' })).toHaveCount(1)
    await expect(chart.locator('svg text', { hasText: 'Shortfall' })).toHaveCount(0)
  })

  test('a shortfall period renders Shortfall and never Savings', async ({ page }) => {
    await mockSankeyDependencies(page, 'negative')
    await page.goto('/reports')
    await page.waitForLoadState('networkidle')
    const chart = await sankeyChart(page)

    await expect(chart.locator('svg text', { hasText: 'Shortfall' })).toHaveCount(1)
    await expect(chart.locator('svg text', { hasText: 'Savings' })).toHaveCount(0)
  })
})

// ---------------------------------------------------------------------------
// Subcategories toggle changes rendered depth
// ---------------------------------------------------------------------------

test.describe('Cash Flow Sankey — Subcategories toggle depth', () => {
  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
  })

  test('toggling to Subcategories reveals a deeper node that was not rendered before', async ({ page }) => {
    await mockSankeyDependencies(page, 'positive')
    await page.goto('/reports')
    await page.waitForLoadState('networkidle')
    const chart = await sankeyChart(page)

    // Unfocused, "Categories" (parents-only) mode: only top-level parents render, so Groceries
    // (a depth-2 child of Food and Drink) is not on the chart at all.
    await expect(chart.locator('svg text', { hasText: 'Groceries' })).toHaveCount(0)

    await page.getByTestId('sankey-view-mode-control').getByText('Subcategories').click()
    await sankeyChart(page)

    // Expanded mode renders the deeper level.
    await expect(chart.locator('svg text', { hasText: 'Groceries' })).toHaveCount(1)
  })
})
