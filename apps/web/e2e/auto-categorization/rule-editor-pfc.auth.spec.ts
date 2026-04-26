/**
 * Auto-Categorization Rule Editor — PFC Condition E2E Tests
 *
 * Verifies the two new PFC condition types introduced in Issue #1060:
 * - existing_pfc_primary: dropdown shows curated PFC primaries; saves raw enum
 * - existing_pfc_detailed: cascading two-stage selector; lazy-loads detailed endpoint
 *
 * Issue #1064: PFC test coverage for AutoCategorizationRuleEditor + e2e specs
 */

import { test, expect, type Page } from '@playwright/test'
import { acceptCookieConsent } from '../helpers/auth'
import { mockAuthDependencies, ALL_PFC_PRIMARIES, FOOD_AND_DRINK_DETAILED } from '../helpers/api-mocks'

const mockRule = {
  id: 'rule-1',
  user_id: 'user-123',
  name: 'Existing Rule',
  description: '',
  enabled: true,
  priority: 10,
  conditions: [{ type: 'merchant_contains', value: 'starbucks' }],
  condition_logic: 'AND',
  target_category: ['Food And Drink'],
  target_tags: [],
  tag_behavior: 'add',
  source: 'manual',
  match_count: 0,
  override_count: 0,
  last_matched_at: null,
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-01T00:00:00Z',
}

async function mockRuleEditorAPIs(page: Page, options: { withDetailed?: boolean } = {}) {
  await mockAuthDependencies(page)

  await page.route('**/api/v1/auto-categorization', (route) => {
    if (route.request().method() === 'GET') {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([mockRule]),
      })
    } else if (route.request().method() === 'POST') {
      const body = route.request().postDataJSON()
      route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify({ ...mockRule, id: 'new-rule', ...body }),
      })
    } else {
      route.continue()
    }
  })

  await page.route('**/api/v1/categories/plaid-primaries*', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(ALL_PFC_PRIMARIES),
    })
  })

  if (options.withDetailed) {
    await page.route('**/api/v1/categories/plaid-detailed*', (route) => {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(FOOD_AND_DRINK_DETAILED),
      })
    })
  }

  await page.route('**/api/v1/categories/user*', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([]),
    })
  })

  await page.route('**/api/v1/categories*', (route) => {
    if (!route.request().url().includes('plaid') && !route.request().url().includes('user')) {
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
    } else {
      route.continue()
    }
  })

  await page.route('**/api/v1/tags*', (route) => {
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
  })

  await page.route('**/api/v1/accounts*', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ accounts: [] }),
    })
  })

  await page.route('**/api/v1/auto-categorization/**', (route) => {
    route.continue()
  })
}

test.describe('Rule Editor — PFC condition types', () => {
  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
  })

  test('Scenario A: existing_pfc_primary dropdown shows curated enums and saves raw enum', async ({
    page,
  }) => {
    let savedBody: Record<string, unknown> | null = null

    await mockRuleEditorAPIs(page)

    // Capture the save request to verify the raw enum is persisted
    await page.route('**/api/v1/auto-categorization', async (route) => {
      if (route.request().method() === 'POST') {
        savedBody = route.request().postDataJSON()
        route.fulfill({
          status: 201,
          contentType: 'application/json',
          body: JSON.stringify({ ...mockRule, id: 'new-rule', ...savedBody }),
        })
      } else {
        route.continue()
      }
    })

    await page.goto('/auto-categorization')
    await page.waitForLoadState('domcontentloaded')

    // Open create rule dialog
    await page.getByRole('button', { name: /create rule/i }).click()

    // Fill rule name
    await page.getByLabel(/rule name/i).fill('PFC Primary Test Rule')

    // Add a condition
    await page.getByRole('button', { name: /add condition/i }).click()

    // Change condition type to existing_pfc_primary
    const conditionTypeSelect = page.getByLabel(/condition type/i)
    await conditionTypeSelect.selectOption('existing_pfc_primary')

    // The PFC primary dropdown should appear
    const pfcPrimaryDropdown = page.getByRole('combobox', { name: /select pfc primary$/i })
    await expect(pfcPrimaryDropdown).toBeVisible()

    // Open the dropdown and verify curated primaries are shown (not raw enums)
    await pfcPrimaryDropdown.click()
    await expect(page.getByRole('option', { name: 'Food And Drink' })).toBeVisible()
    await expect(page.getByRole('option', { name: 'Loan Payments' })).toBeVisible()
    // Raw enum should NOT appear as an option label
    await expect(page.getByRole('option', { name: 'FOOD_AND_DRINK' })).not.toBeVisible()

    // Pick Food And Drink
    await page.getByRole('option', { name: 'Food And Drink' }).click()

    // Set target category
    const targetDropdown = page.getByRole('combobox', { name: /target category/i })
    await targetDropdown.click()
    await page.getByRole('option', { name: 'Food And Drink' }).first().click()

    // Save the rule
    await page.getByRole('button', { name: /save/i }).click()

    // Verify the saved body has the raw enum as condition value (not display string)
    await expect(async () => {
      expect(savedBody).not.toBeNull()
      const conditions = (savedBody as { conditions: Array<{ type: string; value: string }> }).conditions
      const pfcCondition = conditions.find((c) => c.type === 'existing_pfc_primary')
      expect(pfcCondition).toBeDefined()
      expect(pfcCondition?.value).toBe('FOOD_AND_DRINK')
    }).toPass({ timeout: 5000 })
  })

  test('Scenario B: existing_pfc_detailed cascading selector — primary then children', async ({
    page,
  }) => {
    await mockRuleEditorAPIs(page, { withDetailed: true })

    await page.goto('/auto-categorization')
    await page.waitForLoadState('domcontentloaded')

    // Open create rule dialog
    await page.getByRole('button', { name: /create rule/i }).click()

    // Fill rule name
    await page.getByLabel(/rule name/i).fill('PFC Detailed Test Rule')

    // Add a condition
    await page.getByRole('button', { name: /add condition/i }).click()

    // Change condition type to existing_pfc_detailed
    const conditionTypeSelect = page.getByLabel(/condition type/i)
    await conditionTypeSelect.selectOption('existing_pfc_detailed')

    // Stage 1: primary dropdown should appear and be enabled (detailed data loaded)
    const primaryDropdown = page.getByRole('combobox', { name: /select pfc primary for detailed/i })
    await expect(primaryDropdown).toBeVisible()
    await expect(primaryDropdown).not.toBeDisabled()

    // Stage 2 should NOT appear yet (no primary selected)
    await expect(page.getByRole('combobox', { name: /select pfc detailed/i })).not.toBeVisible()

    // Pick Food And Drink as primary
    await primaryDropdown.click()
    await expect(page.getByRole('option', { name: 'Food And Drink' })).toBeVisible()
    // Transportation should also be in stage 1 (it has children in our mock)
    await expect(page.getByRole('option', { name: 'Transportation' })).toBeVisible()
    await page.getByRole('option', { name: 'Food And Drink' }).click()

    // Stage 2 should now appear with Food And Drink children only
    const detailedDropdown = page.getByRole('combobox', { name: /select pfc detailed/i })
    await expect(detailedDropdown).toBeVisible()

    await detailedDropdown.click()
    await expect(page.getByRole('option', { name: 'Groceries' })).toBeVisible()
    await expect(page.getByRole('option', { name: 'Coffee' })).toBeVisible()
    // Transportation children must NOT appear in stage 2
    await expect(page.getByRole('option', { name: 'Gas' })).not.toBeVisible()

    // Type "Gro" to filter
    await detailedDropdown.fill('Gro')
    await expect(page.getByRole('option', { name: 'Groceries' })).toBeVisible()
    await expect(page.getByRole('option', { name: 'Coffee' })).not.toBeVisible()

    // Pick Groceries
    await page.getByRole('option', { name: 'Groceries' }).click()

    // Set target category and save
    const targetDropdown = page.getByRole('combobox', { name: /target category/i })
    await targetDropdown.click()
    await page.getByRole('option', { name: 'Food And Drink' }).first().click()

    // Verify stage 1 still shows Food And Drink and stage 2 shows Groceries
    await expect(primaryDropdown).toHaveValue('Food And Drink')
    await expect(detailedDropdown).toHaveValue('Groceries')
  })

  test('Scenario C: plaid-detailed endpoint NOT fetched until existing_pfc_detailed condition added', async ({
    page,
  }) => {
    let detailedRequested = false

    await mockRuleEditorAPIs(page, { withDetailed: true })

    // Override to track whether plaid-detailed was fetched
    await page.route('**/api/v1/categories/plaid-detailed*', (route) => {
      detailedRequested = true
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(FOOD_AND_DRINK_DETAILED),
      })
    })

    await page.goto('/auto-categorization')
    await page.waitForLoadState('domcontentloaded')

    // Open create dialog
    await page.getByRole('button', { name: /create rule/i }).click()
    await page.getByLabel(/rule name/i).fill('Lazy Load Test')

    // Add a non-detailed condition (merchant_contains) — detailed endpoint should NOT fire
    await page.getByRole('button', { name: /add condition/i }).click()
    // Default condition type is merchant_contains, so no type change needed

    // Wait a moment to ensure no background request fires
    await page.waitForTimeout(500)
    expect(detailedRequested).toBe(false)

    // Now change condition type to existing_pfc_detailed — endpoint SHOULD fire
    const conditionTypeSelect = page.getByLabel(/condition type/i)
    await conditionTypeSelect.selectOption('existing_pfc_detailed')

    // Stage 1 must appear (data loaded from lazy endpoint)
    const primaryDropdown = page.getByRole('combobox', { name: /select pfc primary for detailed/i })
    await expect(primaryDropdown).toBeVisible({ timeout: 5000 })
    await expect(primaryDropdown).not.toBeDisabled()

    // Endpoint should now have been requested
    expect(detailedRequested).toBe(true)
  })
})
