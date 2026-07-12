import { test, expect } from '@playwright/test'
import { acceptCookieConsent, createSecondIdentityContext } from './helpers/auth'
import { apiClient } from './helpers/api/client'
import {
  createHouseholdAsOwner,
  createInvite,
  acceptInviteAs,
  removeMemberAsOwner,
  disbandHouseholdAsOwner,
} from './helpers/household'

// Issue #1370, Task 13: "share-toggle exclusion from household scope" (Gap 3, Option B
// substitute path).
//
// SUBSTITUTE-PATH DOCUMENTATION (deliberate, agreed during gap-remediation planning — not an
// implementation shortcut): the issue's original requirement was "post-Plaid-link share panel
// (ShareNewAccountsDialog) → toggle hide → account excluded from household scope." There is no
// Plaid Sandbox Link E2E automation anywhere in this suite (confirmed during gap analysis), so
// there is no way to drive that literal dialog end-to-end against the real backend. This spec
// substitutes AccountCustomizationModal's "Share with household" switch — the same
// SetHouseholdShareVisibility endpoint, the same household_account_shares row, the same
// household-scope dashboard aggregation — for the ShareNewAccountsDialog trigger. The
// underlying claim under test (toggling an account's household visibility off excludes it
// from the household-scope dashboard) is identical either way; only the UI entry point differs.
//
// CORRECTED DESIGN vs. the original task brief (discovered during Task 12's implementation,
// confirmed again here before writing any code): the brief said "As the member, open
// AccountCustomizationModal for that account, toggle OFF." This is wrong. Household sharing is
// a PER-ACCOUNT-OWNER setting on an account YOU personally linked — `/accounts` only ever shows
// accounts the signed-in user linked themselves (AccountCustomizationModal's share switch reads
// `account.user_account_id`, resolved from the caller's own accounts). A household member has
// no UI path to toggle another member's account sharing, and — as Task 12 already established —
// the invitee (Task 11's second Clerk identity) has ZERO seeded accounts of their own (no Plaid
// Link automation, no per-user seeding endpoint). So there is no account for "the member" to
// toggle in the brief's literal sense. The corrected flow used here:
//   1. Owner + invitee: household created, invited, and accepted entirely via Task 11's REST
//      helpers (not the UI accept flow) — Task 12's spec already covers and verifies that UI
//      path; re-doing it here would just be duplicate coverage of something else.
//   2. OWNER shares one of their own pre-existing seeded accounts via the real
//      AccountCustomizationModal (setup precondition — account starts shared).
//   3. Invitee switches to Household scope and confirms the account is visible (sanity check
//      that the precondition actually took effect before the crux toggle-off is exercised).
//   4. The OWNER (not the invitee) reopens AccountCustomizationModal for the same account and
//      toggles the switch OFF this time, saves.
//   5. Invitee refreshes the household-scope dashboard and confirms the account is now excluded.
//      This is the crux assertion — it proves the "toggle hide → excluded from household scope"
//      claim end-to-end against the real backend.
//
// STALE-DATA FALSE-NEGATIVE AVOIDANCE (flagged as a risk to guard against, not guess around):
// the owner's setVisibility mutation only invalidates the OWNER's own React Query cache
// (useHouseholdVisibility.ts) — it has no way to reach into the invitee's entirely separate
// browser context/page and its own in-memory QueryClient. So after the owner's toggle-off, the
// invitee's already-mounted dashboard would keep showing its last-fetched (stale, "still
// shared") household-scope balance summary for up to its 30s staleTime
// (useDashboardData.ts) unless forced to refetch. To make the exclusion assertion trustworthy
// rather than a race:
//   - The invitee does a hard `page.reload()` (a full browser navigation creates a brand new
//     in-memory QueryClient — there is no query-client persister to localStorage in this app —
//     so the very first render for any scope is guaranteed to be a genuine network fetch, never
//     a stale cache hit). Dashboard widget layout survives the reload because
//     `useDashboardLayout` persists layout server-side (`/api/v1/dashboard/preferences`), not in
//     localStorage — no need to re-add the Account Cards widget after reloading.
//   - `scope` itself is plain in-memory React state (DashboardContext.tsx), not persisted, so it
//     resets to 'personal' on every reload — the household-scope radio must be re-selected.
//   - The click that re-selects Household scope is paired with an explicit
//     `page.waitForResponse(...)` on the real `/api/v1/dashboard/balance-summary` request
//     carrying `scope=household`, so the assertions below only run once a genuinely fresh
//     network round trip for the post-toggle-off state has actually completed — not merely once
//     some UI text happens to match.
//
// KNOWN LIMITATION (pre-existing, not introduced here — re-flagged per Codex adversarial
// review since household-invite.auth.spec.ts's own note only accounted for two files): this
// spec is now a THIRD file mutating the SAME shared singleton state — the one primary owner
// (E2E_CLERK_USER_ID) and the one TEST_CLERK_EMAIL_2 invitee identity that
// household-infra-smoke.auth.spec.ts and household-invite.auth.spec.ts already share (see
// household-invite.auth.spec.ts's own header for the full explanation). playwright.config.ts
// runs `fullyParallel: true` with `workers: 2` locally (`workers: 1` in CI, which is
// unaffected), so running all three household specs together locally risks the same real
// 409/race already documented there. Not fixed here for the same reason it wasn't fixed in
// Task 12 — a real fix needs either a Playwright project-dependency chain forcing all three
// files to run sequentially, or additional seeded identities (out of scope for this task).
// Pass `--workers=1` to force serialization when running the household specs together locally:
//   bunx playwright test household-infra-smoke household-invite household-share-exclusion --workers=1
test.describe('Household share-toggle exclusion from household scope (#1370)', () => {
  // Mirrors household-infra-smoke/household-invite's reasoning: households.owner_user_id and
  // household_members(user_id) WHERE status='active' are both uniqueness-constrained, so a
  // bare Playwright retry within the same DB lifetime would hit a 409 on the second accept.
  test.describe.configure({ retries: 0 })

  test('owner toggles share OFF on a seeded account; invitee sees it excluded from household-scope dashboard', async ({
    page,
    browser,
  }, testInfo) => {
    const secondEmail = process.env.TEST_CLERK_EMAIL_2
    test.skip(
      !secondEmail,
      'TEST_CLERK_EMAIL_2 not set — skipping household share-exclusion E2E test. See e2e/.env.test.example.'
    )
    testInfo.setTimeout(150_000)

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
      // ARRANGE: create the household, invite, and accept — entirely via Task 11's real
      // REST helpers. Not what's under test here (Task 12's spec already covers the UI accept
      // flow) — just how both identities get into a state where the invitee is an active
      // household member, so this test's actual focus (the share/unshare toggle) can start.
      // =========================================================================
      await acceptCookieConsent(page)
      await page.goto('/dashboard')
      await page.waitForLoadState('domcontentloaded')

      await createHouseholdAsOwner(page)
      const invite = await createInvite(page, secondEmail!)
      await acceptInviteAs(invitee.page, invite.token)

      // =========================================================================
      // SETUP PRECONDITION (owner, real UI): share one of the owner's pre-existing seeded
      // accounts ("E2E Checking" — see scripts/seed-e2e-data.sql) with the household via the
      // real AccountCustomizationModal. Selectors/flow verified and reused verbatim from
      // Task 12's household-invite.auth.spec.ts.
      //
      // Navigating fresh to /accounts matters: createHouseholdAsOwner/createInvite above went
      // straight through the REST API, bypassing the app's own React Query cache, so
      // useHousehold()'s `isInHousehold` needs a fresh mount to see the household that now
      // exists — otherwise the "Share with household" switch (which only renders when
      // isInHousehold is true) would never appear.
      // =========================================================================
      await page.goto('/accounts')
      await acceptCookieConsent(page)
      await page.waitForLoadState('domcontentloaded')

      // Defensive: force cards view regardless of any persisted list-view preference — the
      // card-based locator below only matches in cards view.
      await page.locator('button[title="Cards view"]').click()

      const checkingCard = page
        .locator('[data-slot="card"]')
        .filter({ hasText: 'E2E Test Checking Account' })
      await expect(checkingCard).toBeVisible({ timeout: 15000 })
      await checkingCard.getByRole('button', { name: 'Edit account' }).click()

      let customizeModal = page.getByRole('dialog')
      await expect(customizeModal).toBeVisible()

      // "Share with household" is the switch's aria-label while the account is NOT YET
      // shared (AccountCustomizationModal.tsx: wantsShared ? 'Hide from household' : 'Share
      // with household') — the initial state for this account.
      const shareOnSwitch = customizeModal.getByRole('switch', { name: 'Share with household' })
      await expect(shareOnSwitch).toBeVisible({ timeout: 10000 })
      await shareOnSwitch.click()
      await customizeModal.getByRole('button', { name: 'Save Changes' }).click()
      await expect(customizeModal).not.toBeVisible({ timeout: 10000 })

      // =========================================================================
      // SANITY CHECK (invitee, real UI): switching to Household scope reveals the owner's
      // just-shared account — confirms the setup precondition actually took effect before
      // exercising the crux toggle-off below. Fresh navigation (not a reused page) so the
      // invitee's own `isInHousehold` picks up the acceptInviteAs call above.
      // =========================================================================
      await invitee.page.goto('/dashboard')
      await invitee.page.waitForLoadState('domcontentloaded')

      // The default dashboard layout doesn't include the Account Cards widget — only
      // cash-flow-chart and balance-history, which are chart/canvas-only and can't be
      // asserted on by account name. Add it via the real "Add Widget" catalog flow, unless a
      // prior run already persisted it for this invitee (WidgetCatalog treats an
      // already-present widget as "Added" — re-adding would be a no-op that never closes).
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
        // SAVE_DEBOUNCE_MS) to settle before the scope switch below re-fetches widget data.
        await invitee.page.waitForTimeout(500)
      }

      await invitee.page.getByRole('radio', { name: 'Household scope' }).click()
      await expect(invitee.page.getByRole('radio', { name: 'Household scope' })).toHaveAttribute(
        'data-state',
        'on'
      )
      await expect(invitee.page.getByText('E2E Checking')).toBeVisible({ timeout: 15000 })

      // =========================================================================
      // ACT — the crux of this test (owner, real UI): reopen AccountCustomizationModal for
      // the SAME account and toggle the switch OFF. Corrected design: this is done by the
      // OWNER (the account's actual owner), never the invitee — see file header. A second,
      // full `page.goto('/accounts')` navigation (not an in-app SPA link) so the owner's own
      // useHouseholdVisibility query is a fresh mount too, not a potentially-stale cached
      // "shared" result from the SETUP PRECONDITION step above.
      // =========================================================================
      await page.goto('/accounts')
      await acceptCookieConsent(page)
      await page.waitForLoadState('domcontentloaded')
      await page.locator('button[title="Cards view"]').click()

      const checkingCardAgain = page
        .locator('[data-slot="card"]')
        .filter({ hasText: 'E2E Test Checking Account' })
      await expect(checkingCardAgain).toBeVisible({ timeout: 15000 })
      await checkingCardAgain.getByRole('button', { name: 'Edit account' }).click()

      customizeModal = page.getByRole('dialog')
      await expect(customizeModal).toBeVisible()

      // The switch's aria-label has now flipped to 'Hide from household' since the account is
      // currently shared (confirmed by the sanity check above) — target that label directly
      // rather than re-deriving state client-side.
      const shareOffSwitch = customizeModal.getByRole('switch', { name: 'Hide from household' })
      await expect(shareOffSwitch).toBeVisible({ timeout: 10000 })
      await shareOffSwitch.click()
      await customizeModal.getByRole('button', { name: 'Save Changes' }).click()
      await expect(customizeModal).not.toBeVisible({ timeout: 10000 })

      // =========================================================================
      // ASSERT — proves household-scope exclusion end-to-end. See file header's
      // "STALE-DATA FALSE-NEGATIVE AVOIDANCE" note for why a hard reload + an explicit
      // network-response wait are used here instead of just re-clicking the scope radio:
      // this is a NEGATIVE assertion (account NOT visible), which would otherwise be
      // indistinguishable from "the invitee's dashboard just hasn't refetched yet."
      // =========================================================================
      await invitee.page.reload()
      await invitee.page.waitForLoadState('domcontentloaded')

      const householdBalanceResponse = invitee.page.waitForResponse(
        (response) =>
          response.url().includes('/api/v1/dashboard/balance-summary') &&
          response.url().includes('scope=household'),
        { timeout: 15000 }
      )
      await invitee.page.getByRole('radio', { name: 'Household scope' }).click()
      await householdBalanceResponse

      await expect(invitee.page.getByRole('radio', { name: 'Household scope' })).toHaveAttribute(
        'data-state',
        'on'
      )

      // Positive control paired with the negative assertion below: the widget genuinely
      // resolved to a zero-shared-accounts state (not stuck loading, not crashed) — the owner
      // never shared any OTHER account in this test, so once "E2E Checking" is excluded, the
      // household-scope Account Cards widget has nothing left to show.
      await expect(invitee.page.getByText('No accounts yet')).toBeVisible({ timeout: 15000 })
      await expect(invitee.page.getByText('E2E Checking')).not.toBeVisible()
    } finally {
      // Best-effort teardown, mirroring household-infra-smoke/household-invite's
      // independently-try-caught, non-fatal style.
      //
      // The owner's account-share toggle is deliberately NOT reverted here: disbanding the
      // household below cascade-deletes household_account_shares rows for it (migration
      // 000110's fk_household_account_shares_household_id ... ON DELETE CASCADE), so the
      // share is already gone once the household is (per Task 12's own finding).
      if (inviteeUserId) {
        try {
          await removeMemberAsOwner(page, inviteeUserId)
        } catch (cleanupError) {
          console.warn(`[household-share-exclusion] member cleanup failed (non-fatal): ${cleanupError}`)
        }
        try {
          await disbandHouseholdAsOwner(page)
        } catch (cleanupError) {
          console.warn(`[household-share-exclusion] household cleanup failed (non-fatal): ${cleanupError}`)
        }
      }
      await invitee.close()
    }
  })
})
