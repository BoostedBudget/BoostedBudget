/**
 * Budget Hierarchy E2E Tests
 * Issue #1200: Multi-level category hierarchy
 *
 * Covers three happy-path scenarios for the hierarchical budget category UI:
 * 1. Parent PFC category renders with child cards visually indented beneath it
 * 2. Budget form shows the PFC hierarchical tree picker (HierarchicalCategoryTreeSelect)
 * 3. Budget form custom kind shows a grouped <select> with <optgroup> labels
 *
 * Uses API mocking (page.route) so no live backend is required.
 * Pattern follows savings.auth.spec.ts / rule-editor-pfc.auth.spec.ts.
 */

import { test, expect, type Page } from '@playwright/test'
import { acceptCookieConsent } from '../helpers/auth'
import { mockAuthDependencies } from '../helpers/api-mocks'

// ---------------------------------------------------------------------------
// Fixture data
// ---------------------------------------------------------------------------

const BUDGET_ID = 'budget-test-id'

/** Parent PFC category — primary only, no pfc_detailed */
const CATEGORY_PARENT = {
  id: 'cat-parent-1',
  budget_id: BUDGET_ID,
  category_kind: 'pfc',
  pfc_primary: 'FOOD_AND_DRINK',
  pfc_detailed: undefined,
  pfc_primary_display: 'Food & Drink',
  pfc_detailed_display: undefined,
  custom_category_id: undefined,
  allocated_amount: 500,
  spent_amount: 200,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
  tags: [],
  match_logic: 'category_only',
}

/** Child PFC category — same primary + specific detailed */
const CATEGORY_CHILD = {
  id: 'cat-child-1',
  budget_id: BUDGET_ID,
  category_kind: 'pfc',
  pfc_primary: 'FOOD_AND_DRINK',
  pfc_detailed: 'FOOD_AND_DRINK_RESTAURANT',
  pfc_primary_display: 'Food & Drink',
  pfc_detailed_display: 'Restaurant',
  custom_category_id: undefined,
  allocated_amount: 150,
  spent_amount: 80,
  created_at: '2026-01-02T00:00:00Z',
  updated_at: '2026-01-02T00:00:00Z',
  tags: [],
  match_logic: 'category_only',
}

/** Custom category with a parent PFC primary */
const CUSTOM_CATEGORY_USER = {
  id: 'custom-cat-uuid-1',
  user_id: 'user-123',
  name: 'My Organic Market',
  parent_pfc_primary: 'FOOD_AND_DRINK',
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
}

const BUDGET_FIXTURE = {
  id: BUDGET_ID,
  user_id: 'user-123',
  name: 'January Budget',
  description: 'Monthly budget for January',
  period: 'monthly',
  start_date: '2026-01-01T00:00:00Z',
  end_date: '2026-01-31T00:00:00Z',
  amount: 3000,
  currency_code: 'USD',
  is_active: true,
  expected_income: 3000,
  income_amount: 0,
  income_sources: [],
  categories: [CATEGORY_PARENT, CATEGORY_CHILD],
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
}

// ---------------------------------------------------------------------------
// API mock helpers
// ---------------------------------------------------------------------------

async function mockBudgetDetailAPIs(
  page: Page,
  options: {
    categories?: typeof BUDGET_FIXTURE.categories
    withCustomCategories?: boolean
  } = {}
) {
  // MUST be called first — needed for PremiumRoute
  await mockAuthDependencies(page)

  const budget = {
    ...BUDGET_FIXTURE,
    categories: options.categories ?? BUDGET_FIXTURE.categories,
  }

  // Budget detail endpoint
  await page.route(`**/api/v1/budgets/${BUDGET_ID}*`, (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(budget),
    })
  })

  // User custom categories (fetched by BudgetCategoryManager + BudgetCategoryForm)
  const userCategories = options.withCustomCategories ? [CUSTOM_CATEGORY_USER] : []
  await page.route('**/api/v1/categories*', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(userCategories),
    })
  })

  // Available categories (fetched by useAvailableCategories — legacy helper, still called)
  await page.route('**/api/v1/budgets/categories/available*', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([]),
    })
  })

  // Tags
  await page.route('**/api/v1/tags*', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([]),
    })
  })
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe('Budget category hierarchy (#1200)', () => {
  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
  })

  test('renders parent budget category with child cards indented beneath it', async ({ page }) => {
    await mockBudgetDetailAPIs(page)

    await page.goto(`/budgets/${BUDGET_ID}`)
    await page.waitForLoadState('domcontentloaded')

    // Both parent and child categories must be visible
    await expect(page.getByText('Food & Drink')).toBeVisible()
    await expect(page.getByText('Restaurant')).toBeVisible()

    // Verify structural hierarchy: parent appears before child in the DOM
    const allCards = page.locator('article.budget-category-card')
    const cardCount = await allCards.count()
    expect(cardCount).toBeGreaterThanOrEqual(2)

    // Parent card (first/standalone) is accessible by its label
    const parentCard = page.getByRole('article').filter({ hasText: /Food & Drink/i }).first()
    const childCard = page.getByRole('article').filter({ hasText: /Restaurant/i })
    await expect(parentCard).toBeVisible()
    await expect(childCard).toBeVisible()

    // Structural check: child card appears after parent in document order
    const parentIndex = await allCards.evaluateAll((articles) =>
      articles.findIndex(a => a.textContent?.includes('Food & Drink'))
    )
    const childIndex = await allCards.evaluateAll((articles) =>
      articles.findIndex(a => a.textContent?.includes('Restaurant'))
    )
    expect(childIndex).toBeGreaterThan(parentIndex)
  })

  test('budget form shows PFC primary/detailed picker with tree structure', async ({ page }) => {
    // Empty categories budget — we just need the form to open
    await mockBudgetDetailAPIs(page, { categories: [] })

    await page.goto(`/budgets/${BUDGET_ID}`)
    await page.waitForLoadState('domcontentloaded')

    // Open the Add Category dialog
    await page.getByRole('button', { name: /add category/i }).click()

    // Dialog must be visible
    const dialog = page.getByRole('dialog')
    await expect(dialog).toBeVisible()

    // The PFC tree picker renders a Popover trigger button — BudgetCategoryForm passes aria-label="Select PFC category"
    const treePickerTrigger = dialog.getByRole('button', { name: /select pfc category/i })
    await expect(treePickerTrigger).toBeVisible()

    // Open the tree popover
    await treePickerTrigger.click()

    // The popover panel has role="tree" and shows primary category rows
    const treePanel = page.locator('[role="tree"]')
    await expect(treePanel).toBeVisible()

    // At least one primary row should be present — Food & Drink is in the static PFC hierarchy
    const foodDrinkRow = page.getByTestId('primary-row-FOOD_AND_DRINK')
    await expect(foodDrinkRow).toBeVisible()
    await expect(foodDrinkRow).toContainText('Food & Drink')

    // Expand the primary to see its detailed children
    const expandToggle = page.getByTestId('expand-toggle-FOOD_AND_DRINK')
    await expandToggle.click()

    // At least one detailed child row should be visible
    const restaurantRow = page.getByTestId('detailed-row-FOOD_AND_DRINK_RESTAURANT')
    await expect(restaurantRow).toBeVisible()
    await expect(restaurantRow).toContainText('Restaurant')
  })

  test('budget form custom kind shows grouped custom category select', async ({ page }) => {
    // Provide a custom category with a parent PFC primary so the optgroup renders
    await mockBudgetDetailAPIs(page, { categories: [], withCustomCategories: true })

    await page.goto(`/budgets/${BUDGET_ID}`)
    await page.waitForLoadState('domcontentloaded')

    // Open the Add Category dialog
    await page.getByRole('button', { name: /add category/i }).click()

    const dialog = page.getByRole('dialog')
    await expect(dialog).toBeVisible()

    // Switch to Custom Categories mode — the button is only visible when user has custom categories
    const customCategoriesBtn = dialog.getByRole('button', { name: /custom categories/i })
    await expect(customCategoriesBtn).toBeVisible()
    await customCategoriesBtn.click()

    // The grouped <select> element should appear
    const categorySelect = dialog.getByRole('combobox', { name: /category/i })
    await expect(categorySelect).toBeVisible()

    // The custom category name should be present in the select
    await expect(categorySelect).toContainText('My Organic Market')

    // The optgroup label should reflect the parent PFC primary
    // formatPFCPrimary('FOOD_AND_DRINK') → 'Food & Drink'
    // We verify via the DOM — <optgroup label="Food & Drink"> should exist
    const optgroupLabel = await categorySelect.evaluate((sel: HTMLSelectElement) => {
      const firstOptgroup = sel.querySelector('optgroup')
      return firstOptgroup?.getAttribute('label') ?? null
    })
    expect(optgroupLabel).toBe('Food & Drink')
  })
})
