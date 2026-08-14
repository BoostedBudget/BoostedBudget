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

// Issue #1531 Task 11e: proves the household-SCOPE cache generation is bumped by another
// member's write and immediately reflects it, that household-scope data never leaks into the
// reader's own personal scope, and that removing a member immediately revokes their household
// visibility. This is the E2E analog of Task 11b/11c's Go-level
// TestAnalyticsCacheParity_HouseholdScope, exercised over the real HTTP path instead.
//
// ⚠ GUARD FIRST — see cache-freshness.auth.spec.ts's file header for the full rationale (same
// guard, same reasoning, not repeated here).
//
// SUBSTITUTE-PATH DOCUMENTATION (mirrors cache-freshness.auth.spec.ts's own substitution, and
// household-share-exclusion.auth.spec.ts's precedent for this exact class of gap): the brief asks
// for "member B's NEW transaction appears in member A's household view". There is no
// transaction-creation endpoint anywhere in this API (see cache-freshness.auth.spec.ts's header),
// and — per household-invite.auth.spec.ts's own documented design constraint — the SECOND Clerk
// identity this suite can provision has ZERO seeded accounts/transactions of its own (no Plaid
// Link automation, no per-user seeding endpoint). So there is no transaction for "member B" to
// literally add in the first place, regardless of whether "adding" were possible.
//
// The roles are therefore swapped from the brief's literal wording: the OWNER (who has real,
// seeded, shareable data) plays the role of the member whose WRITE must propagate, and the
// INVITEE (the second identity, provisioned fresh with nothing of their own) plays the role of
// the member whose HOUSEHOLD-SCOPE READ must observe it. The underlying property under test is
// identical either way: another household member's real write must bump the READER's
// household-scope cache generation and be visible on their very next request. Which of the two
// members physically owns the data is incidental to that property, not the property itself.
//
// The write itself reuses cache-freshness.auth.spec.ts's category-move substitution (see that
// file's header for why "move a real transaction's category across the TRANSFER_OUT boundary" is
// the closest real, cache-invalidating write available) — same transaction, same mechanism, this
// time observed from the INVITEE's household-scope read instead of the owner's own.
test.describe('Household-scope cache freshness and visibility revocation (#1531)', () => {
  // Mirrors household-share-exclusion/household-invite's reasoning: households.owner_user_id and
  // household_members(user_id) WHERE status='active' are both uniqueness-constrained, so a bare
  // Playwright retry within the same DB lifetime would hit a 409 on a second accept.
  test.describe.configure({ retries: 0 })

  test("owner's transaction-category write is immediately visible in invitee's household-scope view, absent from invitee's personal scope, and disappears once the invitee is removed", async ({
    page,
    browser,
  }, testInfo) => {
    const secondEmail = process.env.TEST_CLERK_EMAIL_2
    test.skip(
      !secondEmail,
      'TEST_CLERK_EMAIL_2 not set — skipping household-scope cache E2E test. See e2e/.env.test.example.'
    )
    testInfo.setTimeout(150_000)

    const CHIPOTLE_AMOUNT = 45.0
    const ORIGINAL_PFC_PRIMARY = 'FOOD_AND_DRINK'
    const ORIGINAL_PFC_DETAILED = 'FOOD_AND_DRINK_RESTAURANTS'
    const MOVED_PFC_PRIMARY = 'TRANSFER_OUT'
    const MOVED_PFC_DETAILED = 'TRANSFER_OUT_SAVINGS'
    const START_DATE = '2026-01-01'
    const END_DATE = '2026-12-31'

    const invitee = await createSecondIdentityContext(browser, secondEmail!)
    let inviteeUserId: string | undefined
    let transactionId: string | undefined

    try {
      // =========================================================================
      // GUARD — real admin-gated cache-enabled check, owner's session. See
      // cache-freshness.auth.spec.ts's file header for why this must run first and loudly.
      // =========================================================================
      await acceptCookieConsent(page)
      await page.goto('/dashboard')
      await page.waitForLoadState('domcontentloaded')
      await waitForClerkSession(page)
      const ownerClient = await apiClient(page)

      const healthResponse = await ownerClient.get('/api/v1/admin/system/health')
      expect(
        healthResponse.ok(),
        `GET /api/v1/admin/system/health failed (${healthResponse.status()}): ` +
          `${await healthResponse.text()}`
      ).toBe(true)
      const health: { cache_enabled: boolean } = await healthResponse.json()
      expect(
        health.cache_enabled,
        'Cache is reported disabled on /api/v1/admin/system/health — not running.'
      ).toBe(true)

      // =========================================================================
      // ARRANGE — household created/invited/accepted via Task 11's real REST helpers (not what's
      // under test here; household-invite.auth.spec.ts already covers the UI accept flow).
      // =========================================================================
      const inviteeProfileRes = await (await apiClient(invitee.page)).get('/api/v1/user/profile')
      expect(
        inviteeProfileRes.ok(),
        `GET /api/v1/user/profile (invitee) failed (${inviteeProfileRes.status()}): ` +
          `${await inviteeProfileRes.text()}`
      ).toBe(true)
      const inviteeProfile: { id: string } = await inviteeProfileRes.json()
      inviteeUserId = inviteeProfile.id

      await createHouseholdAsOwner(page)
      const invite = await createInvite(page, secondEmail!)
      await acceptInviteAs(invitee.page, invite.token)

      // =========================================================================
      // SETUP PRECONDITION (owner, real UI) — share the seeded checking account (which holds
      // the seeded "Chipotle Mexican Grill" transaction) with the household. Selectors verified
      // and reused verbatim from household-share-exclusion.auth.spec.ts.
      // =========================================================================
      await page.goto('/accounts')
      await acceptCookieConsent(page)
      await page.waitForLoadState('domcontentloaded')
      await page.locator('button[title="Cards view"]').click()

      const checkingCard = page
        .locator('[data-slot="card"]')
        .filter({ hasText: 'E2E Test Checking Account' })
      await expect(checkingCard).toBeVisible({ timeout: 15000 })
      await checkingCard.getByRole('button', { name: 'Edit account' }).click()

      const customizeModal = page.getByRole('dialog')
      await expect(customizeModal).toBeVisible()
      const shareOnSwitch = customizeModal.getByRole('switch', { name: 'Share with household' })
      await expect(shareOnSwitch).toBeVisible({ timeout: 10000 })
      await shareOnSwitch.click()
      await customizeModal.getByRole('button', { name: 'Save Changes' }).click()
      await expect(customizeModal).not.toBeVisible({ timeout: 10000 })

      // =========================================================================
      // Find the real seeded transaction (owner's REST session — never the invitee's, who owns
      // no accounts and cannot see raw transactions in household scope anyway — Issue #1369
      // decision #5 forbids that endpoint from ever accepting scope=household).
      // =========================================================================
      const txnResponse = await ownerClient.get('/api/v1/transactions?search=Chipotle&limit=10')
      expect(txnResponse.ok(), `GET transactions search failed: ${await txnResponse.text()}`).toBe(
        true
      )
      const txnBody: { transactions: Array<{ id: string; amount: number; pfc_primary?: string }> } =
        await txnResponse.json()
      const chipotle = txnBody.transactions.find((t) => t.amount === CHIPOTLE_AMOUNT)
      expect(
        chipotle,
        'Seeded "Chipotle Mexican Grill" ($45.00) transaction not found — run ' +
          'apps/web/scripts/seed-e2e-data.sql against the E2E database first.'
      ).toBeTruthy()
      transactionId = chipotle!.id
      expect(chipotle!.pfc_primary).toBe(ORIGINAL_PFC_PRIMARY)

      // =========================================================================
      // BASELINE — invitee's household-scope AND personal-scope cash-flow-summary reads, before
      // the owner's write. Fresh navigation (goto, not reused page) for the invitee too, so
      // window.Clerk hydrates from ITS OWN storageState-equivalent (a live sign-in here, per
      // createSecondIdentityContext) before apiClient reads a token from it.
      // =========================================================================
      await invitee.page.goto('/dashboard')
      await invitee.page.waitForLoadState('domcontentloaded')
      await waitForClerkSession(invitee.page)
      const inviteeClient = await apiClient(invitee.page)

      const householdBaseline = await getCashFlowSummary(inviteeClient, {
        scope: 'household',
        startDate: START_DATE,
        endDate: END_DATE,
      })
      const personalBaseline = await getCashFlowSummary(inviteeClient, {
        scope: 'personal',
        startDate: START_DATE,
        endDate: END_DATE,
      })
      // Sanity check on the setup precondition: the shared account's expenses are genuinely
      // visible in household scope before the crux write — otherwise a later "moved by $45"
      // assertion could pass vacuously against an empty baseline.
      expect(
        householdBaseline.expenses,
        'Household-scope expenses were zero even before the write — the account-share setup ' +
          'precondition did not take effect; the crux assertion below would be meaningless.'
      ).toBeGreaterThan(0)

      // =========================================================================
      // ACT — the crux mutation, by the OWNER (see file header for the role-swap rationale).
      // Real HTTP PUT through the real blanket cache-invalidation middleware
      // (internal/huma/middleware/cache_invalidation.go), which bumps BOTH the owner's user-scope
      // generation AND — since the middleware resolves the acting user's household membership —
      // the shared household-scope generation, in the same request.
      // =========================================================================
      const moveResponse = await ownerClient.put(`/api/v1/transactions/${transactionId}/category`, {
        kind: 'pfc',
        pfc_primary: MOVED_PFC_PRIMARY,
        pfc_detailed: MOVED_PFC_DETAILED,
      })
      expect(moveResponse.ok(), `category move failed: ${await moveResponse.text()}`).toBe(true)

      try {
        // =========================================================================
        // ASSERT — invitee's household-scope view moves IMMEDIATELY (next request, no polling/
        // waiting), and invitee's personal-scope view is untouched (proves no cross-scope leak).
        // =========================================================================
        const householdAfterMove = await getCashFlowSummary(inviteeClient, {
          scope: 'household',
          startDate: START_DATE,
          endDate: END_DATE,
        })
        const personalAfterMove = await getCashFlowSummary(inviteeClient, {
          scope: 'personal',
          startDate: START_DATE,
          endDate: END_DATE,
        })

        expect(
          householdBaseline.expenses - householdAfterMove.expenses,
          "Invitee's household-scope expenses did not drop by exactly the moved amount on the " +
            "VERY NEXT request after the owner's write — household-scope cache is stale."
        ).toBeCloseTo(CHIPOTLE_AMOUNT, 2)
        expect(
          personalAfterMove.expenses,
          "Invitee's personal-scope expenses changed after the OWNER's write to a HOUSEHOLD-" +
            'shared account — household-scope data leaked into the personal-scope cache entry.'
        ).toBeCloseTo(personalBaseline.expenses, 2)
      } finally {
        // Revert the category (mirrors cache-freshness.auth.spec.ts's edit-revert substitution
        // for "deleting reverts the total") before the removed-member assertion below, so a
        // later re-run of this spec (or of cache-freshness.auth.spec.ts, sharing the same seed
        // fixture) doesn't inherit a mutated transaction.
        const revertResponse = await ownerClient.put(
          `/api/v1/transactions/${transactionId}/category`,
          { kind: 'pfc', pfc_primary: ORIGINAL_PFC_PRIMARY, pfc_detailed: ORIGINAL_PFC_DETAILED }
        )
        expect(revertResponse.ok(), `category revert failed: ${await revertResponse.text()}`).toBe(
          true
        )
      }

      // =========================================================================
      // ASSERT — a removed member IMMEDIATELY loses household visibility (next request, not
      // eventually). householdSvc.ResolveScope returns domain.ErrHouseholdNotFound for a
      // non-member, mapped to a 404 — the invitee's own membership no longer resolves at all,
      // which is the whole point: this is authorization revocation, not staleness, so it must be
      // instantaneous rather than bounded by any cache TTL.
      // =========================================================================
      await removeMemberAsOwner(page, inviteeUserId)

      const householdAfterRemoval = await inviteeClient.get(
        `/api/v1/analytics/cash-flow-summary?start_date=${START_DATE}&end_date=${END_DATE}&scope=household`
      )
      expect(
        householdAfterRemoval.ok(),
        `Removed member's household-scope request unexpectedly succeeded ` +
          `(${householdAfterRemoval.status()}): ${await householdAfterRemoval.text()} — household ` +
          'visibility was not revoked immediately upon removal.'
      ).toBe(false)
      inviteeUserId = undefined // already removed; skip the finally block's removeMemberAsOwner
    } finally {
      // Best-effort teardown, mirroring household-share-exclusion/household-invite's
      // independently-try-caught, non-fatal style.
      if (inviteeUserId) {
        try {
          await removeMemberAsOwner(page, inviteeUserId)
        } catch (cleanupError) {
          console.warn(`[cache-household-visibility] member cleanup failed (non-fatal): ${cleanupError}`)
        }
      }
      try {
        await disbandHouseholdAsOwner(page)
      } catch (cleanupError) {
        console.warn(`[cache-household-visibility] household cleanup failed (non-fatal): ${cleanupError}`)
      }
      // Belt-and-suspenders: guarantee the seeded fixture's category is never left mutated even
      // if an assertion above threw before the in-test revert ran.
      if (transactionId) {
        try {
          const client = await apiClient(page)
          await client.put(`/api/v1/transactions/${transactionId}/category`, {
            kind: 'pfc',
            pfc_primary: ORIGINAL_PFC_PRIMARY,
            pfc_detailed: ORIGINAL_PFC_DETAILED,
          })
        } catch (cleanupError) {
          console.warn(`[cache-household-visibility] category cleanup failed (non-fatal): ${cleanupError}`)
        }
      }
      await invitee.close()
    }
  })
})

// waitForClerkSession mirrors auth.setup.ts's own explicit wait for window.Clerk.session.id —
// necessary before the FIRST apiClient() call on a freshly-navigated page (see
// cache-freshness.auth.spec.ts's identical helper/comment for the full rationale).
async function waitForClerkSession(page: import('@playwright/test').Page): Promise<void> {
  try {
    await page.waitForFunction(
      () => {
        const clerk = (window as unknown as { Clerk?: { session?: { id?: string } } }).Clerk
        return clerk?.session?.id != null
      },
      { timeout: 15000 }
    )
  } catch {
    // Fall through to apiClient's own retry loop.
  }
}

interface CashFlowSummaryResponse {
  expenses: number
  income: number
}

// getCashFlowSummary calls the REAL GET /api/v1/analytics/cash-flow-summary endpoint directly
// (not via UI widget rendering) — a genuine authenticated HTTP round trip through the actual
// running backend, exercising the exact same cache/scope-resolution path a real page load would.
async function getCashFlowSummary(
  client: Awaited<ReturnType<typeof apiClient>>,
  opts: { scope: 'personal' | 'household'; startDate: string; endDate: string }
): Promise<CashFlowSummaryResponse> {
  const response = await client.get(
    `/api/v1/analytics/cash-flow-summary?start_date=${opts.startDate}&end_date=${opts.endDate}&scope=${opts.scope}`
  )
  expect(
    response.ok(),
    `GET cash-flow-summary (scope=${opts.scope}) failed (${response.status()}): ` +
      `${await response.text()}`
  ).toBe(true)
  return response.json()
}
