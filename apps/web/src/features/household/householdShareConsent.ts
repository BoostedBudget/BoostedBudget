/**
 * Household Share Consent — Canonical Copy
 *
 * Issue #1372: Legal-document updates disclosing a consented shared-household
 * data model (Task 1).
 *
 * This module is the single source of truth for household data-sharing
 * consent copy. Two consent-adjacent UI moments source their visible strings
 * from here:
 *   - `ConsentPreview` (invite-accept flow)
 *   - `ShareNewAccountsDialog` (post-Plaid-link share flow)
 *
 * The Terms of Service and Privacy Policy legal text (issue #1372 Tasks 2-3)
 * are written to match these same disclosures, so the in-app consent UI and
 * the legal docs never drift apart.
 *
 * `HOUSEHOLD_SHARE_CONSENT_VERSION` MUST match the backend's
 * `domain.HouseholdShareConsentVersion` constant
 * (`backend/internal/domain/household.go`) — both sides pin this literal in
 * a test so a change to one without the other fails CI. Bump both together
 * whenever the consent language changes materially.
 */

export const HOUSEHOLD_SHARE_CONSENT_VERSION = 'household-share-v1'

export const HOUSEHOLD_SHARE_CONSENT_COPY = {
  /** Shown above the per-account toggles in `ConsentPreview` at invite-accept time. */
  invitePreviewIntro:
    'Choose which of your accounts to share with this household. All accounts are shared by ' +
    'default — you can change this any time after joining.',

  /** `DialogTitle` for `ShareNewAccountsDialog`, shown after linking new accounts via Plaid. */
  shareNewAccountsTitle: 'Share your new accounts?',

  /** `DialogDescription` for `ShareNewAccountsDialog`. */
  shareNewAccountsDescription:
    'Choose which of the accounts you just connected to share with your household. All ' +
    'accounts are shared by default — you can change this any time from account settings.',

  /**
   * What a household member can see for any account shared with the household.
   * Matches the data actually gated by `visibility='shared' AND revoked_at IS
   * NULL` in the backend's household-scope aggregation (see
   * `householdAccountsCTE()` in `analytics_repository.go`), which powers not
   * just net-worth/spending views but also household-scope income-source
   * breakdowns and savings-rate calculations.
   */
  visibleToMembers: [
    'Account names',
    'Account balances',
    'Net worth contribution',
    'Spending activity, including merchant names',
    'Income sources and savings-rate figures',
  ],

  /** What is never shared with household members, regardless of visibility settings. */
  neverShared: ['Individual transaction line items', 'Full account numbers', 'Login credentials'],
} as const
