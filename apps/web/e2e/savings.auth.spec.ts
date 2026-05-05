/**
 * Savings Page E2E Tests
 * Issue #954: Dedicated Savings Goals Page
 *
 * Covers navigation, empty state, CRUD flows, tab switching, and mobile.
 * Uses API mocking (page.route) so no live backend is required.
 *
 * Pattern follows dashboard.auth.spec.ts: mock all API calls that PremiumRoute
 * and SavingsPage depend on, then drive the UI through Playwright locators.
 */

import { test, expect } from '@playwright/test'
import { acceptCookieConsent } from './helpers/auth'
import { mockAuthDependencies } from './helpers/api-mocks'

// ---------------------------------------------------------------------------
// Fixture data
// ---------------------------------------------------------------------------

const FIXTURE_GOAL_ACTIVE = {
  id: 'goal-001',
  user_id: 'user-123',
  name: 'Vacation Fund',
  description: 'Trip to Japan',
  target_amount: 5000,
  current_amount: 1250,
  currency_code: 'USD',
  account_id: 'acc-1',
  deadline: '2026-12-31T00:00:00Z',
  is_completed: false,
  completed_at: null,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
}

const FIXTURE_GOAL_COMPLETED = {
  id: 'goal-002',
  user_id: 'user-123',
  name: 'Emergency Fund',
  description: null,
  target_amount: 10000,
  current_amount: 10000,
  currency_code: 'USD',
  account_id: 'acc-1',
  deadline: null,
  is_completed: true,
  completed_at: '2026-03-01T00:00:00Z',
  created_at: '2025-06-01T00:00:00Z',
  updated_at: '2026-03-01T00:00:00Z',
}

const GOALS_LIST_RESPONSE = {
  goals: [FIXTURE_GOAL_ACTIVE, FIXTURE_GOAL_COMPLETED],
}

const EMPTY_GOALS_RESPONSE = { goals: [] }

const FIXTURE_ACCOUNT = {
  id: 'acc-1',
  name: 'Savings Account',
  type: 'depository',
  mask: '1234',
  institution_name: 'Chase',
  balance: 5000,
  currency_code: 'USD',
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type PageArg = Parameters<typeof mockAuthDependencies>[0]

/** Mock the savings-goals endpoints with the given list body. GET = list, POST = create, DELETE = 204. */
async function mockSavingsGoals(page: PageArg, body: unknown) {
  await page.route('**/api/v1/savings-goals*', (route) => {
    if (route.request().method() === 'GET') {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(body),
      })
    } else {
      route.continue()
    }
  })
}

/** Mock the accounts endpoint (required by CreateSavingsGoalDialog). */
async function mockAccounts(page: PageArg) {
  await page.route('**/api/v1/accounts*', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ accounts: [FIXTURE_ACCOUNT] }),
    })
  })
}

// ---------------------------------------------------------------------------
// Protected Route
// ---------------------------------------------------------------------------

test.describe('Savings Page - Protected Route', () => {
  test.use({ storageState: { cookies: [], origins: [] } })

  test('redirects unauthenticated users away from /savings', async ({ page }) => {
    await page.goto('/savings')
    await expect(page).not.toHaveURL(/\/savings/, { timeout: 10000 })
  })
})

// ---------------------------------------------------------------------------
// Navigation
// ---------------------------------------------------------------------------

test.describe('Savings Page - Navigation', () => {
  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
  })

  test('clicking Savings nav link navigates to /savings', async ({ page }) => {
    await mockAuthDependencies(page)
    await mockSavingsGoals(page, GOALS_LIST_RESPONSE)

    await page.goto('/dashboard')
    await page.waitForLoadState('networkidle')

    const savingsLink = page.getByRole('link', { name: 'Savings' }).first()
    await expect(savingsLink).toBeVisible()
    await savingsLink.click()
    await page.waitForLoadState('networkidle')

    await expect(page).toHaveURL(/\/savings/)
  })
})

// ---------------------------------------------------------------------------
// Empty State
// ---------------------------------------------------------------------------

test.describe('Savings Page - Empty State', () => {
  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
  })

  test('shows "No savings goals yet" when user has no goals', async ({ page }) => {
    await mockAuthDependencies(page)
    await mockSavingsGoals(page, EMPTY_GOALS_RESPONSE)

    await page.goto('/savings')
    await page.waitForLoadState('networkidle')

    await expect(page.getByText('No savings goals yet')).toBeVisible()
    await expect(page.getByRole('button', { name: /create goal/i })).toBeVisible()
  })

  test('Create goal button in empty state opens CreateSavingsGoalDialog', async ({ page }) => {
    await mockAuthDependencies(page)
    await mockSavingsGoals(page, EMPTY_GOALS_RESPONSE)
    await mockAccounts(page)

    await page.goto('/savings')
    await page.waitForLoadState('networkidle')

    await page.getByRole('button', { name: /create goal/i }).click()

    await expect(page.getByRole('dialog', { name: /create savings goal/i })).toBeVisible()
  })
})

// ---------------------------------------------------------------------------
// Goals Grid
// ---------------------------------------------------------------------------

test.describe('Savings Page - Goals Grid', () => {
  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
  })

  test('shows page heading and goal cards when goals exist', async ({ page }) => {
    await mockAuthDependencies(page)
    await mockSavingsGoals(page, GOALS_LIST_RESPONSE)

    await page.goto('/savings')
    await page.waitForLoadState('networkidle')

    await expect(page.getByRole('heading', { name: 'Savings' })).toBeVisible()
    // All tab shows every goal
    await page.getByRole('tab', { name: /^all$/i }).click()
    await expect(page.getByText('Vacation Fund')).toBeVisible()
    await expect(page.getByText('Emergency Fund')).toBeVisible()
  })

  test('Active tab shows only non-completed goals', async ({ page }) => {
    await mockAuthDependencies(page)
    await mockSavingsGoals(page, GOALS_LIST_RESPONSE)

    await page.goto('/savings')
    await page.waitForLoadState('networkidle')

    await page.getByRole('tab', { name: /active/i }).click()

    await expect(page.getByText('Vacation Fund')).toBeVisible()
    await expect(page.getByText('Emergency Fund')).not.toBeVisible()
  })

  test('Completed tab shows only completed goals', async ({ page }) => {
    await mockAuthDependencies(page)
    await mockSavingsGoals(page, GOALS_LIST_RESPONSE)

    await page.goto('/savings')
    await page.waitForLoadState('networkidle')

    await page.getByRole('tab', { name: /completed/i }).click()

    await expect(page.getByText('Emergency Fund')).toBeVisible()
    await expect(page.getByText('Vacation Fund')).not.toBeVisible()
  })

  test('Completed tab shows inline message when no completed goals exist', async ({ page }) => {
    await mockAuthDependencies(page)
    await mockSavingsGoals(page, { goals: [FIXTURE_GOAL_ACTIVE] })

    await page.goto('/savings')
    await page.waitForLoadState('networkidle')

    await page.getByRole('tab', { name: /completed/i }).click()

    await expect(page.getByText('No completed goals yet.')).toBeVisible()
  })

  test('All tab shows both active and completed goals', async ({ page }) => {
    await mockAuthDependencies(page)
    await mockSavingsGoals(page, GOALS_LIST_RESPONSE)

    await page.goto('/savings')
    await page.waitForLoadState('networkidle')

    await page.getByRole('tab', { name: /^all$/i }).click()

    await expect(page.getByText('Vacation Fund')).toBeVisible()
    await expect(page.getByText('Emergency Fund')).toBeVisible()
  })
})

// ---------------------------------------------------------------------------
// Create Flow
// ---------------------------------------------------------------------------

test.describe('Savings Page - Create Flow', () => {
  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
  })

  test('New goal button opens CreateSavingsGoalDialog', async ({ page }) => {
    await mockAuthDependencies(page)
    await mockSavingsGoals(page, GOALS_LIST_RESPONSE)
    await mockAccounts(page)

    await page.goto('/savings')
    await page.waitForLoadState('networkidle')

    await page.getByRole('button', { name: /new goal/i }).click()

    await expect(page.getByRole('dialog', { name: /create savings goal/i })).toBeVisible()
  })

  test('submitting create form calls POST and shows new goal', async ({ page }) => {
    const NEW_GOAL = {
      id: 'goal-003',
      user_id: 'user-123',
      name: 'Car Fund',
      description: '',
      target_amount: 8000,
      current_amount: 0,
      currency_code: 'USD',
      account_id: 'acc-1',
      deadline: null,
      is_completed: false,
      completed_at: null,
      created_at: '2026-05-01T00:00:00Z',
      updated_at: '2026-05-01T00:00:00Z',
    }

    await mockAuthDependencies(page)
    await mockAccounts(page)

    let callCount = 0
    await page.route('**/api/v1/savings-goals*', (route) => {
      const method = route.request().method()
      if (method === 'GET') {
        callCount++
        const body = callCount === 1 ? EMPTY_GOALS_RESPONSE : { goals: [NEW_GOAL] }
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(body),
        })
      } else if (method === 'POST') {
        route.fulfill({
          status: 201,
          contentType: 'application/json',
          body: JSON.stringify(NEW_GOAL),
        })
      } else {
        route.continue()
      }
    })

    await page.goto('/savings')
    await page.waitForLoadState('networkidle')

    // Empty state — click Create goal
    await page.getByRole('button', { name: /create goal/i }).click()
    await expect(page.getByRole('dialog', { name: /create savings goal/i })).toBeVisible()

    await page.getByLabel(/^name/i).fill('Car Fund')
    await page.getByLabel(/target amount/i).fill('8000')
    await page.getByLabel(/account/i).selectOption({ label: /savings account/i })

    await page.getByRole('button', { name: /create goal/i }).last().click()

    await expect(page.getByRole('dialog', { name: /create savings goal/i })).not.toBeVisible()
    await expect(page.getByText('Car Fund')).toBeVisible()
  })
})

// ---------------------------------------------------------------------------
// Edit Flow
// ---------------------------------------------------------------------------

test.describe('Savings Page - Edit Flow', () => {
  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
  })

  test('clicking Edit on a card opens UpdateSavingsGoalDialog', async ({ page }) => {
    await mockAuthDependencies(page)
    await mockSavingsGoals(page, GOALS_LIST_RESPONSE)

    await page.goto('/savings')
    await page.waitForLoadState('networkidle')

    await page.getByRole('tab', { name: /^all$/i }).click()

    // Find the Vacation Fund card and click its Edit button
    const card = page.locator('.bg-card').filter({ hasText: 'Vacation Fund' }).first()
    await card.getByRole('button', { name: /edit/i }).click()

    await expect(page.getByRole('dialog', { name: /edit savings goal/i })).toBeVisible()
  })

  test('saving edited name closes dialog and shows updated card', async ({ page }) => {
    const UPDATED_GOAL = { ...FIXTURE_GOAL_ACTIVE, name: 'Euro Trip Fund' }

    await mockAuthDependencies(page)

    let fetchCount = 0
    await page.route('**/api/v1/savings-goals*', (route) => {
      const method = route.request().method()
      if (method === 'GET') {
        fetchCount++
        const goals = fetchCount === 1
          ? [FIXTURE_GOAL_ACTIVE, FIXTURE_GOAL_COMPLETED]
          : [UPDATED_GOAL, FIXTURE_GOAL_COMPLETED]
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ goals }),
        })
      } else if (method === 'PUT' || method === 'PATCH') {
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(UPDATED_GOAL),
        })
      } else {
        route.continue()
      }
    })

    await page.goto('/savings')
    await page.waitForLoadState('networkidle')

    await page.getByRole('tab', { name: /^all$/i }).click()

    const card = page.locator('.bg-card').filter({ hasText: 'Vacation Fund' }).first()
    await card.getByRole('button', { name: /edit/i }).click()

    await expect(page.getByRole('dialog', { name: /edit savings goal/i })).toBeVisible()

    const nameField = page.getByLabel(/^name/i)
    await nameField.clear()
    await nameField.fill('Euro Trip Fund')

    await page.getByRole('button', { name: /save/i }).click()

    await expect(page.getByRole('dialog', { name: /edit savings goal/i })).not.toBeVisible()
    await expect(page.getByText('Euro Trip Fund')).toBeVisible()
  })
})

// ---------------------------------------------------------------------------
// Delete Flow
// ---------------------------------------------------------------------------

test.describe('Savings Page - Delete Flow', () => {
  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
  })

  test('clicking Delete opens ConfirmationDialog with goal name', async ({ page }) => {
    await mockAuthDependencies(page)
    await mockSavingsGoals(page, GOALS_LIST_RESPONSE)

    await page.goto('/savings')
    await page.waitForLoadState('networkidle')

    await page.getByRole('tab', { name: /^all$/i }).click()

    const card = page.locator('.bg-card').filter({ hasText: 'Vacation Fund' }).first()
    await card.getByRole('button', { name: /delete/i }).click()

    await expect(page.getByRole('dialog', { name: /delete savings goal/i })).toBeVisible()
    await expect(
      page.getByText(/are you sure you want to delete "Vacation Fund"/i)
    ).toBeVisible()
  })

  test('confirming delete removes goal from grid', async ({ page }) => {
    await mockAuthDependencies(page)

    let fetchCount = 0
    await page.route('**/api/v1/savings-goals*', (route) => {
      const method = route.request().method()
      if (method === 'GET') {
        fetchCount++
        const goals = fetchCount === 1
          ? [FIXTURE_GOAL_ACTIVE, FIXTURE_GOAL_COMPLETED]
          : [FIXTURE_GOAL_COMPLETED]
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ goals }),
        })
      } else if (method === 'DELETE') {
        route.fulfill({ status: 204 })
      } else {
        route.continue()
      }
    })

    await page.goto('/savings')
    await page.waitForLoadState('networkidle')

    await page.getByRole('tab', { name: /^all$/i }).click()

    const card = page.locator('.bg-card').filter({ hasText: 'Vacation Fund' }).first()
    await card.getByRole('button', { name: /delete/i }).click()

    await expect(page.getByRole('dialog', { name: /delete savings goal/i })).toBeVisible()

    // Confirm deletion using the destructive Delete button
    await page.getByRole('button', { name: /^delete$/i }).last().click()

    await expect(page.getByText('Vacation Fund')).not.toBeVisible()
    await expect(page.getByText('Emergency Fund')).toBeVisible()
  })

  test('cancelling delete keeps goal in grid', async ({ page }) => {
    await mockAuthDependencies(page)
    await mockSavingsGoals(page, GOALS_LIST_RESPONSE)

    await page.goto('/savings')
    await page.waitForLoadState('networkidle')

    await page.getByRole('tab', { name: /^all$/i }).click()

    const card = page.locator('.bg-card').filter({ hasText: 'Vacation Fund' }).first()
    await card.getByRole('button', { name: /delete/i }).click()

    await expect(page.getByRole('dialog', { name: /delete savings goal/i })).toBeVisible()

    await page.getByRole('button', { name: /cancel/i }).click()

    await expect(page.getByRole('dialog', { name: /delete savings goal/i })).not.toBeVisible()
    await expect(page.getByText('Vacation Fund')).toBeVisible()
  })
})

// ---------------------------------------------------------------------------
// Read-only Mode
// ---------------------------------------------------------------------------

test.describe('Savings Page - Read-only Mode (inactive subscription)', () => {
  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
  })

  test('Create goal button is hidden when subscription is inactive', async ({ page }) => {
    await mockAuthDependencies(page, { subscriptionStatus: 'inactive' })
    await mockSavingsGoals(page, EMPTY_GOALS_RESPONSE)

    await page.goto('/savings')
    await page.waitForLoadState('networkidle')

    // ReadOnlyGuard wraps the button — it should not render
    await expect(page.getByRole('button', { name: /create goal/i })).not.toBeVisible()
  })
})

// ---------------------------------------------------------------------------
// Mobile
// ---------------------------------------------------------------------------

test.describe('Savings Page - Mobile', () => {
  test.use({ viewport: { width: 375, height: 667 } })

  test.beforeEach(async ({ page }) => {
    await acceptCookieConsent(page)
  })

  test('mobile nav drawer contains Savings link', async ({ page }) => {
    await mockAuthDependencies(page)
    await mockSavingsGoals(page, EMPTY_GOALS_RESPONSE)

    await page.goto('/savings')
    await page.waitForLoadState('networkidle')

    // Open the mobile hamburger drawer
    await page.getByRole('button', { name: /open menu/i }).click()

    await expect(page.getByRole('link', { name: 'Savings' })).toBeVisible()
  })

  test('goal grid renders cards within viewport width at 375px', async ({ page }) => {
    await mockAuthDependencies(page)
    await mockSavingsGoals(page, GOALS_LIST_RESPONSE)

    await page.goto('/savings')
    await page.waitForLoadState('networkidle')

    await page.getByRole('tab', { name: /^all$/i }).click()

    // Locate goal cards in the grid
    const cards = page.locator('.grid > .bg-card')
    await expect(cards.first()).toBeVisible()

    const count = await cards.count()
    expect(count).toBeGreaterThanOrEqual(2)

    // At 375px, single-column layout means each card should fill most of the width
    const box = await cards.first().boundingBox()
    expect(box).not.toBeNull()
    if (box) {
      expect(box.width).toBeGreaterThan(300)
    }
  })
})
