/**
 * Household Share Consent Copy — Tests
 *
 * Issue #1372: Legal-document updates disclosing a consented shared-household
 * data model (Task 1) — TDD RED, written before implementation.
 *
 * This module is the single source of truth for household data-sharing
 * consent copy: both consent-UI moments (`ConsentPreview`,
 * `ShareNewAccountsDialog`) source their visible strings from here, and the
 * Terms of Service / Privacy Policy legal text (Tasks 2-3) is written to
 * match these same disclosures, so the UI and the legal docs never drift
 * apart.
 */
import { describe, it, expect } from 'bun:test'
import { HOUSEHOLD_SHARE_CONSENT_VERSION, HOUSEHOLD_SHARE_CONSENT_COPY } from './householdShareConsent'

describe('HOUSEHOLD_SHARE_CONSENT_VERSION', () => {
  it('matches the backend domain.HouseholdShareConsentVersion constant', () => {
    // MUST match backend `domain.HouseholdShareConsentVersion`
    // (backend/internal/domain/household.go) — pinned on both sides so a
    // change to one without the other fails CI.
    expect(HOUSEHOLD_SHARE_CONSENT_VERSION).toBe('household-share-v1')
  })
})

describe('HOUSEHOLD_SHARE_CONSENT_COPY', () => {
  it('discloses that accounts are shared by default', () => {
    expect(HOUSEHOLD_SHARE_CONSENT_COPY.invitePreviewIntro).toMatch(/shared by default/i)
    expect(HOUSEHOLD_SHARE_CONSENT_COPY.shareNewAccountsDescription).toMatch(/shared by default/i)
  })

  it('discloses that sharing can be opted out per-account at join time', () => {
    expect(HOUSEHOLD_SHARE_CONSENT_COPY.invitePreviewIntro).toMatch(/choose which of your accounts/i)
    expect(HOUSEHOLD_SHARE_CONSENT_COPY.invitePreviewIntro).toMatch(/change this any time/i)
  })

  it('discloses the ShareNewAccountsDialog title and description', () => {
    expect(HOUSEHOLD_SHARE_CONSENT_COPY.shareNewAccountsTitle).toMatch(/share your new accounts/i)
    expect(HOUSEHOLD_SHARE_CONSENT_COPY.shareNewAccountsDescription).toMatch(
      /choose which of the accounts you just connected/i
    )
  })

  it('discloses what household members CAN see for a shared account', () => {
    // Includes income sources / savings-rate figures alongside net worth and
    // spending — the backend's household-scope aggregation (`analytics_repository.go`'s
    // `householdAccountsCTE()`) gates income-source breakdowns and savings-rate
    // calculations behind the exact same `visibility='shared'` gate used for
    // net worth/spending, so omitting them here would under-disclose.
    const visible = HOUSEHOLD_SHARE_CONSENT_COPY.visibleToMembers.join(' | ').toLowerCase()
    expect(visible).toMatch(/account name/)
    expect(visible).toMatch(/balance/)
    expect(visible).toMatch(/net worth/)
    expect(visible).toMatch(/spending/)
    expect(visible).toMatch(/merchant/)
    expect(visible).toMatch(/income/)
    expect(visible).toMatch(/savings.rate/)
  })

  it('discloses what is NEVER shared, regardless of visibility settings', () => {
    const neverShared = HOUSEHOLD_SHARE_CONSENT_COPY.neverShared.join(' | ').toLowerCase()
    expect(neverShared).toMatch(/individual transaction/)
    expect(neverShared).toMatch(/full account number/)
    // The household aggregate/dashboard response never serializes a `mask` field either
    // (only id/name/balance), so the never-shared disclosure must cover masks, not just
    // full account numbers.
    expect(neverShared).toMatch(/mask/)
    expect(neverShared).toMatch(/credential/)
  })
})
