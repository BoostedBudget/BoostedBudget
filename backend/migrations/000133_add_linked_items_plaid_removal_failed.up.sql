-- Issue #1494: record "Plaid /item/remove can NEVER succeed for this item", as a fact DISTINCT
-- from "Plaid /item/remove succeeded" (000131's plaid_removed_at).
--
-- WHY THIS CANNOT BE plaid_removed_at, and it is the whole design constraint. The motivating error
-- is Plaid INVALID_ACCESS_TOKEN: it says the TOKEN WE HOLD is unusable, and it says NOTHING about
-- whether the Item still exists at Plaid or is still billing. Writing plaid_removed_at for such a
-- row would assert "billing has stopped" on Plaid's behalf when Plaid never said so — recreating,
-- with the opposite polarity, exactly the defect 000131 closed (a GDPR erasure completing over a
-- live, billing Item). Two columns, two facts:
--
--   plaid_removed_at        -> we PROVED the Plaid side is done (removal succeeded, or Plaid
--                              answered ITEM_NOT_FOUND, i.e. the Item is already gone).
--   plaid_removal_failed_at -> we proved we can NEVER prove that: the credential we hold cannot be
--                              used to remove this Item, so retrying with it cannot change the
--                              outcome. The Item may still exist and may still bill.
--
-- WHAT THIS UNBLOCKS, and what it deliberately does not. DeleteUserCascade's C2 guard
-- (repository.uncleanedLinkedItemExistsClause) blocks a GDPR erasure while any of the user's rows
-- has plaid_removed_at IS NULL. Since 000131 that is fail-closed and correct, but for a
-- permanently-unremovable item it is also PERMANENT — issue #1494: the erasure can never complete.
-- This column lets the guard stop blocking on such a row, and it is safe to do so ONLY because the
-- cascade now copies every not-provably-removed row into plaid_orphaned_items (migration 000134)
-- inside the SAME transaction, before deleting anything. The knowledge of the live vendor resource
-- outlives the erasure; that is what makes unblocking something other than a silent leak.
--
-- IT IS NOT A LICENCE TO STOP TRYING. Nothing that SELECTS work reads this column: the cleanup
-- sweep (linked_item_repository.cleanupCandidateSelectQuery) and the stranded-cleanup backstop
-- (GetItemsPendingCleanup) both still key off plaid_removed_at IS NULL alone, so a row marked here
-- keeps being re-attempted for as long as it exists. That is deliberate, and it is the self-healing
-- property that makes a MISCLASSIFICATION survivable: if the error was in truth transient, a later
-- attempt succeeds, stamps plaid_removed_at, and every predicate stops caring about this column's
-- value (all of them test plaid_removed_at first). A column that both marked the work hopeless AND
-- removed it from every work queue would be the "state marked, work never happened, mark survives
-- forever" shape that four consecutive fixes on this branch each had to close.
--
-- plaid_removal_failed_at is therefore NOT cleared on a later success — it is retained as evidence
-- that a code we classified unrecoverable eventually succeeded, which is the only signal that would
-- reveal the classification was too wide. Contradictory-looking rows (both columns set) are read
-- unambiguously by every predicate, because plaid_removed_at IS NOT NULL alone settles them.
--
-- plaid_removal_error_code carries the Plaid error_code string (or a local sentinel for the
-- no-usable-credential case) so an operator can see WHY, and so a too-wide classification is
-- auditable after the fact. VARCHAR(100) matches linked_items.error_code (migration 000013), which
-- holds Plaid error codes for the sync path.
--
-- NOT PII: a Plaid error code is a fixed vocabulary string from Plaid's error reference, not user
-- data. It is copied into plaid_orphaned_items, which survives erasure, so this matters.
--
-- LOCK COST: metadata-only, no table rewrite. Two `ADD COLUMN <nullable> <type>` with NO DEFAULT —
-- that form has never rewritten the table on any PostgreSQL version (the PG 11 improvement was for
-- non-volatile DEFAULTs, which this deliberately does not use). ACCESS EXCLUSIVE is taken for the
-- catalog write only, for the instant of the ALTER. Asserted from PostgreSQL semantics, NOT from a
-- green Squawk run: this repo's .squawk.toml sets assume_in_transaction = true and carries a
-- twelve-rule excluded_rules list, and per .claude/skills/db-migration-guide Squawk lints
-- statements in isolation and cannot see transaction-level or lock-duration hazards at all. The
-- evidence is the absence of a DEFAULT, not the linter.
--
-- NO BACKFILL: there is nothing to backfill. No pre-existing row can carry a fact nothing ever
-- recorded, and the correct default for "have we proved this is unremovable?" is NULL (no, keep
-- trying) for every existing row. Contrast 000132, which had a defensible proxy for its column.
--
-- NO INDEX: the only predicate that reads this column is C2's EXISTS, which is already narrowed by
-- an equality on user_id; this is a cheap filter on that narrow set, not a scan driver. Same
-- reasoning as 000131's.
--
-- Idempotent: ADD COLUMN IF NOT EXISTS, so the ALTER can also be run ahead of time in its own
-- short transaction during a low-traffic window (the operator escape hatch db-migration-guide
-- documents), after which these statements are no-ops.
ALTER TABLE linked_items ADD COLUMN IF NOT EXISTS plaid_removal_failed_at TIMESTAMPTZ;
ALTER TABLE linked_items ADD COLUMN IF NOT EXISTS plaid_removal_error_code VARCHAR(100);

COMMENT ON COLUMN linked_items.plaid_removal_failed_at IS
    'When a Plaid /item/remove attempt for this item failed with an error we judge UNRECOVERABLE (see services.terminalPlaidRemovalFailure for the exact, deliberately narrow code set). Means "the credential we hold cannot remove this Item, so retrying with it cannot change the outcome" — it does NOT mean the Item is gone, and it does NOT mean billing stopped; only plaid_removed_at means that. Overwritten by each later terminal attempt, so it is the MOST RECENT such attempt. Read by DeleteUserCascade''s C2 guard (uncleanedLinkedItemExistsClause) to stop blocking a GDPR erasure forever, which is only safe because the cascade first copies the row into plaid_orphaned_items. Deliberately NOT read by any predicate that selects cleanup work, so a marked row keeps being re-attempted while it exists — and is deliberately NOT cleared when a later attempt succeeds, so a too-wide classification stays visible. Issue #1494.';

COMMENT ON COLUMN linked_items.plaid_removal_error_code IS
    'The Plaid error_code (e.g. ''INVALID_ACCESS_TOKEN'') that caused plaid_removal_failed_at to be set, or the local sentinel ''NO_USABLE_ACCESS_TOKEN'' when this row carries no access token to send at all. Fixed-vocabulary diagnostic, not user data — it is copied into plaid_orphaned_items, which survives the user''s erasure. NULL whenever plaid_removal_failed_at is NULL. Issue #1494.';
