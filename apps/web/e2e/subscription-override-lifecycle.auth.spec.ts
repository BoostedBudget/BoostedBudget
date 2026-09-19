/**
 * Subscription Override Lifecycle E2E Test (Issue #1414 AC 17)
 *
 * Closes gap G6: three specs already exist and are each fine in isolation —
 * `admin-subscription-override.auth.spec.ts` exercises the admin modal but never actually
 * applies an override and never looks at the target user's access; `comped-account.auth.spec.ts`
 * and `frozen-account.auth.spec.ts` describe the user's two states from hand-written payloads
 * with no link to any admin action. None of them prove that an admin action *causes* a change in
 * what the user can do. This spec is that missing causal link: apply -> verify access ->
 * remove -> verify revoked, in one continuous chain.
 *
 * --- What this does and does NOT prove -----------------------------------------------------
 * Every endpoint here is `page.route`-mocked — there is no live admin surface, no live backend
 * subscription state, and no second real Clerk account standing in for "the target user" (the
 * admin-users endpoints and the `/api/v1/user/profile` endpoint are simply two different mocked
 * views of one shared piece of in-memory state — see below). This validates the FRONTEND
 * CONTRACT and the WIRING between the admin surface and the user surface: that applying an
 * override through the modal produces the request the backend expects, and that the user-facing
 * components correctly react to the resulting profile shape. It does NOT prove the backend
 * round-trip (that a real PUT/POST against Postgres actually persists and is read back
 * correctly) — that is covered by
 * `backend/internal/repository/admin_user_repository_remove_tier_override_integration_test.go`
 * and its sibling apply-path tests. Overselling a mocked E2E is exactly how #1437 happened: a
 * globally-preloaded router mock let 6119 unit tests pass while the app threw
 * `useNavigate() outside <Router>` on every route in a real browser. Say what you mocked.
 *
 * --- The mutable-mock-state mechanism (the actual point of this file) ----------------------
 * A single `OverrideState` object (`{ tier, endsAt }`) is captured by every route handler below.
 * The admin-side GET routes (users list, subscription detail) and the user-side GET route
 * (`/api/v1/user/profile`) all READ it fresh on every request. The apply route (`PUT
 * .../subscription/tier`) and the clear route (`POST .../subscription/tier/clear`) are the ONLY
 * things that WRITE to it, driven by the real request body the modal sends. Nothing here swaps in
 * a second static fixture for the "after" assertions — the target user's profile response is
 * *computed from the same object the admin mutation just wrote to*, which is what makes the
 * later user-side assertions caused by the earlier admin-side action rather than independently
 * asserted. Each `page.goto()` below is a full browser navigation (not a client-side route
 * change), so React Query's cache is discarded each time and every screen refetches from the
 * live (mutable) mock state rather than reusing anything cached from an earlier step.
 *
 * One simplification worth naming: this is a single mocked browser session standing in for two
 * people (the admin operating `/admin/users`, and the target user visiting `/dashboard`). Their
 * IDs deliberately do NOT match (`TARGET_USER.id` for the admin-side user being comped vs. the
 * `/api/v1/user/profile` id representing "whoever is logged in") — that's intentional and mirrors
 * reality (an admin and the account they're comping are different people); only the mutable
 * `OverrideState` object connects the two mocked surfaces, which is exactly the thing under test.
 *
 * --- The `inert` diagnosis hazard (#1441) ---------------------------------------------------
 * `ReadOnlyGuard` (src/auth/ReadOnlyGuard.tsx) wraps blocked controls in a div carrying
 * `inert={true}`, `pointer-events-none`, and `aria-disabled="true"` — it does NOT unmount them.
 * Verified directly against this repo's Playwright/Chromium (not assumed): `getByRole` still
 * resolves a button inside an `inert` container (Chromium's AX-tree exclusion doesn't stop
 * Playwright's DOM-based role engine from matching it), so a bare presence check can't tell
 * "present but inert" from "not rendered". What DOES go ambiguous is calling `.click()` directly
 * on such a control: Playwright's actionability wait hangs until timeout whether the control is
 * genuinely inert (correct behavior) or the page just hasn't finished loading yet (a real bug) —
 * both surface as the same "Timeout ... exceeded" failure. The fix, reused verbatim from
 * `frozen-account.auth.spec.ts` (which pinned this after losing time to it): assert presence via
 * text/role first (fails fast and distinctly if the page never rendered), then assert the
 * concrete blocked state (`inert` attribute, `aria-disabled`, `pointer-events: none`) directly
 * instead of attempting a click and inferring the reason from a timeout.
 */

import { test, expect, type Page } from '@playwright/test'
import { mockAuthDependencies } from './helpers/api-mocks'

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const TARGET_USER = {
  id: 'user-lifecycle-1',
  email: 'lifecycle-target@example.com',
}

const BASE_TIER = 'premium'
const COMP_TIER = 'household_2'
const APPLY_REASON = 'Goodwill upgrade — support case #789'
const REMOVE_REASON = 'Comp period concluded — support case #789'

const SUBSCRIPTION_FEATURES = {
  advanced_reports: false,
  api_access: false,
  custom_categories: true,
  custom_tags: true,
  priority_support: false,
}

/**
 * The single mutable ground truth for the target user's manual override. Every route below
 * either reads it (to answer a GET) or writes it (in response to the apply/clear mutations) —
 * see the file header for why that's the whole point of this spec.
 */
interface OverrideState {
  tier: string | null
  endsAt: string | null
}

function buildUsersListItem(state: OverrideState) {
  return {
    id: TARGET_USER.id,
    email: TARGET_USER.email,
    subscription_tier: BASE_TIER,
    subscription_status: 'inactive',
    effective_status: state.tier ? 'active' : 'inactive',
    primary_region: 'us-east-1',
    last_login_at: '2026-05-01T10:00:00Z',
    created_at: '2026-01-01T00:00:00Z',
    linked_accounts_count: 1,
    boosts_count: 0,
    referrals_count: 0,
    is_admin: false,
    is_active: true,
    manual_tier_override: state.tier,
    manual_tier_ends_at: state.endsAt,
  }
}

function buildSubscriptionDetail(state: OverrideState) {
  return {
    active_overrides_count: state.tier ? 1 : 0,
    base_tier: BASE_TIER,
    effective_status: state.tier ? 'active' : 'inactive',
    effective_tier: state.tier ?? BASE_TIER,
    features: SUBSCRIPTION_FEATURES,
    limits: { transaction_history_days: 730 },
    manual_tier_override: state.tier ?? undefined,
    manual_tier_ends_at: state.endsAt ?? undefined,
    resource_overrides: [],
    subscription_status: 'inactive',
  }
}

/**
 * `GET /api/v1/user/profile` as seen by "the target user" — a subscriber whose own plan has
 * genuinely lapsed (`subscription_status: 'inactive'`). Whether they have write access at any
 * given moment depends entirely on `state.tier`, exactly the way `presentationFromUser` resolves
 * it in production (`is_comped === true` wins outright regardless of raw status).
 */
function buildTargetUserProfile(state: OverrideState) {
  return {
    id: 'user-123',
    clerk_user_id: 'user_test123',
    email: 'test@example.com',
    subscription_status: 'inactive',
    subscription_tier: BASE_TIER,
    is_comped: !!state.tier,
    comp_tier: state.tier ?? undefined,
    comp_ends_at: state.endsAt ?? undefined,
    primary_region: 'us-east-1',
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
  }
}

// ---------------------------------------------------------------------------
// Route wiring
// ---------------------------------------------------------------------------

async function setupAdminChrome(page: Page) {
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
  await page.route('**/api/v1/regions*', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ regions: {}, default_region: 'us-east-1' }),
    })
  )
}

interface CapturedBodies {
  apply: unknown
  remove: unknown
}

/**
 * Wires every route this spec needs around one shared `OverrideState`. This is the mechanism the
 * whole file exists to demonstrate: `applyRoute`/`clearRoute` WRITE `state`; every GET below READS
 * it fresh per-request, so nothing here is a fixture swapped in after the fact.
 */
async function mockLifecycleRoutes(page: Page, state: OverrideState, captured: CapturedBodies) {
  // Admin: users list (GET only — table refetches are irrelevant here, already covered by
  // admin-subscription-override.auth.spec.ts).
  await page.route('**/api/v1/admin/users*', (route) => {
    if (route.request().method() !== 'GET') {
      route.continue()
      return
    }
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        users: [buildUsersListItem(state)],
        total_count: 1,
        page: 1,
        total_pages: 1,
        has_more: false,
      }),
    })
  })

  // Admin: subscription detail (GET only — the trailing `*` here doesn't cross the `/tier` and
  // `/tier/clear` path segments below, same pattern proven in admin-subscription-override.auth.spec.ts).
  await page.route(`**/api/v1/admin/users/${TARGET_USER.id}/subscription*`, (route) => {
    if (route.request().method() !== 'GET') {
      route.continue()
      return
    }
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(buildSubscriptionDetail(state)),
    })
  })

  // Admin: apply the override ("Save Override"). WRITES `state` from the real request body.
  await page.route(`**/api/v1/admin/users/${TARGET_USER.id}/subscription/tier`, (route) => {
    const body = route.request().postDataJSON()
    captured.apply = body
    state.tier = body.tier
    state.endsAt = body.expires_at
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        stage: 'pending',
        message: 'Complimentary access change queued. Access changes only after RevenueCat confirms it.',
      }),
    })
  })

  // Admin: remove the override. WRITES `state` back to "no override".
  await page.route(`**/api/v1/admin/users/${TARGET_USER.id}/subscription/tier/clear`, (route) => {
    const body = route.request().postDataJSON()
    captured.remove = body
    state.tier = null
    state.endsAt = null
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        stage: 'pending',
        message: 'Complimentary access removal queued. Paid access and trials are unchanged.',
      }),
    })
  })

  // Target user: profile. Registered AFTER `mockAuthDependencies` runs in the test body, so this
  // (the more specific, later-registered handler) wins on every match — Playwright runs
  // overlapping route handlers in the reverse of their registration order.
  await page.route('**/api/v1/user/profile*', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(buildTargetUserProfile(state)),
    })
  })
}

// ---------------------------------------------------------------------------
// Test
// ---------------------------------------------------------------------------

test.describe('Subscription override lifecycle', () => {
  test('admin applies an override, the target user gains access, admin removes it, access is revoked', async ({
    page,
  }) => {
    const state: OverrideState = { tier: null, endsAt: null }
    const captured: CapturedBodies = { apply: null, remove: null }

    await setupAdminChrome(page)
    // isAdmin: true only affects the (baseline) profile/check-role routes this call installs;
    // the profile route it registers for `/api/v1/user/profile*` is immediately superseded by
    // the mutable one registered in mockLifecycleRoutes below (last-registered wins).
    await mockAuthDependencies(page, { isAdmin: true })
    await mockLifecycleRoutes(page, state, captured)

    // -------------------------------------------------------------------------------------
    // Step 1: Admin applies an override. This Save-Override path does not exist as a test
    // anywhere else in the repo today.
    // -------------------------------------------------------------------------------------
    await page.goto('/admin/users')
    await page.waitForLoadState('networkidle')

    await page.getByRole('button', { name: 'Override subscription' }).click()
    await expect(page.getByRole('dialog', { name: 'Subscription Override' })).toBeVisible()

    // No override yet: Save is present, Remove is not.
    await expect(page.getByRole('button', { name: 'Save Override' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Remove Override' })).not.toBeVisible()

    await page.locator('#override-tier-select').selectOption(COMP_TIER)
    await page.getByLabel(/reason/i).fill(APPLY_REASON)
    await page.getByRole('button', { name: 'Save Override' }).click()

    await expect(page.getByText('Complimentary access change queued. Access changes only after RevenueCat confirms it.')).toBeVisible()

    // The mutation reached the backend contract with exactly what the form showed.
    expect(captured.apply).toEqual({
      tier: COMP_TIER,
      reason: APPLY_REASON,
      expires_at: null,
    })
    // And the shared state — read by every route below — was actually written.
    expect(state.tier).toBe(COMP_TIER)

    // -------------------------------------------------------------------------------------
    // Step 2: The target user now has write access. Full navigation: React Query's cache is
    // gone, so `/api/v1/user/profile` is refetched fresh and reflects `state` as Step 1 left it.
    // -------------------------------------------------------------------------------------
    await page.goto('/dashboard')
    await page.waitForLoadState('networkidle')

    // Presence first (fails fast/distinctly if the page just didn't load).
    await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible()
    await expect(page.getByText(/your subscription has expired/i)).not.toBeVisible()

    // Genuinely usable, not just "not hidden": ReadOnlyGuard renders children un-wrapped (no
    // inert container) when write access is present, so this is a real, clickable button —
    // proven by actually clicking it and observing its effect, not merely reading a CSS property.
    const editButton = page.getByRole('button', { name: 'Edit' }).first()
    await expect(editButton).toBeVisible()
    await expect(editButton).toBeEnabled()
    await editButton.click()
    await expect(page.getByRole('button', { name: 'Add Widget' })).toBeVisible()

    // -------------------------------------------------------------------------------------
    // Step 3: Admin removes the override, with the reason reaching the request body.
    // -------------------------------------------------------------------------------------
    await page.goto('/admin/users')
    await page.waitForLoadState('networkidle')

    await page.getByRole('button', { name: 'Override subscription' }).click()
    await expect(page.getByRole('dialog', { name: 'Subscription Override' })).toBeVisible()

    // The detail GET reflects Step 1's mutation, not a fresh fixture re-declared here.
    await expect(page.getByText('Manual override active')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Remove Override' })).toBeVisible()

    await page.getByLabel(/reason/i).fill(REMOVE_REASON)
    await page.getByRole('button', { name: 'Remove Override' }).click()

    const confirmDialog = page.getByRole('alertdialog')
    await expect(confirmDialog).toBeVisible()
    await confirmDialog.getByLabel(/type "REMOVE" to confirm/i).fill('REMOVE')
    await confirmDialog.getByRole('button', { name: 'Remove Override' }).click()

    await expect(page.getByText('Complimentary access removal queued. Paid access and trials are unchanged.')).toBeVisible()

    // Point 3's highest-value assertion (same rationale as admin-subscription-override.auth.spec.ts):
    // the reason must reach the request BODY, the whole reason the endpoint is POST not DELETE.
    expect(captured.remove).toEqual({ reason: REMOVE_REASON })
    expect(state.tier).toBeNull()

    // -------------------------------------------------------------------------------------
    // Step 4: The target user is read-only again. Same "present vs. inert" split as Step 2 —
    // presence via text/role first, then the concrete blocked-state assertions, never a bare
    // `.click()` whose timeout wouldn't distinguish "correctly blocked" from "didn't load".
    // -------------------------------------------------------------------------------------
    await page.goto('/dashboard')
    await page.waitForLoadState('networkidle')

    await expect(page.getByRole('alert')).toContainText(/your subscription has expired/i)

    const guardWrapper = page.locator('[title="Resubscribe to customize your dashboard"]').first()
    // Longer timeout on THIS wait only, and only because `networkidle` above can resolve before
    // React has finished rendering the guard. It is a race against render, not against the
    // behaviour: the `getByRole('alert')` assertion immediately above has already proven the page
    // loaded AND that the user is genuinely read-only again, so if the guard were truly absent this
    // still fails — the extra time cannot manufacture a pass. Needed because CI runs this spec
    // roughly 3x slower than local (≈22s vs ≈7s) under full-suite parallel load on a self-hosted
    // runner, which pushed this single lookup past the 10s default and failed 6/6 CI attempts while
    // passing locally every time. Deliberately not applied to the assertions below: once the
    // wrapper is visible they resolve immediately, and widening them would start masking real
    // regressions in the blocked-state behaviour this step exists to verify.
    await expect(guardWrapper).toBeVisible({ timeout: 30_000 })
    await expect(guardWrapper).toHaveAttribute('inert', '')
    await expect(guardWrapper.locator('[aria-disabled="true"]')).toBeVisible()

    // The control stays in the DOM and visible (dimmed), same as frozen-account.auth.spec.ts —
    // ReadOnlyGuard shows users what they'd have access to rather than hiding it. What must be
    // true is that it cannot be operated: pointer-events is inherited from the guard wrapper, so
    // the button cannot receive a click. (Deliberately NOT `.click()` + expect-a-timeout, and
    // deliberately NOT `getByRole(...).toHaveCount(0)` — Playwright's role engine resolves this
    // button whether or not `inert` is applied, so that assertion would test Playwright's
    // internals, not this component's behavior.)
    const editButtonAfter = page.getByRole('button', { name: 'Edit' }).first()
    await expect(editButtonAfter).toBeVisible()
    await expect(editButtonAfter).toHaveCSS('pointer-events', 'none')
  })
})
