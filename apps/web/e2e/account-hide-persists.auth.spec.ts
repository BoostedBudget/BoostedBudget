/**
 * Hiding an account SURVIVES A RELOAD — Issue #1609, Item 3.
 *
 * The reported defect: hide an account, refresh, and it is back. Three separate faults produced
 * it, and no test in the tree crossed all three:
 *
 *   1. the write landed on `user_accounts` while every read projected `accounts` (task 3b),
 *   2. a failed write was swallowed into a resolved `null`, so the modal closed as if it had
 *      saved (task 3c),
 *   3. the optimistic cache was never reconciled, so the UI kept showing the guess (task 3c).
 *
 * Every one of those is invisible to a test that asserts on the optimistic UI alone. This spec is
 * deliberately shaped to be blind to it: the server fake OWNS the state, the assertion is made
 * after a RELOAD, and the reload re-reads from that fake. If the write never reached the server,
 * or the page believed its own cache instead of the server's answer, the account comes back
 * visible and this fails.
 *
 * ⚠ It is a FRONTEND contract test, not a database test: the fake stands in for the API. What it
 * pins is that the UI writes what it claims to write and then believes the server rather than
 * itself. The backend half — the write landing in the column the read prefers — is pinned by the
 * Go integration tests (account_customization_overlay_integration_test.go,
 * account_display_order_routing_integration_test.go), which is where a claim about SQL belongs.
 *
 * The pre-existing account-customization.auth.spec.ts is entirely `test.describe.skip`, so none
 * of this was covered by it.
 */

import { test, expect, type Page } from '@playwright/test'
import { mockAuthDependencies } from './helpers/api-mocks'
import { navigateWithPrivacyLoad, rejectUnmockedPrivacy } from './helpers/privacy-consent'

const ACCOUNT_ID = 'acc-hide-1'
const USER_ACCOUNT_ID = 'ua-hide-1'

function buildAccount(isHidden: boolean) {
  return {
    id: ACCOUNT_ID,
    user_id: 'user-123',
    linked_item_id: 'item-1',
    linked_item_status: 'active',
    user_account_id: USER_ACCOUNT_ID,
    plaid_account_id: 'plaid-acc-1',
    name: 'Primary Checking',
    official_name: 'Premium Checking Account',
    nickname: null,
    mask: '1234',
    type: 'depository',
    subtype: 'checking',
    institution_name: 'Chase',
    current_balance: 5000,
    available_balance: 5000,
    currency_code: 'USD',
    status: 'active',
    is_hidden: isHidden,
    display_order: 0,
    custom_color: null,
    custom_icon: null,
    custom_emoji: null,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

/**
 * A STATEFUL fake, which is the whole point. A route that always replies with the same fixture
 * cannot tell "the write persisted" from "the write vanished and the UI is showing its own
 * optimistic copy" — the two look identical until a reload, and after a reload the static
 * fixture lies in the failing direction.
 */
async function mockDependencies(page: Page): Promise<{ patched: () => number }> {
  let hidden = false
  let patchCount = 0

  await page.addInitScript(() => {
    const consent = {
      consents: { necessary: true, analytics: false, advertising: false },
      timestamp: '2026-01-01T00:00:00.000Z',
      version: '2.0',
    }
    document.cookie = `boostedbudget_cookie_consent=${encodeURIComponent(JSON.stringify(consent))}; path=/; SameSite=Strict`
  })

  await rejectUnmockedPrivacy(page)
  await mockAuthDependencies(page)

  // The chain write the frontend uses for a chained account (user_account_id present).
  await page.route('**/api/v1/user-accounts/**', async (route) => {
    if (route.request().method() === 'PATCH') {
      patchCount++
      const body = route.request().postDataJSON() as { is_hidden?: boolean | null }
      if (typeof body?.is_hidden === 'boolean') hidden = body.is_hidden
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ user_account: { id: USER_ACCOUNT_ID, is_hidden: hidden } }),
      })
    }
    return route.fulfill({ status: 200, contentType: 'application/json', body: '{}' })
  })

  await page.route('**/api/v1/accounts**', async (route) => {
    if (route.request().method() !== 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: '{}' })
    }
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ accounts: [buildAccount(hidden)] }),
    })
  })

  await page.route('**/api/v1/subscription/tiers**', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ tiers: [{ id: 'premium', name: 'Premium', max_accounts: 50, price: 8.99 }] }),
    })
  )

  await page.route('**/api/v1/plaid/link-token**', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ link_token: 'test-link-token' }) })
  )

  return { patched: () => patchCount }
}

test.describe('Hiding an account persists (Issue #1609)', () => {
  test('stays hidden after a reload', async ({ page }) => {
    const server = await mockDependencies(page)

    // The card shows official_name ("Premium Checking Account"), NOT the bare `name`
    // ("Primary Checking") — AccountCard.tsx's displayName priority is
    // nickname || official_name || name, and this fixture sets both fields to different values
    // (matching how Plaid actually shapes an account). The fixture's `name` is used only in the
    // dashboard widget's own raw-field render, never on this page — see the same gotcha
    // documented at household-invite.auth.spec.ts:129-134, which this spec fell into: it asserted
    // on `name` and the element it waited for could never appear, burning the full 15s timeout
    // on every run in CI (P3-T7).
    await navigateWithPrivacyLoad(page, () => page.goto('/accounts'))
    await expect(page).toHaveURL(/\/accounts$/)
    await expect(page.getByText('Premium Checking Account').first()).toBeVisible({ timeout: 15000 })

    // Open the customization modal from the card.
    await page.getByRole('button', { name: 'Edit account' }).first().click()
    await expect(page.getByText('Customize Account')).toBeVisible()

    // Hide it. The toggle raises a confirmation first (Issue #542).
    //
    // ⚠ role 'switch', not 'button'. src/components/ui/switch.tsx renders
    // `<button type="button" role="switch">`, and an explicit role REPLACES the implicit one, so
    // a getByRole('button') locator matches nothing here and the spec times out. Every other spec
    // in this tree uses 'switch' for the same component (household-invite.auth.spec.ts:148,
    // household-share-exclusion.auth.spec.ts:161/229). Fix round 1, F3.
    await page.getByRole('switch', { name: 'Hide account' }).click()
    await page.getByRole('button', { name: 'Hide Account', exact: true }).click()
    await page.getByRole('button', { name: 'Save Changes' }).click()

    // The modal closes only when nothing failed, so its disappearance is itself an assertion
    // that the write was not swallowed.
    await expect(page.getByText('Customize Account')).toBeHidden({ timeout: 15000 })
    expect(server.patched()).toBeGreaterThan(0)

    // THE ASSERTION THAT MATTERS. A fresh page has no optimistic cache to fall back on, so what
    // renders here is what the server said.
    await navigateWithPrivacyLoad(page, () => page.reload())
    await expect(page).toHaveURL(/\/accounts$/)
    await expect(page.getByRole('button', { name: /Show Hidden/ })).toBeVisible({ timeout: 15000 })
    await expect(page.getByText('Premium Checking Account')).toBeHidden()

    // And it is hidden rather than gone: revealing hidden accounts brings it back.
    await page.getByRole('button', { name: /Show Hidden/ }).click()
    await expect(page.getByText('Premium Checking Account').first()).toBeVisible()
  })
})
