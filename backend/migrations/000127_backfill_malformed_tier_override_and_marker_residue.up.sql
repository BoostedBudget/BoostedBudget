-- Issue #1453: backfill malformed manual_tier_override rows to NULL, and null the
-- plaid_cleanup_pending_at residue left by migrations 000119/000120.
--
-- ─── Part 1: malformed manual_tier_override ────────────────────────────────────────────────────
--
-- users.manual_tier_override is an unconstrained VARCHAR(50) with no CHECK constraint (migration
-- 000046), so a value naming no configured tier can be stored — a legacy row, a direct SQL write, a
-- migration, or a write made while tierConfig was nil. PR #1443 made such a value HARMLESS at every
-- read path (SetOverrideTierValidator / isConfiguredOverrideTier reject it and fall back to the raw
-- subscription_status); this migration makes it UNREACHABLE going forward by clearing any that
-- already exist, on top of the write-time validation admin_user_service.go's UpdateUserTier already
-- enforces for every NEW write.
--
-- Deliberately NOT a CHECK constraint (Option 3 in the issue, rejected): the tier set lives in
-- config/subscription_tiers.yaml, not the database, so inlining it into schema would duplicate
-- application config and drift the next time a tier is added or renamed. This migration instead
-- takes Option 1: backfill + keep the existing application-level write-time validation + a
-- monitoring query (RetentionCleanupCheckWorker's new malformed-override check, wired in
-- retention_cleanup_check.go, alongside profileRepository.GetUsersWithMalformedTierOverride).
--
-- Hardcoding the CURRENT tier list here (unlike a perpetual CHECK) carries no drift risk: this is a
-- ONE-TIME point-in-time cleanup that never runs again once applied, so a tier renamed or added
-- afterward cannot make this migration's own logic stale — only the ongoing monitoring query (which
-- reads the SAME tierConfig every other validator uses, never a hardcoded SQL list) has to stay
-- current, and it does so by construction.
--
-- There are no production users yet at the time this migration was written — a documented premise
-- for deciding to backfill directly rather than run a read-only audit first (the ticket's own
-- caution about "the count is unknown and clearing changes those users' effective tier" does not
-- apply). That premise does not relax the backfill's own correctness: it must remain exactly right
-- if this ever runs against a populated table in a lower environment.
--
-- Clears manual_tier_ends_at alongside manual_tier_override — an end date paired with no override
-- value is meaningless, and RemoveUserTierOverride (admin_user_repository.go) already clears both
-- columns together for the same reason on the ordinary removal path.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'users'
          AND column_name = 'manual_tier_override'
    ) THEN
        UPDATE users
        SET manual_tier_override = NULL,
            manual_tier_ends_at = NULL,
            updated_at = NOW()
        WHERE manual_tier_override IS NOT NULL
          AND manual_tier_override <> ''
          AND manual_tier_override NOT IN (
              'premium', 'household_2', 'household_3', 'household_4', 'household_5'
          );
    END IF;
END $$;

-- ─── Part 2: plaid_cleanup_pending_at residue (zero linked_items rows at all) ──────────────────
--
-- Migrations 000119/000120 stamped plaid_cleanup_pending_at on EVERY inactive/cancelled,
-- non-comped user, regardless of whether that user ever linked a single Plaid item. A marker on a
-- user with ZERO linked_items rows can never be cleared by ClearCompletedPlaidCleanupMarkers
-- (profile_repository.go) — its anyLinkedItemExistsClause guard deliberately leaves that exact
-- population alone, since a zero-row user is indistinguishable at the row level from an ordinary
-- never-linked-Plaid cancellation (the #1453 allowance, unchanged and still legal after this
-- migration — see domain.User.PlaidCleanupPendingAt's doc comment). So this residue sits stamped
-- forever with nothing in the runtime backstops ever revisiting it.
--
-- Harmless today: GetOutstandingPlaidCleanupStats' backlog gauge and every repair-candidate query
-- join on live linked_items, so a zero-row user is never selected or counted regardless of marker
-- state. But it means "marker non-NULL" is NOT a usable proxy for "cleanup outstanding" — a trap for
-- any future query that assumes otherwise. Nulling this one-time residue removes the trap: the
-- remaining non-NULL population after this migration is always backed by at least one genuine
-- linked_items row.
--
-- Scoped identically to ClearCompletedPlaidCleanupMarkers's own WHERE clause, MINUS its
-- "anyLinkedItemExistsClause" requirement (replaced by its exact negation, zero rows at all) — the
-- complementary population that method's own guard deliberately excludes from its ongoing runtime
-- responsibility. Deliberately does NOT exclude a currently-comped user, mirroring
-- ClearCompletedPlaidCleanupMarkers's own asymmetry with StampMissingPlaidCleanupMarkers (see that
-- method's doc comment): this corrects a fact independent of override state, not a runtime candidate
-- selection.
--
-- ...and by that SAME reasoning it deliberately does NOT exclude a user with a pending deletion
-- either, though ClearCompletedPlaidCleanupMarkers does. Found by a Codex adversarial review of this
-- branch: an earlier draft carried that method's `deletion_requested_at IS NULL` guard across too,
-- which left inactive/cancelled + zero-linked-item + deletion-pending users stamped forever and made
-- the invariant claimed above ("the remaining non-NULL population is always backed by at least one
-- genuine linked_items row") FALSE — reintroducing, for that population, the exact trap this
-- statement exists to remove.
--
-- The two guards are excluded for different reasons, and only one of them transfers:
--   * ClearCompletedPlaidCleanupMarkers skips deletion-pending users because during the 30-day
--     deletion grace the deletion flow OWNS Plaid cleanup, and the repair pass must stay out of its
--     way (the #1449 H6 split). That is runtime candidate selection.
--   * This statement corrects a historical data artifact. A user with zero linked_items has nothing
--     to clean up in the first place, so their marker is meaningless regardless of deletion state,
--     and nulling it cannot race the deletion flow — verified: neither DeletionService nor
--     DeletionWorker reads plaid_cleanup_pending_at at all.
-- Note ClearMarkersOnEligibleStatus does not cover these rows either: its predicate is
-- NOT(inactive/cancelled), and these users ARE inactive/cancelled, so branch 1 never sees them.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'users'
          AND column_name = 'plaid_cleanup_pending_at'
    ) THEN
        UPDATE users
        SET plaid_cleanup_pending_at = NULL,
            updated_at = NOW()
        WHERE subscription_status IN ('inactive', 'cancelled')
          AND plaid_cleanup_pending_at IS NOT NULL
          AND is_deleted = FALSE
          AND NOT EXISTS (
              SELECT 1 FROM linked_items WHERE linked_items.user_id = users.id
          );
    END IF;
END $$;
