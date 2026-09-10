/**
 * Constants for Auto-Categorization Feature
 * Issue #584: Unified Card/Table View System
 */

// =============================================================================
// Local Storage Keys
// =============================================================================

export const STORAGE_KEY_LIST_VIEW = 'autocat-list-view'

// =============================================================================
// Display fallbacks — Issue #1626 (Task K)
// =============================================================================

/**
 * Shown for an `account_id` condition whose account cannot be named: the accounts query has not
 * resolved yet, or the id names an account this user can no longer read.
 *
 * A rule condition is a DISPLAY path, so it is resolved against the full account universe
 * (`useAllAccounts`), which includes retained non-active accounts — see
 * `useRuleAccountLabels`. This label is therefore the "we genuinely cannot name it" case, and it
 * exists so the surface never falls back to the raw UUID (the defect the user reported).
 *
 * Wording matches `InvestmentDetailDrawer`'s existing "never show the raw UUID" placeholder so
 * the two account-lookup surfaces agree.
 */
export const UNKNOWN_ACCOUNT_LABEL = 'Unknown account'

/**
 * Shown for a rule whose target is a custom category but whose `target_custom_category_display`
 * did not arrive (the field is enriched separately from the row — see
 * `categorizationRuleService.enrichCustomCategoryDisplay` — so "no name" is reachable).
 *
 * Wording is deliberately identical to the backend's `unnamedCustomCategoryLabel`
 * (`backend/internal/services/analytics_category_display.go`), which #1626 introduced for the same
 * failure mode on the analytics surface, and to the budgets UI's existing fallback.
 */
export const UNNAMED_CUSTOM_CATEGORY_LABEL = 'Custom category'
