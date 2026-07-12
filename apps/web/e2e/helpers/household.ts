import type { Page } from '@playwright/test'
import { apiClient } from './api/client'

// Thin wrappers over the real household REST endpoints (issue #1370).
// Mirrors the "arrange via real API call" style of helpers/api/budgets.ts:
// clear 401 detection, loud errors on non-2xx, no mocking.
//
// Request/response shapes verified against packages/api-client/src/models/
// (createHouseholdRequestBody.ts, createInviteRequestBody.ts/ResponseBody.ts,
// acceptInviteRequestBody.ts) and api/openapi.yaml — not guessed.

export interface HouseholdFixture {
  id: string
  name: string
}

// POST /api/v1/households — response body is the bare Household object
// ({id, name, owner_user_id, created_at, updated_at}), NOT wrapped like
// budgets' `{budget: ...}` shape.
//
// `households.owner_user_id` has a DB-level UNIQUE constraint (see
// backend/migrations/000106_create_households.up.sql) — a user can own at most one
// household, ever. A second POST for the same owner returns 409
// (domain.ErrAlreadyOwnsHousehold). This matters for a shared E2E owner identity across
// specs/retries within the same Postgres instance (CI retries the same test without
// resetting the DB), so — mirroring ensureBudgetExists's check-before-create pattern —
// this checks GET /api/v1/households/me first and reuses any existing household instead
// of blindly POSTing.
export async function createHouseholdAsOwner(
  page: Page,
  name = `E2E Household ${Date.now()}`
): Promise<HouseholdFixture> {
  const client = await apiClient(page)

  const existingResponse = await client.get('/api/v1/households/me')

  if (existingResponse.status() === 401) {
    throw new Error(
      'createHouseholdAsOwner: GET /api/v1/households/me returned 401 — Clerk session not ' +
      'hydrated for this Page. Check storageState / second-identity sign-in.'
    )
  }

  if (existingResponse.ok()) {
    const existing: { household: { id: string; name: string } } = await existingResponse.json()
    return { id: existing.household.id, name: existing.household.name }
  }

  if (existingResponse.status() !== 404) {
    const body = await existingResponse.text()
    throw new Error(
      `createHouseholdAsOwner: GET /api/v1/households/me failed unexpectedly ` +
      `(${existingResponse.status()}): ${body}`
    )
  }

  const response = await client.post('/api/v1/households', { name })

  if (response.status() === 401) {
    throw new Error(
      'createHouseholdAsOwner: POST /api/v1/households returned 401 — Clerk session not ' +
      'hydrated for this Page. Check storageState / second-identity sign-in.'
    )
  }

  if (!response.ok()) {
    const body = await response.text()
    throw new Error(
      `createHouseholdAsOwner: failed to create household (${response.status()}): ${body}`
    )
  }

  const household: { id: string; name: string } = await response.json()
  return { id: household.id, name: household.name }
}

export interface HouseholdInviteFixture {
  inviteId: string
  token: string
  inviteLink: string
}

// POST /api/v1/households/me/invites — response is CreateInviteResponseBody:
// `{invite: HouseholdInvite, invite_link: string}`. The accept/preview endpoints
// take the raw token (last path segment of invite_link), not invite.id.
export async function createInvite(page: Page, email: string): Promise<HouseholdInviteFixture> {
  const client = await apiClient(page)
  const response = await client.post('/api/v1/households/me/invites', { email })

  if (response.status() === 401) {
    throw new Error(
      'createInvite: POST /api/v1/households/me/invites returned 401 — Clerk session not ' +
      'hydrated for this Page.'
    )
  }

  if (!response.ok()) {
    const body = await response.text()
    throw new Error(
      `createInvite: failed to create invite for ${email} (${response.status()}): ${body}`
    )
  }

  const body: { invite: { id: string }; invite_link: string } = await response.json()
  const inviteLink = body.invite_link
  const segments = inviteLink.split('/').filter(Boolean)
  const token = segments[segments.length - 1]

  if (!token) {
    throw new Error(`createInvite: could not extract token from invite_link: "${inviteLink}"`)
  }

  return { inviteId: body.invite.id, token, inviteLink }
}

// POST /api/v1/households/invites/{token}/accept — body is AcceptInviteRequestBody:
// `{consent: boolean, hidden_account_ids: string[] | null}`.
export async function acceptInviteAs(
  page: Page,
  token: string,
  hiddenAccountIds?: string[]
): Promise<void> {
  const client = await apiClient(page)
  const response = await client.post(`/api/v1/households/invites/${token}/accept`, {
    consent: true,
    hidden_account_ids: hiddenAccountIds ?? null,
  })

  if (response.status() === 401) {
    throw new Error(
      'acceptInviteAs: POST accept returned 401 — Clerk session not hydrated for the invitee Page.'
    )
  }

  if (!response.ok()) {
    const body = await response.text()
    throw new Error(
      `acceptInviteAs: failed to accept invite token "${token}" (${response.status()}): ${body}`
    )
  }
}

// DELETE /api/v1/households/me/members/{userId} — owner-only. Used by test cleanup to
// undo acceptInviteAs so a household fixture can be safely reused/torn down across runs:
// household_members has a partial UNIQUE(user_id) WHERE status='active' index (see
// backend/migrations/000107_create_household_members.up.sql), so a member left active
// after a successful test run would make a second acceptInviteAs for the same invitee
// against a reused/persistent household fail with 409.
export async function removeMemberAsOwner(page: Page, userId: string): Promise<void> {
  const client = await apiClient(page)
  const response = await client.delete(`/api/v1/households/me/members/${userId}`)

  if (response.status() === 401) {
    throw new Error(
      'removeMemberAsOwner: DELETE returned 401 — Clerk session not hydrated for this Page.'
    )
  }

  if (!response.ok()) {
    const body = await response.text()
    throw new Error(
      `removeMemberAsOwner: failed to remove member "${userId}" (${response.status()}): ${body}`
    )
  }
}

// DELETE /api/v1/households/me — owner-only, requires zero active non-owner members
// (call removeMemberAsOwner for each member first). Used by test cleanup to fully undo
// createHouseholdAsOwner so the owner's DB-unique `owner_user_id` slot is free again for
// the next run.
export async function disbandHouseholdAsOwner(page: Page): Promise<void> {
  const client = await apiClient(page)
  const response = await client.delete('/api/v1/households/me')

  if (response.status() === 401) {
    throw new Error(
      'disbandHouseholdAsOwner: DELETE /api/v1/households/me returned 401 — Clerk session not ' +
      'hydrated for this Page.'
    )
  }

  if (!response.ok()) {
    const body = await response.text()
    throw new Error(
      `disbandHouseholdAsOwner: failed to disband household (${response.status()}): ${body}`
    )
  }
}
