-- Issue #1497 item 2, follow-up: make "an attestation is never anonymous" TRUE, not merely stated.
--
-- WHAT WAS WRONG. 000139's predecessor, 000137, added
-- plaid_orphaned_items_operator_attestation_is_attributed as
--
--     CHECK ((operator_resolved_at IS NULL) = (operator_resolved_by IS NULL))
--
-- which enforces NULL PARITY and nothing else. So (now(), '') and (now(), '   ') both pass. 000137's
-- own prose argues, at length, that "the entire mitigation for accepting an unverified human claim is
-- that the claim is attributable" and that a timestamp with no attester is "functionally the anonymous
-- green tick this feature was designed not to produce" — and then the constraint permits exactly that,
-- because '' IS NOT NULL. The invariant was advertised as STRUCTURAL and was not.
--
-- NOT EXPLOITABLE THROUGH THE API TODAY, and that is not the point. AdminHandler.
-- RecordPlaidOrphanedItemOperatorResolution takes the attester from the resolved Clerk identity and
-- refuses an empty one, and plaidOrphanedItemRepository.RecordOperatorResolution refuses "" as well.
-- Both are code-level guards on ONE writer. The constraint is what covers every other writer there
-- will ever be — a psql session repairing a row by hand, a backfill, a second service, a future
-- endpoint that takes the attester from somewhere else — and it is the only guard that a code change
-- cannot silently remove. A database guarantee that holds only when the application is careful is the
-- shape of claim this whole table exists to stop making.
--
-- WHY A NEW MIGRATION RATHER THAN AN EDIT TO 000137. Editing an applied migration's CHECK changes
-- nothing anywhere it has already run: golang-migrate records 137 as applied and never re-reads the
-- file, so every existing database would keep the weak constraint while the repository's file said
-- otherwise — the failure mode that already happened once on this branch with a corrected COMMENT ON
-- (000136 exists to reissue comments for that exact reason). A constraint is worse than a comment: the
-- comment merely read wrong, this would BE wrong, silently, and only in the environments that matter.
--
-- WHAT "BLANK" MEANS HERE. `~ '[^[:space:]]'` — "contains at least one non-whitespace character".
-- Deliberately a regex over the POSIX class rather than `btrim(x) <> ''`: btrim's default trim set is
-- the space character alone, so btrim(E'\t') is E'\t' and a tab-only attester would still pass. The
-- class covers space, tab, newline, vertical tab, form feed and carriage return, which is Go's
-- strings.TrimSpace less the two Unicode additions (U+0085, U+00A0). That residue is accepted
-- knowingly: those two are not reachable from any writer here, and the repository trims with
-- strings.TrimSpace BEFORE writing, so the strictly-stricter side is the application's. The database's
-- job is to make the anonymous case impossible, not to be byte-identical to Go's definition of space.
--
-- CONSTRAINT 3 IS NEW, AND IT IS THE SAME DEFECT ONE COLUMN OVER. 000137 permitted
-- operator_resolution_note = '' beside an attestation. An empty-string note and an absent note are
-- indistinguishable to a reader and are stored differently, which is precisely the distinction that
-- rots: the admin surface renders one as an empty explanation and the other as no explanation, and
-- nothing keeps two writers agreeing on which they meant. "No note" now has exactly one
-- representation, NULL, enforced here and normalised in nullIfEmptyNote on the way in.
--
-- LOCKING AND COST. Both statements take ACCESS EXCLUSIVE on plaid_orphaned_items and validate every
-- existing row. That is the same trade 000137 itself made and is acceptable for the same reason: this
-- table holds single digits of rows (one per Plaid Item stranded by a GDPR erasure), and no row
-- anywhere currently has a non-NULL operator_resolved_by at all, so validation cannot fail. NOT VALID
-- is deliberately not used — a NOT VALID constraint does not check existing rows, and the whole
-- purpose here is that the guarantee is unconditional. squawk's constraint-missing-not-valid is in
-- .squawk.toml's excluded_rules, so this is a documented allowance rather than a lint escape.
--
-- Idempotent: DROP CONSTRAINT IF EXISTS immediately followed by ADD CONSTRAINT is re-runnable by
-- construction (the drop clears whichever version is present, the add re-creates the tightened one),
-- and constraint 3 is guarded by a pg_constraint lookup because it has nothing to replace. The name of
-- constraint 1 is deliberately UNCHANGED: it is what appears in the 23514 error text, it is what the
-- integration tests assert on, and renaming it would make an old error message stop matching for no
-- gain.

ALTER TABLE plaid_orphaned_items
    DROP CONSTRAINT IF EXISTS plaid_orphaned_items_operator_attestation_is_attributed;

ALTER TABLE plaid_orphaned_items
    ADD CONSTRAINT plaid_orphaned_items_operator_attestation_is_attributed
    CHECK (
        (operator_resolved_at IS NULL) = (operator_resolved_by IS NULL)
        AND (operator_resolved_by IS NULL OR operator_resolved_by ~ '[^[:space:]]')
    );

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'plaid_orphaned_items_operator_note_is_not_blank'
          AND conrelid = 'plaid_orphaned_items'::regclass
    ) THEN
        ALTER TABLE plaid_orphaned_items
            ADD CONSTRAINT plaid_orphaned_items_operator_note_is_not_blank
            CHECK (operator_resolution_note IS NULL OR operator_resolution_note ~ '[^[:space:]]');
    END IF;
END $$;

COMMENT ON CONSTRAINT plaid_orphaned_items_operator_attestation_is_attributed ON plaid_orphaned_items IS
    'An attestation is never anonymous, structurally. operator_resolved_at and operator_resolved_by '
    'are NULL together or set together, AND a set operator_resolved_by contains at least one '
    'non-whitespace character. The second half is what 000137 stated in prose and did not enforce: '
    '(now(), '''') satisfied NULL parity. The only mitigation for storing an UNVERIFIED human claim is '
    'that somebody is answerable for it, so an attestation nobody is named on would be the anonymous '
    'green tick this table exists to prevent. Issue #1497 item 2, tightened in 000139.';

COMMENT ON CONSTRAINT plaid_orphaned_items_operator_note_is_not_blank ON plaid_orphaned_items IS
    '"No note" has exactly one representation: NULL. A whitespace-only or empty note is refused rather '
    'than stored, because an empty string and an absent value read identically to a human and '
    'differently to code, and nothing keeps two writers agreeing on which they meant. The API already '
    'requires a non-empty note of new writes and the repository normalises a blank one to NULL; this '
    'is the same rule for every other writer. Issue #1497 item 2, added in 000139.';
