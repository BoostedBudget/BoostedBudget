import { expect, test, type Page } from '@playwright/test'

/**
 * Credentialed Clerk smoke test — NON-GATING.
 *
 * This is the only lane that exercises REAL Clerk semantics end to end. The unit tier runs against
 * a hand-written Clerk mock in `jest.setup.ts`, and the contract-integration tier mocks Clerk at the
 * adapter seam, so neither can prove that a real publishable key loads, that a real email/password
 * sign-in completes, or that a real session token is minted and attached to an outbound request.
 *
 * It is NOT authoritative until CI secrets exist. The `mobile-test` job in
 * `.github/workflows/test.yml` carries no Clerk secrets and does not run this lane; it runs only
 * when an operator supplies credentials locally. Treat a green run as evidence, not a gate.
 *
 * What it does, when all three credentials are present:
 *   1. Loads the keyed static export (built by `test:clerk-smoke` into `dist/web-clerk-smoke`).
 *   2. Performs ONE real email/password sign-in through the app's own sign-in screen.
 *   3. Reads the real session token from the Clerk client the app's adapter reads it from.
 *   4. Issues one API request carrying that token and asserts, via `page.route`, that the request
 *      left with `Authorization: Bearer <token>`; the route stubs the response so no live backend
 *      is required.
 *
 * ⚠ The app has no API-calling UI in this slice — authenticated product screens are owned by #1655.
 * The request is therefore issued from the page using the SAME default session token the app's
 * adapter hands to `@boostedbudget/api-client` (`getToken()` with no JWT template). Because the
 * provider requests the default token, a first registration can trigger the backend's Clerk-API
 * email fallback — accepted, and identical to the web app's behaviour.
 *
 * ⚠ The smoke exercises email/password ONLY. The SSO self-activation / `pendingTask` path is
 * validated solely by the mocked unit tier.
 *
 * Credentials (all three required): `EXPO_PUBLIC_CLERK_PUBLISHABLE_KEY`, `CLERK_SMOKE_EMAIL`,
 * `CLERK_SMOKE_PASSWORD`. The `test:clerk-smoke` script refuses to rebuild when any is absent, so
 * this `test.skip` is the defensive second layer for a direct Playwright invocation.
 */

const PUBLISHABLE_KEY = process.env.EXPO_PUBLIC_CLERK_PUBLISHABLE_KEY
const EMAIL = process.env.CLERK_SMOKE_EMAIL
const PASSWORD = process.env.CLERK_SMOKE_PASSWORD

/** The API origin the smoke build was compiled against; used only to scope the route interception. */
const API_URL = (process.env.EXPO_PUBLIC_API_URL ?? 'https://api.clerk-smoke.invalid').replace(
  /\/+$/,
  '',
)

interface ClerkSessionLike {
  getToken: () => Promise<string | null>
}

/** The subset of the browser Clerk global (`window.Clerk`) this smoke reads. */
interface ClerkGlobalLike {
  session: ClerkSessionLike | null
}

/** Reads the real session token from the Clerk client, or `null` before a session exists. */
async function readClerkToken(page: Page): Promise<string | null> {
  return page.evaluate(async () => {
    const clerk = (window as unknown as { Clerk?: ClerkGlobalLike }).Clerk
    if (!clerk?.session) return null
    return clerk.session.getToken()
  })
}

test.skip(
  !PUBLISHABLE_KEY || !EMAIL || !PASSWORD,
  'Credentialed Clerk smoke skipped: set EXPO_PUBLIC_CLERK_PUBLISHABLE_KEY, CLERK_SMOKE_EMAIL and CLERK_SMOKE_PASSWORD.',
)

test('a real email/password sign-in mints a token and attaches it to an API request', async ({
  page,
}) => {
  const authorizationHeaders: string[] = []
  await page.route(`${API_URL}/**`, async (route) => {
    const authorization = route.request().headers()['authorization']
    if (authorization) authorizationHeaders.push(authorization)
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ status: 'ok' }),
    })
  })

  await page.goto('/sign-in', { waitUntil: 'load' })
  await expect(page.getByTestId('sign-in-screen')).toBeVisible()

  // A malformed or absent publishable key renders the inert adapter. The smoke must FAIL here,
  // never pass silently: this is the assertion the malformed-key failure QA exercises.
  await expect(page.getByTestId('sign-in-unavailable')).toHaveCount(0)
  await expect(page.getByTestId('sign-in-email')).toBeEnabled()

  await page.getByTestId('sign-in-email').fill(EMAIL!)
  await page.getByTestId('sign-in-password').fill(PASSWORD!)
  await page.getByTestId('sign-in-submit').click()

  // The guarded `(app)` group renders only once the real session is active.
  await expect(page.getByTestId('dashboard-screen')).toBeVisible({ timeout: 60_000 })
  await expect(page.getByTestId('dashboard-identity')).not.toBeEmpty()

  await expect.poll(() => readClerkToken(page), { timeout: 30_000 }).toBeTruthy()
  const token = await readClerkToken(page)
  expect(token, 'a real Clerk sign-in must mint a session token').toBeTruthy()
  // A Clerk session token is a compact JWS: three non-empty base64url segments.
  expect(token!.split('.')).toHaveLength(3)
  for (const segment of token!.split('.')) expect(segment.length).toBeGreaterThan(0)

  // One request carrying the token the app's adapter would attach. The route above intercepts it.
  await page.evaluate(
    async ({ apiUrl, bearer }) => {
      await window.fetch(`${apiUrl}/api/v1/health`, {
        headers: { Authorization: `Bearer ${bearer}` },
      })
    },
    { apiUrl: API_URL, bearer: token! },
  )

  await expect.poll(() => authorizationHeaders.length).toBeGreaterThan(0)
  expect(authorizationHeaders[0]).toBe(`Bearer ${token}`)
})
