/**
 * The "Exclude Investment Activity" toggle SURVIVES A RELOAD — Issue #1618 / #1618b / #1609 Item 5.
 *
 * #1618 shipped the toggle but dropped its own requirement that the control only be shown where
 * it can matter ("or behind an advanced section" — the zero-cost option). P4-T5 closes that gap by
 * moving the toggle behind a collapsed "Advanced" disclosure in AccountCustomizationModal, and this
 * spec is the E2E coverage that never existed for the toggle at all — #1618b added unit tests, but
 * nothing exercised the real save → reload → still-set round trip end to end.
 *
 * Modeled directly on account-hide-persists.auth.spec.ts: a STATEFUL fake server (not a route that
 * always answers the same fixture) is the whole point. A static fixture cannot distinguish "the
 * write persisted" from "the write vanished and the UI is showing its own optimistic copy" — the
 * two look identical until a reload, and after a reload a static fixture lies in the failing
 * direction.
 *
 * ⚠ Do NOT add this to account-customization.auth.spec.ts — every one of its twelve describe blocks
 * is `test.describe.skip` (#1523/#615), so a spec added there silently never runs.
 *
 * ⚠ The fixture trap this file deliberately avoids: `name` is NEVER what renders on the account
 * card. AccountCard.tsx's displayName priority is `nickname || official_name || name`, and Plaid
 * accounts commonly have a `name` different from `official_name` (matching real shape). This
 * fixture sets both to different values on purpose — locate the card by `official_name`
 * ("Premium Checking Account"), never by `name` ("Primary Checking"). See
 * account-hide-persists.auth.spec.ts:129-136 for the same gotcha documented at its origin
 * (household-invite.auth.spec.ts:129-134, P3-T7).
 */

import { test, expect, type Page } from '@playwright/test'
import { mockAuthDependencies } from './helpers/api-mocks'

const ACCOUNT_ID = 'acc-exclude-invest-1'
const USER_ACCOUNT_ID = 'ua-exclude-invest-1'

function buildAccount(excludeInvestmentSync: boolean) {
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
    is_hidden: false,
    exclude_investment_sync: excludeInvestmentSync,
    display_order: 0,
    custom_color: null,
    custom_icon: null,
    custom_emoji: null,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

/**
 * A STATEFUL fake, same shape as account-hide-persists.auth.spec.ts's `mockDependencies`: the
 * server fake OWNS `excludeInvestmentSync`, and the reload re-reads from it. If the PATCH body
 * never carries the field, or the modal's own optimistic state is what the reload actually shows,
 * this test cannot tell the difference from a static fixture — a stateful fake can.
 */
async function mockDependencies(page: Page): Promise<{ patched: () => number }> {
  let excludeInvestmentSync = false
  let patchCount = 0

  await page.addInitScript(() => {
    const consent = {
      consents: { necessary: true, analytics: false, advertising: false },
      timestamp: '2026-01-01T00:00:00.000Z',
      version: '2.0',
    }
    document.cookie = `boostedbudget_cookie_consent=${encodeURIComponent(JSON.stringify(consent))}; path=/; SameSite=Strict`
  })

  await mockAuthDependencies(page)

  // The chain write the frontend uses for a chained account (user_account_id present) — see
  // useAccountCustomization.ts: exclude_investment_sync is carried ONLY on this branch, the legacy
  // /accounts/{id} PATCH strips it entirely.
  await page.route('**/api/v1/user-accounts/**', async (route) => {
    if (route.request().method() === 'PATCH') {
      patchCount++
      const body = route.request().postDataJSON() as { exclude_investment_sync?: boolean | null }
      if (typeof body?.exclude_investment_sync === 'boolean') {
        excludeInvestmentSync = body.exclude_investment_sync
      }
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          user_account: { id: USER_ACCOUNT_ID, exclude_investment_sync: excludeInvestmentSync },
        }),
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
      body: JSON.stringify({ accounts: [buildAccount(excludeInvestmentSync)] }),
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

test.describe('Exclude Investment Activity toggle persists (Issue #1618/#1609 Item 5)', () => {
  test('stays excluded after a reload, and the toggle round-trips through the Advanced disclosure', async ({ page }) => {
    const server = await mockDependencies(page)

    await page.goto('/accounts')
    await expect(page.getByText('Premium Checking Account').first()).toBeVisible({ timeout: 15000 })

    // Open the customization modal from the card.
    await page.getByRole('button', { name: 'Edit account' }).first().click()
    await expect(page.getByText('Customize Account')).toBeVisible()

    // The toggle is behind the collapsed Advanced disclosure (#1618/P4-T5) — it must not be in the
    // document until this is opened. This is the part of the test that fails if the disclosure
    // never opens: the switch lookup below would time out.
    const advancedButton = page.getByRole('button', { name: 'Advanced' })
    await expect(advancedButton).toHaveAttribute('aria-expanded', 'false')
    await advancedButton.click()
    await expect(advancedButton).toHaveAttribute('aria-expanded', 'true')

    // ⚠ role 'switch', not 'button' — src/components/ui/switch.tsx renders
    // `<button type="button" role="switch">`, and an explicit role replaces the implicit one.
    //
    // ⚠ The switch's OWN aria-label flips with its checked state ('Exclude investment activity'
    // when off, 'Include investment activity' when on — the action a click would take next, same
    // convention as the Hide toggle). A locator is matched by role+accessible name at the moment
    // each assertion runs, so re-using one locator variable across the click would look for a name
    // that no longer exists on the (still-same) element and time out. Query by the new name after
    // the click instead of re-asserting on the pre-click locator.
    const investmentToggleOff = page.getByRole('switch', { name: 'Exclude investment activity' })
    await expect(investmentToggleOff).toBeVisible()
    await expect(investmentToggleOff).toHaveAttribute('aria-checked', 'false')

    await investmentToggleOff.click()
    const investmentToggleOn = page.getByRole('switch', { name: 'Include investment activity' })
    await expect(investmentToggleOn).toHaveAttribute('aria-checked', 'true')

    await page.getByRole('button', { name: 'Save Changes' }).click()

    // The modal closes only when nothing failed, so its disappearance is itself an assertion that
    // the write was not swallowed.
    await expect(page.getByText('Customize Account')).toBeHidden({ timeout: 15000 })
    expect(server.patched()).toBeGreaterThan(0)

    // THE ASSERTION THAT MATTERS. A fresh page has no optimistic cache to fall back on, so what
    // renders here is what the server said — and the server only knows what the PATCH body sent.
    await page.reload()
    await expect(page.getByText('Premium Checking Account').first()).toBeVisible({ timeout: 15000 })

    await page.getByRole('button', { name: 'Edit account' }).first().click()
    await expect(page.getByText('Customize Account')).toBeVisible()

    const advancedButtonAfterReload = page.getByRole('button', { name: 'Advanced' })
    await expect(advancedButtonAfterReload).toHaveAttribute('aria-expanded', 'false')
    await advancedButtonAfterReload.click()

    // Note: aria-label flips once excluded — the switch is now "Include investment activity"
    // (the action the button would take if clicked next), same convention as the Hide toggle.
    const investmentToggleAfterReload = page.getByRole('switch', { name: 'Include investment activity' })
    await expect(investmentToggleAfterReload).toBeVisible()
    await expect(investmentToggleAfterReload).toHaveAttribute('aria-checked', 'true')
  })
})
