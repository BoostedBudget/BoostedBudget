import { test, expect, Page } from '@playwright/test'

/**
 * Custom Category Manager E2E — Authenticated (Issue #1203)
 *
 * Verifies the Settings → Categories tab renders the custom category manager,
 * lists the user's custom categories grouped by hierarchy, and opens the create form.
 */

const mockProfile = {
  id: 'user-123',
  clerk_id: 'clerk_user_123',
  email: 'test@example.com',
  name: 'Test User',
  tier: 'premium',
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-01T00:00:00Z',
}

const mockCategories = [
  {
    id: 'cat-food',
    user_id: 'user-123',
    name: 'Food Budget',
    parent_pfc_primary: null,
    parent_id: null,
    color: '#FF5733',
    icon: 'utensils',
    usage_count: 3,
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
  },
  {
    id: 'cat-fastfood',
    user_id: 'user-123',
    name: 'Fast Food',
    parent_pfc_primary: null,
    parent_id: 'cat-food',
    color: '#C70039',
    icon: 'coffee',
    usage_count: 1,
    created_at: '2025-01-02T00:00:00Z',
    updated_at: '2025-01-02T00:00:00Z',
  },
]

async function mockAPIs(page: Page) {
  await page.route('**/api/v1/user/profile*', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(mockProfile) })
  )
  await page.route('**/api/v1/subscription/**', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ tier: 'premium', status: 'active' }),
    })
  )
  await page.route('**/api/v1/categories*', (route) => {
    const method = route.request().method()
    if (method === 'GET') {
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(mockCategories) })
    } else if (method === 'POST') {
      route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify({ ...mockCategories[0], id: 'cat-new', name: 'New One' }),
      })
    } else {
      route.fulfill({ status: 200, contentType: 'application/json', body: '{}' })
    }
  })
}

test.describe('Custom Category Manager - Authenticated', () => {
  test.beforeEach(async ({ page }) => {
    await mockAPIs(page)
  })

  test('shows the Categories tab and lists custom categories by hierarchy', async ({ page }) => {
    await page.goto('/profile')
    await page.waitForLoadState('networkidle')

    await page.click('button:has-text("Categories")')

    await expect(page.getByText('Custom categories')).toBeVisible()
    await expect(page.getByText('Food Budget', { exact: true })).toBeVisible()
    await expect(page.getByText('Fast Food', { exact: true })).toBeVisible()
  })

  test('opens the create-category form', async ({ page }) => {
    await page.goto('/profile')
    await page.waitForLoadState('networkidle')
    await page.click('button:has-text("Categories")')

    await page.click('button:has-text("Add category")')

    await expect(page.getByText('New category')).toBeVisible()
    await expect(page.getByLabel('Name')).toBeVisible()
    await expect(page.getByLabel('Nest under')).toBeVisible()
  })

  test('shows the delete-impact confirmation for a category', async ({ page }) => {
    await page.goto('/profile')
    await page.waitForLoadState('networkidle')
    await page.click('button:has-text("Categories")')

    await page.getByRole('button', { name: 'Delete Food Budget' }).click()

    await expect(page.getByText('Delete category')).toBeVisible()
  })
})
