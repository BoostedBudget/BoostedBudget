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

// Infrastructure verification for issue #1370, Task 11: multi-user auth + household
// seeding. This is genuinely more integration-flavored than a typical single-assertion
// spec on purpose — it proves the whole new E2E toolkit end-to-end (second Clerk
// identity provisioning, independent auth, and the real household/invite REST round
// trip) before Task 12 builds household UI specs on top of it.
//
// Requires TEST_CLERK_EMAIL_2 (see e2e/.env.test.example) in addition to the existing
// TEST_CLERK_EMAIL/TEST_CLERK_PASSWORD — skips gracefully if unset, mirroring
// auth.setup.ts's handling of missing primary credentials.
//
// NOTE (Task 12, Codex review): household-invite.auth.spec.ts mutates this exact same
// owner/invitee state and can race this file if both are matched in a local (2-worker) run —
// see that file's header comment for the known-limitation writeup. CI (workers: 1) is safe.

test.describe('Household E2E infrastructure smoke test (#1370)', () => {
  // households.owner_user_id and household_members(user_id) WHERE status='active' are both
  // DB-uniqueness-constrained (see backend/migrations/000106_create_households.up.sql,
  // 000107_create_household_members.up.sql) — a user can own/belong to at most one active
  // household. createHouseholdAsOwner() reuses an existing household (idempotent), but
  // acceptInviteAs() does not special-case "invitee already joined" — a genuine Playwright
  // retry of this same test within one DB lifetime would hit household_members' active-user
  // uniqueness on the second accept. Retries are disabled here rather than papering over that
  // with retry-specific logic in the (deliberately thin, honest) household.ts wrapper.
  test.describe.configure({ retries: 0 })

  test('two independently-authenticated identities complete a real household invite round trip', async ({
    page,
    browser,
  }, testInfo) => {
    const secondEmail = process.env.TEST_CLERK_EMAIL_2
    test.skip(
      !secondEmail,
      'TEST_CLERK_EMAIL_2 not set — skipping household multi-user infra smoke test. ' +
      'See e2e/.env.test.example.'
    )
    testInfo.setTimeout(90_000)

    // --- Provision + independently authenticate the second ("invitee") identity ---
    // (createSecondIdentityContext calls ensureTestUserExists internally; no need to
    // call it again here.)
    const invitee = await createSecondIdentityContext(browser, secondEmail!)
    let inviteeUserId: string | undefined

    try {
      // --- Both identities can independently reach a protected route ---
      await acceptCookieConsent(page)
      await page.goto('/dashboard')
      await page.waitForLoadState('domcontentloaded')
      await expect(page).toHaveURL(/\/dashboard/)

      await invitee.page.goto('/dashboard')
      await invitee.page.waitForLoadState('domcontentloaded')
      await expect(invitee.page).toHaveURL(/\/dashboard/)

      // --- Confirm the two sessions really are different users, not the same one twice ---
      const ownerProfileRes = await (await apiClient(page)).get('/api/v1/user/profile')
      expect(ownerProfileRes.status(), await describeFailure(ownerProfileRes)).toBe(200)
      const ownerProfile = await ownerProfileRes.json()

      const inviteeProfileRes = await (await apiClient(invitee.page)).get('/api/v1/user/profile')
      expect(inviteeProfileRes.status(), await describeFailure(inviteeProfileRes)).toBe(200)
      const inviteeProfile = await inviteeProfileRes.json()
      inviteeUserId = inviteeProfile.id

      expect(ownerProfile.id).toBeTruthy()
      expect(inviteeProfile.id).toBeTruthy()
      expect(inviteeProfile.id).not.toBe(ownerProfile.id)
      expect(inviteeProfile.email?.toLowerCase()).toBe(secondEmail!.toLowerCase())

      // --- Real household + invite + accept round trip against the live backend ---
      const household = await createHouseholdAsOwner(page)
      expect(household.id).toBeTruthy()

      const invite = await createInvite(page, secondEmail!)
      expect(invite.token).toBeTruthy()
      expect(invite.inviteLink).toContain(invite.token)

      await acceptInviteAs(invitee.page, invite.token)

      const membersRes = await (await apiClient(page)).get('/api/v1/households/me/members')
      expect(membersRes.status(), await describeFailure(membersRes)).toBe(200)
      const members: Array<{ user_id: string }> = (await membersRes.json()) ?? []

      expect(members.length).toBeGreaterThanOrEqual(2)
      const memberIds = members.map((m) => m.user_id)
      expect(memberIds).toContain(ownerProfile.id)
      expect(memberIds).toContain(inviteeProfile.id)
    } finally {
      // Best-effort teardown: undo the membership + household this test created so a
      // later run against the same (owner_user_id UNIQUE / active-member UNIQUE)
      // Postgres state can recreate/reuse cleanly instead of hitting 409s. Not fatal if
      // this fails (e.g. the test failed before the household existed) — swallow and
      // warn rather than masking the real test failure.
      // Each step is independently best-effort: if the invitee was never actually added
      // as a member (e.g. the test failed between household creation and accept),
      // removeMemberAsOwner will 404 — that shouldn't prevent still attempting to
      // disband the (memberless) household below.
      if (inviteeUserId) {
        try {
          await removeMemberAsOwner(page, inviteeUserId)
        } catch (cleanupError) {
          console.warn(`[household-infra-smoke] member cleanup failed (non-fatal): ${cleanupError}`)
        }
        try {
          await disbandHouseholdAsOwner(page)
        } catch (cleanupError) {
          console.warn(`[household-infra-smoke] household cleanup failed (non-fatal): ${cleanupError}`)
        }
      }
      await invitee.close()
    }
  })
})

// Small diagnostic helper so a failed status assertion prints the response body instead
// of just a status-code mismatch — useful when this spec is run for the first time
// against a fresh local harness.
async function describeFailure(response: {
  ok: () => boolean
  status: () => number
  text: () => Promise<string>
}): Promise<string> {
  if (response.ok()) return ''
  const body = await response.text().catch(() => '<unreadable body>')
  return `Expected 2xx, got ${response.status()}: ${body}`
}
