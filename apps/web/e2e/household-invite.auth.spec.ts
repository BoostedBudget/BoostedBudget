import { test, expect } from '@playwright/test'
import { acceptCookieConsent, createSecondIdentityContext } from './helpers/auth'
import { apiClient } from './helpers/api/client'
import {
  createHouseholdAsOwner,
  createInvite,
  removeMemberAsOwner,
  disbandHouseholdAsOwner,
} from './helpers/household'

// Issue #1370, Task 12: "invite → consent preview → accept → shared account appears in
// household dashboard view" (Gap 2). Builds on Task 11's multi-user auth + household REST
// helpers (household-infra-smoke.auth.spec.ts already proves the raw round trip works) by
// adding the actual UI interactions this task is about: sharing an account via the real
// AccountCustomizationModal (Task 8), following the backend-generated invite link, joining
// via AcceptInvitePage (Task 6), and switching dashboard scope (Task 1369/1370's household
// aggregation) to see the shared account.
//
// Corrected design vs. the original task brief (confirmed during implementation, before any
// code was written here): the invitee — Task 11's `ensureTestUserExists`-provisioned second
// Clerk identity — is a brand-new user with ZERO seeded accounts. There is no Plaid Link
// automation in this E2E suite and no backend admin/QA endpoint to seed accounts for an
// arbitrary user; only `scripts/seed-e2e-data.sql` seeds accounts, and only for the ONE
// primary `E2E_CLERK_USER_ID`. So the "shared account" this test verifies is one of the
// OWNER's (primary user's) pre-existing seeded accounts, shared into the household via the
// real UI — not one of the invitee's own. The invitee is therefore expected to see
// ConsentPreview's EMPTY state ("no linked accounts yet") when accepting the invite, which
// is correct, deliberate behavior (Task 6 built this state on purpose), not a workaround.
//
// Two pre-existing test-infrastructure gaps were discovered and fixed while building this
// spec (see scripts/seed-e2e-data.sql's Step 3b/3c, added alongside this file):
//   1. Seeded accounts had NULL `accounts.user_account_id` (the seed script inserted
//      directly into `accounts`, skipping the `user_accounts` row a real Plaid Link
//      exchange always creates via UserAccountService.MatchOrCreate). Without it,
//      AccountCustomizationModal's "Share with household" switch never renders at all
//      (`canShowShareSwitch = isInHousehold && !!account.user_account_id`).
//   2. Seeded accounts had no `balance_snapshots` rows (real accounts get one per Plaid
//      webhook sync; direct-insert seeding never went through that path). Both the
//      personal AND household dashboard balance-summary endpoints read exclusively from
//      `balance_snapshots` — never `accounts.current_balance` directly — so without a
//      snapshot row, neither scope would ever show these accounts on the dashboard,
//      regardless of whether household sharing itself works.
// Neither gap was specific to this test; both silently affected any real-backend E2E test
// that might try to assert on seeded-account dashboard balances (none did until now).
//
// KNOWN LIMITATION (Codex adversarial review, Medium): this spec and
// household-infra-smoke.auth.spec.ts both mutate the SAME shared state — the one primary
// owner (E2E_CLERK_USER_ID) and the one TEST_CLERK_EMAIL_2 invitee identity, since those are
// the only two Clerk identities this E2E harness supports (see the design-constraint note
// above: only the primary user has seeded accounts). Playwright's `workers` config
// (playwright.config.ts) is `1` in CI but `2` locally, and different *.spec.ts FILES can be
// dispatched to different workers concurrently regardless of `fullyParallel` — so running
// both household specs together locally with 2+ workers risks a real race (concurrent
// createHouseholdAsOwner/createInvite/accept/disband against the same owner_user_id and
// household_members active-uniqueness constraints). CI is unaffected (workers: 1 there is
// already fully serial). This was not fixed here — a real fix needs either a Playwright
// project-dependency chain forcing these two files to run sequentially, or a third/fourth
// seeded identity (out of scope for this task; there is currently no way to seed accounts
// for more than one user — see the design-constraint note above). If running both specs
// locally, pass `--workers=1` to force serialization:
//   bunx playwright test household-infra-smoke household-invite --workers=1
test.describe('Household invite → consent preview → accept → shared dashboard (#1370)', () => {
  // Mirrors household-infra-smoke's reasoning: households.owner_user_id and
  // household_members(user_id) WHERE status='active' are both uniqueness-constrained, so a
  // bare Playwright retry within the same DB lifetime would hit a 409 on the second accept.
  // Retries are disabled here rather than adding retry-specific logic to the (deliberately
  // thin, honest) household.ts/auth.ts helpers.
  test.describe.configure({ retries: 0 })

  test('owner shares a seeded account via UI; invitee joins and sees it in household-scope dashboard', async ({
    page,
    browser,
  }, testInfo) => {
    const secondEmail = process.env.TEST_CLERK_EMAIL_2
    test.skip(
      !secondEmail,
      'TEST_CLERK_EMAIL_2 not set — skipping household invite E2E test. See e2e/.env.test.example.'
    )
    testInfo.setTimeout(120_000)

    const invitee = await createSecondIdentityContext(browser, secondEmail!)
    let inviteeUserId: string | undefined

    try {
      // Look up the invitee's user id up front so cleanup can still run even if a later
      // assertion fails midway through the test.
      const inviteeProfileRes = await (await apiClient(invitee.page)).get('/api/v1/user/profile')
      expect(
        inviteeProfileRes.ok(),
        `GET /api/v1/user/profile failed (${inviteeProfileRes.status()}): ${await inviteeProfileRes.text()}`
      ).toBe(true)
      const inviteeProfile: { id: string } = await inviteeProfileRes.json()
      inviteeUserId = inviteeProfile.id

      // =========================================================================
      // ARRANGE (owner): create the household + invite via Task 11's real REST helpers.
      // Not what's under test here — Task 11's smoke test already proves this raw round
      // trip — just how the owner gets into a state where they have a household and a
      // pending invite to run the real UI flow against.
      // =========================================================================
      await acceptCookieConsent(page)
      await page.goto('/dashboard')
      await page.waitForLoadState('domcontentloaded')

      const household = await createHouseholdAsOwner(page)
      const invite = await createInvite(page, secondEmail!)

      // =========================================================================
      // ACT 1 (owner, real UI): share one of the owner's pre-existing seeded accounts
      // ("E2E Checking" — see scripts/seed-e2e-data.sql) with the household via the real
      // AccountCustomizationModal, not an API shortcut.
      //
      // Navigating fresh to /accounts (rather than reusing an already-open page) matters:
      // createHouseholdAsOwner/createInvite above went straight through the REST API,
      // bypassing the app's own React Query cache, so useHousehold()'s `isInHousehold`
      // needs a fresh mount to see the household that now exists — otherwise the "Share
      // with household" switch (which only renders when isInHousehold is true) would
      // never appear.
      // =========================================================================
      await page.goto('/accounts')
      await acceptCookieConsent(page)
      await page.waitForLoadState('domcontentloaded')

      // Defensive: force cards view regardless of any persisted list-view preference —
      // the card-based locator below only matches in cards view (table view renders
      // <table> rows instead of AccountCard's `data-slot="card"` divs).
      await page.locator('button[title="Cards view"]').click()

      // The accounts-page card shows official_name ("E2E Test Checking Account"), NOT the
      // bare `name` ("E2E Checking") — AccountCard.tsx's displayName priority is
      // nickname || official_name || name, and seed-e2e-data.sql sets official_name for
      // this account. The dashboard widget checked later renders the backend's raw
      // `account.Name` field instead (dashboard_service.go: AccountBalanceDetail{Name:
      // account.Name}), which IS "E2E Checking" — hence the two different strings used in
      // this test for what is the same underlying account.
      const checkingCard = page
        .locator('[data-slot="card"]')
        .filter({ hasText: 'E2E Test Checking Account' })
      await expect(checkingCard).toBeVisible({ timeout: 15000 })
      await checkingCard.getByRole('button', { name: 'Edit account' }).click()

      const customizeModal = page.getByRole('dialog')
      await expect(customizeModal).toBeVisible()

      // "Share with household" is the switch's aria-label while the account is NOT YET
      // shared (AccountCustomizationModal.tsx: wantsShared ? 'Hide from household' :
      // 'Share with household') — the initial state for this account.
      const shareSwitch = customizeModal.getByRole('switch', { name: 'Share with household' })
      await expect(shareSwitch).toBeVisible({ timeout: 10000 })
      await shareSwitch.click()
      await customizeModal.getByRole('button', { name: 'Save Changes' }).click()
      await expect(customizeModal).not.toBeVisible({ timeout: 10000 })

      // =========================================================================
      // ACT 2 (invitee, real UI): follow the REAL backend-generated invite link — not a
      // hardcoded route — exactly like a real invitee clicking an email link would. Task 6
      // registered both `/households/invites/:token` (the real route the backend
      // generates) and an alias `/household/invite/:token`; using invite.inviteLink's own
      // path avoids guessing which one to hardcode.
      //
      // The link's ORIGIN is re-pointed at this Playwright run's actual dynamically
      // allocated frontend port instead of being followed as an absolute URL: the
      // backend's FRONTEND_URL config (internal/config/config.go) is not wired to the E2E
      // harness's per-run E2E_FRONTEND_PORT (playwright.config.ts allocates a fresh port
      // per run; nothing sets FRONTEND_URL to match it), so invite.inviteLink's origin is
      // generally the http://localhost:5173 fallback, not this run's real server. Following
      // just the path — resolved against Playwright's own configured `baseURL` — uses the
      // real backend-computed route while sidestepping that unrelated config gap.
      // =========================================================================
      const inviteUrl = new URL(invite.inviteLink)
      await invitee.page.goto(`${inviteUrl.pathname}${inviteUrl.search}`)
      await invitee.page.waitForLoadState('domcontentloaded')

      await expect(invitee.page.getByText(household.name)).toBeVisible({ timeout: 15000 })

      // Invitee has zero linked accounts (see file header) — ConsentPreview's EMPTY state
      // is CORRECT, EXPECTED behavior here (Task 6 built this deliberately), not a bug to
      // work around.
      await expect(
        invitee.page.getByText(
          'You have no linked accounts yet — you can still join this household.'
        )
      ).toBeVisible()

      await invitee.page.getByRole('button', { name: 'Join household' }).click()
      await invitee.page.waitForURL(/\/dashboard/, { timeout: 15000 })
      await invitee.page.waitForLoadState('domcontentloaded')

      // =========================================================================
      // ASSERT (owner, real UI): the household-tab/seat-count half of this task's TDD
      // checklist ("Accept join → household tab shows new member; SubscriptionCard shows
      // updated seat count"). ACT 3 below and the dashboard-scope assertion at the end of
      // this test only exercise the INVITEE's view (dashboard balance aggregation) —
      // neither touches the OWNER's household tab, so this block fills that gap using the
      // owner's `page`, which has stayed live and authenticated since ARRANGE.
      //
      // A fresh `page.goto` (not the stale in-memory /accounts view) matters here for the
      // same reason ACT 1's /accounts navigation needed one: `page` was last navigated
      // there for the account-share step, well before the invitee's accept above changed
      // server-side state, so React Query needs a fresh mount to refetch
      // listHouseholdMembers/getHouseholdEntitlement rather than serving a stale cache.
      // =========================================================================
      await page.goto('/profile?tab=household')
      await acceptCookieConsent(page)
      await page.waitForLoadState('domcontentloaded')

      // MembersList never renders a member's email — household_service.go's
      // clerkDisplayName() deliberately never falls back to email ("more sensitive than a
      // missing name, handled separately if ever needed"), so matching on `secondEmail`
      // text is not viable here regardless of what Clerk profile fields the invitee
      // identity happens to have set. The one per-row thing MembersList renders that's
      // both stable and known ahead of time is the identity cell's
      // `id={`member-name-${member.user_id}`}` span (MembersList.tsx) — and this test
      // already captured the invitee's user id (`inviteeUserId`, looked up via
      // GET /user/profile above) before the invite was ever created. Locating on that id
      // proves the invitee's row is actually present without depending on a guessed
      // display name.
      await expect(page.locator(`[id="member-name-${inviteeUserId}"]`)).toBeVisible({
        timeout: 15000,
      })

      // SeatUsageBadge (rendered inside HouseholdCard on this tab, `data-slot=
      // "seat-usage-badge"`) renders literally `${used}/${total}` (SeatUsageBadge.tsx).
      // The household's total seat count depends on the owner's tier and isn't a fixture
      // this test controls — but whatever it is, the invitee's accept above only succeeds
      // because of HouseholdService's own accept-time seat re-check
      // (household_service.go: `if activeCount >= ownerTier.Pricing.UsersIncluded`), which
      // means total must already be > 1 active member for this point in the test to have
      // been reached at all. Asserting on the "used" half alone ("2/") is therefore both
      // sufficient and total-agnostic.
      await expect(page.locator('[data-slot="seat-usage-badge"]')).toHaveText(/^2\//, {
        timeout: 15000,
      })

      // =========================================================================
      // ASSERT (owner, real UI): the OTHER half of the same TDD line — "Accept join →
      // household tab shows new member; SubscriptionCard shows updated seat count". Added
      // after an adversarial (Codex) review of this file's first pass (commit 6b70a242)
      // found that the assertions above only ever prove HouseholdCard's SeatUsageBadge is
      // correct — SubscriptionCard is a genuinely separate component, rendered on the
      // default `/profile` tab (not `?tab=household`), with its own independent seat-usage
      // rendering (plain text, not <SeatUsageBadge>) that could regress on its own even with
      // the badge assertion above passing. A second navigation is required because the two
      // components live on different `activeTab` branches of ProfilePage and are never both
      // mounted at once.
      // =========================================================================
      await page.goto('/profile')
      await acceptCookieConsent(page)
      await page.waitForLoadState('domcontentloaded')

      // SubscriptionCard renders seat usage as `${used} of ${total} seats used`
      // (SubscriptionCard.tsx), a different literal format from SeatUsageBadge's
      // `${used}/${total}` for the same underlying data. Same total-agnostic reasoning as
      // the SeatUsageBadge assertion above applies — only "used" (2) is asserted, not the
      // exact total.
      await expect(page.getByText(/^2 of \d+ seats used$/)).toBeVisible({ timeout: 15000 })

      // =========================================================================
      // ACT 3 (invitee, real UI): the default dashboard layout
      // (config/default_dashboard_layout.yaml) doesn't include the Account Cards widget —
      // only cash-flow-chart and balance-history, which are chart/canvas-only and can't be
      // asserted on by account name. Add it via the real "Add Widget" catalog flow, unless
      // a prior run of this same test already persisted it for this invitee — WidgetCatalog
      // treats an already-present widget as "Added" (its onClick becomes a no-op and the
      // catalog would never close), so re-adding it is skipped in that case.
      // =========================================================================
      const accountCardsHeading = invitee.page.getByText('Account Cards', { exact: true })
      if (!(await accountCardsHeading.isVisible().catch(() => false))) {
        await invitee.page.getByRole('button', { name: 'Edit' }).click()
        await invitee.page.getByRole('button', { name: 'Add Widget' }).click()

        const catalogDialog = invitee.page.getByRole('dialog')
        await expect(catalogDialog.getByText('Add Widget')).toBeVisible()
        await catalogDialog.getByText('Account Cards', { exact: true }).click()
        await expect(catalogDialog).not.toBeVisible({ timeout: 10000 })

        await invitee.page.getByRole('button', { name: 'Done' }).click()
        // Small buffer for the debounced layout save (useDashboardLayout's
        // SAVE_DEBOUNCE_MS) to settle before the scope switch below re-fetches widget
        // data — not required for correctness (the widget is already rendered locally),
        // just stability.
        await invitee.page.waitForTimeout(500)
      }

      // =========================================================================
      // ASSERT: switching to Household scope reveals the owner's shared account. This is
      // the crux of the test — it would genuinely fail if household account sharing or
      // scope-aware dashboard aggregation regressed, since the invitee's own
      // personal-scope balance is $0 (zero seeded accounts).
      // =========================================================================
      await expect(invitee.page.getByText('E2E Checking')).not.toBeVisible()

      await invitee.page.getByRole('radio', { name: 'Household scope' }).click()
      await expect(invitee.page.getByRole('radio', { name: 'Household scope' })).toHaveAttribute(
        'data-state',
        'on'
      )

      await expect(invitee.page.getByText('E2E Checking')).toBeVisible({ timeout: 15000 })
    } finally {
      // Best-effort teardown, mirroring household-infra-smoke's independently-try-caught,
      // non-fatal style: undo the membership + household this test created so a later run
      // against the same (owner_user_id UNIQUE / active-member UNIQUE) Postgres state can
      // recreate/reuse cleanly instead of hitting 409s.
      //
      // The owner's account-share toggle is deliberately NOT reverted here: disbanding the
      // household below cascade-deletes household_account_shares rows for it (migration
      // 000110's fk_household_account_shares_household_id ... ON DELETE CASCADE), so the
      // share is already gone once the household is. Leaving the UI toggle itself "on"
      // costs nothing — the next createHouseholdAsOwner (a fresh household) starts with
      // zero shares regardless of this account's last on-screen toggle state.
      if (inviteeUserId) {
        try {
          await removeMemberAsOwner(page, inviteeUserId)
        } catch (cleanupError) {
          console.warn(`[household-invite] member cleanup failed (non-fatal): ${cleanupError}`)
        }
        try {
          await disbandHouseholdAsOwner(page)
        } catch (cleanupError) {
          console.warn(`[household-invite] household cleanup failed (non-fatal): ${cleanupError}`)
        }
      }
      await invitee.close()
    }
  })
})
