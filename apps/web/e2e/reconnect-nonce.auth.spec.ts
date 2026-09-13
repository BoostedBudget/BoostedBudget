/**
 * The reconnect repair CARRIES the nonce the server minted — Issue #1620 (P4-T2), spec H1.
 *
 * POST /api/v1/plaid/reconnect used to mark a broken linked item 'active' on nothing but
 * `{item_id}` from an authenticated owner, and a Plaid item_id is not a secret
 * (ExchangePublicToken already returns it to the client). The server half now mints a single-use
 * `repair_nonce` alongside the update-mode Link token and refuses a repair that does not present
 * it, pinned by repository integration tests against real Postgres (including an 8-way concurrent
 * burn).
 *
 * ⚠ What NONE of those tests can see is whether the BROWSER actually carries the nonce from the
 * mint response into the repair call. The server can be perfectly correct and the feature still
 * be dead: if the page dropped the nonce, every real reconnect would 403 and the user would be
 * told to try again forever. That seam is what this spec pins, and it is a FRONTEND contract
 * test — the fake owns the nonce, so an implementation that invents one, reuses a constant, or
 * omits the field fails here.
 *
 * ⚠ It also pins the CREDENTIAL handling. The nonce must never reach a URL: `item_id` in the
 * query string is not a secret, this is, and this branch removed a Plaid link token from a
 * notification's `action_url` (migration 000188) for exactly that reason. The check covers EVERY
 * outgoing request URL, not only main-frame navigations — the earlier draft checked navigations
 * alone, which would have missed a repair POST carrying the credential in its own query string.
 *
 * ── Why this is a new sibling spec and not an addition to account-customization.auth.spec.ts:
 * all 12 describe blocks in that file are `test.describe.skip`, so anything added there silently
 * never runs. This follows account-hide-persists.auth.spec.ts, the one proven-running
 * page.route-mocked pattern in this tree.
 *
 * ── The Plaid Link stub. usePlaidLink loads
 * https://cdn.plaid.com/link/v2/stable/link-initialize.js and calls `window.Plaid.create(config)`,
 * expecting a handler with open/exit/destroy/submit. Routing that script to a stub that fires
 * `onSuccess` on `open()` is what makes an otherwise un-automatable third-party modal
 * deterministic — and it is honest about its scope: this spec asserts what OUR page does with the
 * nonce, never that Plaid Link works.
 */

import { test, expect, type Page, type Request } from '@playwright/test'
import { mockAuthDependencies } from './helpers/api-mocks'
import { navigateWithPrivacySync, rejectUnmockedPrivacy } from './helpers/privacy-consent'

const ITEM_ID = 'plaid-item-reconnect-1'
const PLAID_LINK_SCRIPT = 'https://cdn.plaid.com/link/v2/stable/link-initialize.js'

/**
 * ⚠ The nonce is minted FRESH on every mint call and never appears as a constant in this file.
 * An earlier draft used a single hard-coded `MINTED_NONCE`, which meant a page that ignored the
 * mint response entirely and sent that same constant would have PASSED — while the file's own
 * comment claimed such an implementation fails. Codex caught that; the comment was false, not the
 * intent. Correlating against a value the fake invents per call is what makes the claim true.
 */
let mintSeq = 0
const freshNonce = () => `nonce-minted-${++mintSeq}-${Math.random().toString(36).slice(2, 10)}`

/**
 * A stub for the Plaid Link SDK. `open()` invokes the page's own onSuccess handler synchronously,
 * which is the moment the repair call is made.
 */
const PLAID_STUB_JS = `
  window.Plaid = {
    create: function (config) {
      return {
        open: function () {
          config.onSuccess('public-token-stub', {
            institution: { name: 'Test Bank', institution_id: 'ins_1' },
            accounts: [],
            link_session_id: 'link-session-stub',
          })
        },
        submit: function () {},
        exit: function () {},
        destroy: function () {},
      }
    },
  }
`

type Fake = {
  repairRequests: () => Request[]
  mintedNonces: () => string[]
  allRequestUrls: () => string[]
}

async function mockReconnectDependencies(
  page: Page,
  opts: { repairStatus?: number } = {}
): Promise<Fake> {
  const { repairStatus = 200 } = opts
  const repairRequests: Request[] = []
  const mintedNonces: string[] = []

  // EVERY outgoing request URL, not just main-frame navigations. The earlier draft inspected
  // navigation URLs only, so a repair POST carrying the nonce in its query string would have
  // passed the "never in a URL" assertion — again falsifying this file's own comment.
  const allRequestUrls: string[] = []
  page.on('request', (req) => allRequestUrls.push(req.url()))

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

  await page.route(PLAID_LINK_SCRIPT, (route) =>
    route.fulfill({ status: 200, contentType: 'application/javascript', body: PLAID_STUB_JS })
  )

  // THE MINT. The nonce is a value this fake owns, so the page cannot pass by accident.
  await page.route('**/api/v1/plaid/reconnect-link-token**', (route) => {
    const nonce = freshNonce()
    mintedNonces.push(nonce)
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        link_token: 'link-sandbox-update-mode-token',
        repair_nonce: nonce,
        expiration: '2099-01-01T00:00:00Z',
      }),
    })
  })

  // THE REPAIR. Every request is captured so the body can be inspected after the fact.
  await page.route('**/api/v1/plaid/reconnect', (route) => {
    repairRequests.push(route.request())
    if (repairStatus !== 200) {
      return route.fulfill({
        status: repairStatus,
        contentType: 'application/json',
        body: JSON.stringify({
          title: 'Forbidden',
          status: repairStatus,
          detail: 'This reconnect session is no longer valid. Please start the reconnect again.',
        }),
      })
    }
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ status: 'active' }),
    })
  })

  // The page navigates to /accounts on success, so that page's data must be mocked too.
  await page.route('**/api/v1/accounts**', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ accounts: [] }) })
  )
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

  return {
    repairRequests: () => repairRequests,
    mintedNonces: () => mintedNonces,
    allRequestUrls: () => allRequestUrls,
  }
}

test.describe('Reconnect presents the minted repair nonce (Issue #1620)', () => {
  test('carries the minted nonce into the repair call, and never into the URL', async ({ page }) => {
    const server = await mockReconnectDependencies(page)

    await navigateWithPrivacySync(page, () => page.goto(`/accounts/reconnect?item_id=${ITEM_ID}`))

    // The whole flow is automatic: mint on mount, Link opens when ready, the stub fires
    // onSuccess, the repair fires, and the page leaves for /accounts.
    await expect(page).toHaveURL(/\/accounts(?!\/reconnect)/, { timeout: 15000 })

    const minted = server.mintedNonces()
    expect(minted, 'the page must mint its own update-mode token').not.toHaveLength(0)

    const repairs = server.repairRequests()
    expect(repairs, 'the repair endpoint must be called after Link succeeds').toHaveLength(1)

    const body = repairs[0].postDataJSON() as { item_id?: string; repair_nonce?: string }
    expect(body.item_id).toBe(ITEM_ID)

    // PROVENANCE, not just presence. The value must be the one THIS session's mint invented — a
    // fresh random string the page could not have known any other way. A missing field, a
    // hard-coded constant, or a nonce carried over from a different mint all fail here.
    expect(
      body.repair_nonce,
      'the repair must present the nonce THIS mint returned. Anything else means the browser ' +
        'dropped the credential and every real reconnect would 403 — with the server half ' +
        'working perfectly.'
    ).toBe(minted[minted.length - 1])

    // ⚠ EVERY outgoing request URL, plus the address bar. Checking only navigation URLs would
    // miss a repair POST that put the credential in its own query string — which is precisely
    // the shape of leak migration 000188 removed from a notification's action_url.
    for (const nonce of minted) {
      for (const url of [...server.allRequestUrls(), page.url()]) {
        expect(
          url.includes(nonce),
          `the repair nonce must never appear in any URL (found it in ${url})`
        ).toBe(false)
      }
    }
  })

  test('shows the actionable error and does not retry when the server refuses the nonce', async ({
    page,
  }) => {
    const server = await mockReconnectDependencies(page, { repairStatus: 403 })

    await navigateWithPrivacySync(page, () => page.goto(`/accounts/reconnect?item_id=${ITEM_ID}`))
    await expect(page).toHaveURL(/\/accounts\/reconnect/)

    await expect(page.getByText("We couldn't reconnect your bank")).toBeVisible({ timeout: 15000 })

    // Still on the reconnect page — a refused repair must NOT look like a success.
    await expect(page).toHaveURL(/\/accounts\/reconnect/)

    // ⚠ Exactly one attempt. The nonce is single-use, so a client-side retry could only ever
    // re-present a value the server has already burned — turning one honest failure into a loop
    // of guaranteed 403s.
    expect(server.repairRequests(), 'a burned nonce must not be resent').toHaveLength(1)

    // And the refused credential must not have leaked into a URL on the way out either.
    for (const nonce of server.mintedNonces()) {
      for (const url of [...server.allRequestUrls(), page.url()]) {
        expect(url.includes(nonce), `nonce leaked into a URL: ${url}`).toBe(false)
      }
    }

    await expect(page.getByRole('button', { name: 'Back to Accounts' })).toBeVisible()
  })
})
