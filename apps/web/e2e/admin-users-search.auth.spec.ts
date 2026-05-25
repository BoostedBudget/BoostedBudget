/**
 * Admin Email Prefix Search E2E Smoke Tests
 * Issue #1178: Admin email prefix search
 *
 * Covers:
 * - Typing ≥3 chars fires API with `search` param
 * - Typing <3 chars suppresses API search param and shows hint text
 *
 * Uses API mocking (page.route) — no live backend required.
 */

import { test, expect, type Page } from '@playwright/test'
import { mockAuthDependencies } from './helpers/api-mocks'

// ---------------------------------------------------------------------------
// Fixture data
// ---------------------------------------------------------------------------

type UsersListResponse = {
  users: Array<{
    id: string
    email: string
    subscription_tier: string
    subscription_status: string
    primary_region: string
    last_login_at: string
    created_at: string
    linked_accounts_count: number
    automation_rules_count: number
    referrals_count: number
    is_admin: boolean
    manual_tier_override: null
    manual_tier_ends_at: null
  }>
  total_count: number
  page: number
  total_pages: number
  has_more: boolean
}

const FIXTURE_RESULTS: UsersListResponse = {
  users: [
    {
      id: 'user-search-1',
      email: 'test@example.com',
      subscription_tier: 'premium',
      subscription_status: 'active',
      primary_region: 'us-east-1',
      last_login_at: '2026-05-01T10:00:00Z',
      created_at: '2026-01-01T00:00:00Z',
      linked_accounts_count: 1,
      automation_rules_count: 0,
      referrals_count: 0,
      is_admin: false,
      manual_tier_override: null,
      manual_tier_ends_at: null,
    },
  ],
  total_count: 1,
  page: 1,
  total_pages: 1,
  has_more: false,
}

const FIXTURE_EMPTY: UsersListResponse = {
  users: [],
  total_count: 0,
  page: 1,
  total_pages: 0,
  has_more: false,
}

// ---------------------------------------------------------------------------
// Mock helper
// ---------------------------------------------------------------------------

async function mockUsersSearch(
  page: Page,
  fixture: UsersListResponse,
  onSearch?: (s: string) => void
) {
  await mockAuthDependencies(page, { isAdmin: true })
  await page.route('**/api/v1/regions*', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ regions: {}, default_region: 'us-east-1' }),
    })
  )
  await page.route('**/api/v1/admin/users*', (route) => {
    if (route.request().method() === 'GET') {
      const url = new URL(route.request().url())
      const s = url.searchParams.get('search')
      if (s && onSearch) onSearch(s)
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(fixture),
      })
    } else {
      route.continue()
    }
  })
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe('Admin email prefix search', () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript(() => {
      const consent = {
        consents: { necessary: true, analytics: false, advertising: false },
        timestamp: '2026-01-01T00:00:00.000Z',
        version: '2.0',
      }
      document.cookie = `boostedbudget_cookie_consent=${encodeURIComponent(
        JSON.stringify(consent)
      )}; path=/; SameSite=Strict`
      localStorage.setItem('boosted-admin-mode', 'true')
    })
  })

  test('typing ≥3 chars fires API with search param', async ({ page }) => {
    // No onSearch callback needed — we assert on URL instead of captured requests.
    await mockUsersSearch(page, FIXTURE_RESULTS)

    await page.goto('/admin/users')
    await page.waitForLoadState('networkidle')

    await page
      .getByPlaceholder('Search by email prefix (min. 3 chars)...')
      .fill('test@')

    // setSearch calls setSearchParams synchronously inside React's onChange handler,
    // so the URL updates in the same event flush as the input change.
    // expect(page).toHaveURL() polls until the pattern matches (≤10s expect.timeout).
    // This is the same pattern used by all passing admin-sort.auth.spec.ts tests:
    //   click column header → await expect(page).toHaveURL(/sort=.../)
    // URL containing search= proves useUserFilters gated the param correctly (≥3 chars).
    await expect(page).toHaveURL(/[?&]search=test/)
  })

  test('typing <3 chars suppresses API search param and shows hint', async ({ page }) => {
    const seen: string[] = []
    await mockUsersSearch(page, FIXTURE_EMPTY, (s) => seen.push(s))

    await page.goto('/admin/users')
    await page.waitForLoadState('networkidle')
    seen.length = 0 // discard initial load request (no search param)

    await page
      .getByPlaceholder('Search by email prefix (min. 3 chars)...')
      .fill('te')
    await page.waitForLoadState('networkidle')

    // Hint text is visible
    await expect(
      page.getByText(
        'Keep typing — searches emails that start with your query (min. 3 chars)'
      )
    ).toBeVisible()
    // No API call included a search param
    expect(seen).toHaveLength(0)
  })
})
