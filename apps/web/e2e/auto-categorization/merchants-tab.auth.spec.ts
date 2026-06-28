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

  // G19: assert actual mutation request bodies (not just "modal opened").
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
})
