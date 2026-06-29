/**
 * Merchants Tab E2E Tests
 * Issue #957: Merchant Name Handling — tab navigation + merchant list UI
 *
 * Exercises the happy path: navigate to /auto-categorization, switch to the
 * Merchants tab, verify the DataTable renders merchant data, and verify the
 * rename modal can be opened.
 */

import { test, expect, type Page } from '@playwright/test'
import { acceptCookieConsent } from '../helpers/auth'
import { mockAuthDependencies } from '../helpers/api-mocks'

const mockMerchant = {
  merchant_id: 'merch-1',
  effective_display_name: 'Whole Foods',
  normalized_name: 'Whole Foods',
  raw_variants: ['WHOLEFDSMRKT #123'],
  total_spend: 450.5,
  transaction_count: 12,
  dominant_category: 'FOOD_AND_DRINK',
  has_override: false,
  is_merged: false,
}

const mockOverrideMerchant = {
  merchant_id: 'merch-2',
  effective_display_name: 'My Coffee Shop',
  normalized_name: 'Coffee Shop',
  raw_variants: ['SQ *COFFEE SHOP'],
  total_spend: 87.5,
  transaction_count: 7,
  dominant_category: 'FOOD_AND_DRINK',
  has_override: true,
  is_merged: false,
}

async function mockMerchantsAPIs(page: Page) {
  await mockAuthDependencies(page)

  await page.route('**/api/v1/auto-categorization*', (route) => {
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
  })

  await page.route('**/api/v1/categories/plaid-primaries*', (route) => {
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
  })

  await page.route('**/api/v1/categories/user*', (route) => {
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
  })

  await page.route('**/api/v1/categories*', (route) => {
    route.fallback()
  })

  await page.route('**/api/v1/tags*', (route) => {
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
  })

  await page.route('**/api/v1/accounts*', (route) => {
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ accounts: [] }) })
  })

  await page.route('**/api/v1/merchants/duplicate-suggestions*', (route) => {
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
  })

  await page.route('**/api/v1/merchants*', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([mockMerchant, mockOverrideMerchant]),
    })
  })
}

test.describe('Merchants tab', () => {
  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
  })

  test('tab bar renders with Rules and Merchants tabs', async ({ page }) => {
    await mockMerchantsAPIs(page)
    await page.goto('/auto-categorization')
    await page.waitForLoadState('domcontentloaded')

    await expect(page.getByTestId('tab-rules')).toBeVisible()
    await expect(page.getByTestId('tab-merchants')).toBeVisible()
  })

  test('Merchants tab click shows merchant list', async ({ page }) => {
    await mockMerchantsAPIs(page)
    await page.goto('/auto-categorization')
    await page.waitForLoadState('domcontentloaded')

    await page.getByTestId('tab-merchants').click()

    // DataTable with merchant data should appear
    await expect(page.getByTestId('data-table')).toBeVisible()
    await expect(page.getByText('Whole Foods')).toBeVisible()
  })

  test('empty state is shown when no merchants', async ({ page }) => {
    await mockAuthDependencies(page)

    await page.route('**/api/v1/auto-categorization*', (route) => {
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
    })
    await page.route('**/api/v1/categories/**', (route) => {
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
    })
    await page.route('**/api/v1/categories*', (route) => {
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
    })
    await page.route('**/api/v1/tags*', (route) => {
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
    })
    await page.route('**/api/v1/accounts*', (route) => {
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ accounts: [] }) })
    })
    await page.route('**/api/v1/merchants/duplicate-suggestions*', (route) => {
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
    })
    await page.route('**/api/v1/merchants*', (route) => {
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
    })

    await page.goto('/auto-categorization')
    await page.waitForLoadState('domcontentloaded')

    await page.getByTestId('tab-merchants').click()

    await expect(page.getByTestId('empty-state')).toBeVisible()
  })

  test('rename modal opens when Rename action is clicked', async ({ page }) => {
    await mockMerchantsAPIs(page)
    await page.goto('/auto-categorization')
    await page.waitForLoadState('domcontentloaded')

    await page.getByTestId('tab-merchants').click()
    await page.waitForSelector('[data-testid="action-rename-0"]')

    await page.getByTestId('action-rename-0').click()
    await expect(page.getByTestId('rename-modal')).toBeVisible()
    await expect(page.getByTestId('rename-input')).toBeVisible()
  })

  // G19: assert mutation request bodies
  test('save rename fires PUT with display_name in request body', async ({ page }) => {
    // Intercept the PUT before the general merchants catch-all.
    let capturedBody: Record<string, unknown> | null = null
    await page.route('**/api/v1/merchants/merch-1/override', (route) => {
      if (route.request().method() === 'PUT') {
        capturedBody = JSON.parse(route.request().postData() ?? '{}') as Record<string, unknown>
        route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ id: 'ov-1', merchant_id: 'merch-1', display_name: 'Whole Foods Market' }) })
      } else {
        route.fallback()
      }
    })
    await mockMerchantsAPIs(page)
    await page.goto('/auto-categorization')
    await page.waitForLoadState('domcontentloaded')

    await page.getByTestId('tab-merchants').click()
    await page.waitForSelector('[data-testid="action-rename-0"]')

    await page.getByTestId('action-rename-0').click()
    await expect(page.getByTestId('rename-input')).toBeVisible()
    await page.getByTestId('rename-input').fill('Whole Foods Market')
    await page.getByTestId('rename-save-btn').click()

    await expect(async () => {
      expect(capturedBody).not.toBeNull()
      expect((capturedBody as Record<string, unknown>).display_name).toBe('Whole Foods Market')
    }).toPass({ timeout: 5000 })
  })

  test('clear override fires DELETE to correct merchant endpoint', async ({ page }) => {
    let deleteWasCalled = false
    await page.route('**/api/v1/merchants/merch-2/override', (route) => {
      if (route.request().method() === 'DELETE') {
        deleteWasCalled = true
        route.fulfill({ status: 204 })
      } else {
        route.fallback()
      }
    })
    await mockMerchantsAPIs(page)
    await page.goto('/auto-categorization')
    await page.waitForLoadState('domcontentloaded')

    await page.getByTestId('tab-merchants').click()
    await page.waitForSelector('[data-testid="action-clear-override-1"]')

    await page.getByTestId('action-clear-override-1').click()

    await expect(async () => {
      expect(deleteWasCalled).toBe(true)
    }).toPass({ timeout: 5000 })
  })

  // G41: merge flow — duplicate-suggestions Card visible; clicking Merge fires POST /merchants/merge
  // with correct merchant_ids + target_merchant_id in the request body.
  test('merge flow fires POST /merchants/merge with correct body (Issue #957 G41)', async ({ page }) => {
    const mockDuplicate = {
      merchant_id: 'merch-3',
      effective_display_name: 'WF Market',
      normalized_name: 'WF Market',
      raw_variants: ['WHOLE FOODS MARKET'],
      total_spend: 120.0,
      transaction_count: 3,
      dominant_category: 'FOOD_AND_DRINK',
      has_override: false,
      is_merged: false,
    }

    let capturedMergeBody: Record<string, unknown> | null = null
    // Catch-all mocks first (reverse registration order in Playwright means last-in, first-matched)
    await mockMerchantsAPIs(page)
    await page.route('**/api/v1/merchants/duplicate-suggestions*', (route) => {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([[mockMerchant, mockDuplicate]]),
      })
    })
    await page.route('**/api/v1/merchants/merge', (route) => {
      if (route.request().method() === 'POST') {
        capturedMergeBody = JSON.parse(route.request().postData() ?? '{}') as Record<string, unknown>
        route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
      } else {
        route.fallback()
      }
    })
    await page.goto('/auto-categorization')
    await page.waitForLoadState('domcontentloaded')

    await page.getByTestId('tab-merchants').click()

    await expect(page.getByTestId('duplicate-suggestions')).toBeVisible()
    await page.getByTestId('merge-group-0').click()
    await expect(async () => {
      expect(capturedMergeBody).not.toBeNull()
      const body = capturedMergeBody as Record<string, unknown>
      const ids = body.merchant_ids as string[]
      expect(ids).toContain('merch-1')
      expect(ids).toContain('merch-3')
      expect(body.target_merchant_id).toBe('merch-1')
    }).toPass({ timeout: 5000 })
  })

  // G41: error state when GET /merchants returns non-200
  test('error state shown when merchants API returns error (Issue #957 G41)', async ({ page }) => {
    await mockAuthDependencies(page)

    await page.route('**/api/v1/auto-categorization*', (route) => {
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
    })
    await page.route('**/api/v1/categories/**', (route) => {
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
    })
    await page.route('**/api/v1/categories*', (route) => {
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
    })
    await page.route('**/api/v1/tags*', (route) => {
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
    })
    await page.route('**/api/v1/accounts*', (route) => {
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ accounts: [] }) })
    })
    await page.route('**/api/v1/merchants/duplicate-suggestions*', (route) => {
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
    })
    // Simulate server error on merchants endpoint
    await page.route('**/api/v1/merchants*', (route) => {
      route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ message: 'Internal Server Error' }) })
    })

    await page.goto('/auto-categorization')
    await page.waitForLoadState('domcontentloaded')

    await page.getByTestId('tab-merchants').click()

    await expect(page.getByTestId('merchants-error')).toBeVisible()
    await expect(page.getByRole('button', { name: /try again/i })).toBeVisible()
    await expect(page.getByTestId('data-table')).not.toBeVisible()
  })
})
