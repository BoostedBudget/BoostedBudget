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

  /**
   * Shown on the ONE control that covers a multi-account sharing unit in `ConsentPreview`
   * (issue #1569 T10.3 item C).
   *
   * Two maskless accounts at one institution can share a single `user_account_id`, which is the id
   * the backend hides by — so they are shared or hidden together and no per-account choice exists.
   * The UI used to render a switch per account bound to that one value; it failed closed, but it
   * offered a choice it could not honour. This line says why there is one switch, so the invitee is
   * told the truth about what they are consenting to rather than being shown a control that lies.
   *
   * ⚠ #1569 T10-FIX item 9: THE PREVIOUS WORDING BLAMED THE BANK. It read "Your bank reports
   * these accounts together", which is not what happens. The bank reports them separately; the
   * collision is OURS — `FindByInstitutionAndMask` matches on institution plus mask, and migration
   * 000100's `UNIQUE (user_id, institution_id_blind_idx, mask_blind_idx) NULLS NOT DISTINCT` then
   * forces every MASKLESS account at one institution onto a single `user_accounts` row. Telling a
   * user their bank did it is a false statement on a consent screen about the one thing the screen
   * exists to explain.
   *
   * Not a change to what is shared or to the consent language itself, so
   * `HOUSEHOLD_SHARE_CONSENT_VERSION` is deliberately NOT bumped.
   */
  sharedUnitNote:
    'These accounts don’t have separate account numbers we can use to tell them apart, so ' +
    'they share one setting.',

  /**
   * Shown beside an account that CANNOT be shared with a household at all (issue #1569 T10-FIX
   * item 8).
   *
   * `AcceptInvite` (`household_service.go`) builds the shareable set from `user_accounts`, so an
   * accounts row with no `user_account_id` can never enter `household_account_shares` and is
   * invisible to every household read. Both consent surfaces used to label such a row "Shared" and
   * treat it as shared-by-default — under-sharing, so it fails closed, but it is still a false
   * statement made on a consent screen, and the one place a false statement is least acceptable.
   *
   * Not a change to what is shared or to the consent language itself, so
   * `HOUSEHOLD_SHARE_CONSENT_VERSION` is deliberately NOT bumped — the same reasoning as
   * `sharedUnitNote`.
   */
  unshareableNote: 'This account can’t be shared with your household. Reconnecting it at your bank will fix that.',

  /** The status word shown beside an unshareable account, in place of "Shared"/"Hidden". */
  unshareableStatus: 'Not shared',

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
  neverShared: [
    'Individual transaction line items',
    'Full account numbers or masks',
    'Login credentials',
  ],
} as const
