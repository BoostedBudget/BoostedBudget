-- Issue #1497 item 2: let an OPERATOR record that they removed a stranded Plaid Item by hand in
-- Plaid's own dashboard — and keep that record permanently distinguishable from a proved removal.
--
-- ⚠ WHY THIS IS THREE NEW COLUMNS AND NOT A REUSE OF resolved_at, which is shorter and was the first
-- thing proposed. resolved_at has a documented, load-bearing meaning: "an automated /item/remove came
-- back success or ITEM_NOT_FOUND", i.e. PLAID ITSELF ANSWERED. It is written by exactly one statement
-- (MarkResolved), and that statement purges the retained credential in the same UPDATE because the
-- purpose the credential was retained for is spent the instant the removal is PROVED.
--
-- An operator saying "I handled this in Plaid's dashboard" is a different kind of fact in every
-- respect that matters here:
--
--   * IT IS UNVERIFIED. Nothing checked it, and nothing can check it retroactively — a human's memory
--     of a dashboard click is not an API response. Writing it into resolved_at would make the two
--     indistinguishable forever, which is precisely how linked_items.status = 'replaced' came to be
--     read as proof that an Item had been removed at Plaid (the defect this whole area exists to undo,
--     see 000133/000134). A claim recorded in a proof's column IS a false record, however well meant.
--   * IT LICENSES NO PURGE. resolved_at's write purges because a proved removal makes the credential
--     useless. A claim proves nothing, so it must not destroy the credential — and destroying it would
--     take away the only means by which the claim could ever be CONFIRMED, since a retry needs a
--     token. The credential's life is governed by token_expires_at and nothing else, as 000135 argued.
--   * IT MUST NAME SOMEBODY. A proof needs no author; a claim is worthless without one, because the
--     only way to check it later is to ask the person who made it. Hence operator_resolved_by and the
--     CHECK constraint below.
--
-- So: separate columns, and the derivation over them (domain.PlaidOrphanedItem.AdminState) reports a
-- fourth state, 'operator_attested', which ranks BELOW 'resolved' (proof beats claim) and ABOVE the
-- manual-intervention checks (an operator must not keep being told to do work they say they did).
--
-- ⚠ WHAT THIS MIGRATION DELIBERATELY DOES NOT DO: it adds no predicate anywhere. In particular the
-- automated retry sweep's selection (GetRetryablePage) is NOT changed to skip attested rows — that
-- decision is argued in full in that function's comment, and the short version is that continuing to
-- retry is the only mechanism that can ever upgrade an unverified claim into a proved removal, and it
-- self-heals: if the operator really did remove the Item, the next sweep gets ITEM_NOT_FOUND and
-- stamps resolved_at. Stopping the sweep on a claim would mean a mistaken or mis-clicked attestation
-- permanently silences the only automated path, on an Item that may still be billing.
--
-- ── WHAT IS AND IS NOT PERSONAL DATA HERE, because this table's whole design is about that ─────────
--
-- operator_resolved_by is the CLERK USER ID OF THE MEMBER OF STAFF who attested, and it is not in
-- tension with 000134's "carries NO user_id and no foreign key" rule. That rule is about the ERASED
-- DATA SUBJECT: the table must outlive them and must not remain a pointer to them. An identifier for
-- the employee who performed an administrative action is the opposite kind of record — it is
-- accountability for OUR action, of the same species as audit_logs.actor_user_id, and it concerns a
-- person whose data is not being erased. It is stored in PLAINTEXT for that reason: it is not the
-- subject's data, it is queried and displayed by the admin surface, and encrypting it would buy
-- nothing while making it unreadable to an operator reviewing who attested what.
--
-- ⚠ operator_resolution_note IS FREE TEXT WRITTEN BY STAFF, so it is the one column in this table
-- where the erased user's PII could re-enter by accident (somebody typing "removed Jane Doe's Chase
-- Item"). It cannot be prevented in the schema, and deliberately is not validated in code either — a
-- filter would give false assurance while mangling legitimate notes. The mitigations are that it is
-- reachable only from an admin-authenticated surface, that it is never exported to a data subject or
-- an analytics pipeline, and that the note is the smaller risk than the alternative it replaces: an
-- anonymous, unexplained resolution nobody could later check.
--
-- ⚠ THE SESSION-REPLAY MITIGATION WAS ARGUED HERE BEFORE IT EXISTED. This comment originally
-- claimed the note was "masked in PostHog session replay like every other value on that page".
-- Both halves were false: the note was rendered in a DataTable column absent from
-- `maskedColumnIds`, and most values on that page are deliberately NOT masked (the state, the
-- timestamps, the error codes and the attesting staff id all stay legible on purpose). The claim
-- is now true, and narrower: `data-ph-mask` sits on the note element itself in
-- PlaidOrphanedItemsPage.tsx — not on the column, which would take the attester with it and make
-- the attestation anonymous — and on the dialog textarea that writes it. Enforced by the Round-15
-- PII regression in apps/web/src/test/pii-coverage.test.tsx. Nothing else on that page is claimed
-- to be masked beyond the Plaid Item id and the institution name.
--
-- ── LOCK COST AND SQUAWK, argued from the semantics rather than from a green lint run ──────────────
--
-- ⚠ A GREEN SQUAWK RUN IS NOT EVIDENCE IN THIS REPO, and 000133/000134/000135 all say so:
-- .squawk.toml carries a 12-entry excluded_rules list, so several classes of hazard are simply not
-- reported. The per-statement justification therefore stands on its own:
--
--   * ADD COLUMN … IF NOT EXISTS, three times, all NULLABLE and with NO DEFAULT: metadata-only on
--     every supported PostgreSQL — no table rewrite, and the ACCESS EXCLUSIVE lock is held for the
--     catalogue update only. `adding-not-nullable-field` is an ENABLED rule and fires on NOT NULL
--     without a default; none of these three is NOT NULL, deliberately. NULL is the correct and
--     meaningful state for all of them: it means "no operator has attested", which is true of every
--     existing row and of the overwhelming majority of future ones. There is no backfill and none is
--     possible — inventing an attestation for a historical row would fabricate exactly the kind of
--     record this migration exists to keep honest.
--   * TWO CHECK CONSTRAINTS, added and validated in one step each. `constraint-missing-not-valid` is
--     in excluded_rules, so the linter would not have objected either way; the reason it is safe is
--     the same reason 000135 gave, re-verified rather than inherited: nothing prunes this table, but
--     nothing writes to it either except a completed erasure of a user with a permanently-unremovable
--     Plaid Item, so it holds single digits of rows and the validating scan is a scan of nothing.
--     (domain.PlaidOrphanedItemHealthScanCap is that size argument made mechanical — if the table
--     ever exceeds 500 rows the health readers refuse to report a number at all, which is the
--     alarming signal 000134 asked for in prose.) Both constraints are also trivially satisfied by
--     every existing row, since all three new columns are NULL everywhere: no validation can fail.
--   * NO NEW INDEX, unchanged from 000134/000135's argument. Nothing selects on these columns — the
--     write is by primary key and the reads are the same full scans that already exist — and a full
--     scan of single digits of rows costs less than an index costs to maintain, in I/O and in prose.
--
-- Idempotent: ADD COLUMN IF NOT EXISTS throughout, and each CHECK is guarded by a pg_constraint
-- lookup (ADD CONSTRAINT has no IF NOT EXISTS). COMMENTs are unconditional, which is safe — setting
-- the same comment twice leaves the same comment.

ALTER TABLE plaid_orphaned_items
    ADD COLUMN IF NOT EXISTS operator_resolved_at      TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS operator_resolved_by      VARCHAR(255),
    ADD COLUMN IF NOT EXISTS operator_resolution_note  TEXT;

-- CONSTRAINT 1: AN ATTESTATION IS NEVER ANONYMOUS.
--
-- The entire mitigation for accepting an unverified human claim is that the claim is attributable: the
-- only way anyone can ever check it is to go and ask the person who made it. An attestation with a
-- timestamp and no attester would be a resolution nobody is answerable for — which is functionally the
-- anonymous green tick this feature was designed not to produce. The converse direction is enforced
-- too (an attester with no timestamp is meaningless bookkeeping), so the pair moves together or not at
-- all, and the database refuses every partial write rather than trusting each future UPDATE to be
-- careful. Same reasoning as 000135's plaid_orphaned_items_purged_token_is_null.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'plaid_orphaned_items_operator_attestation_is_attributed'
          AND conrelid = 'plaid_orphaned_items'::regclass
    ) THEN
        ALTER TABLE plaid_orphaned_items
            ADD CONSTRAINT plaid_orphaned_items_operator_attestation_is_attributed
            CHECK ((operator_resolved_at IS NULL) = (operator_resolved_by IS NULL));
    END IF;
END $$;

-- CONSTRAINT 2: A NOTE CANNOT EXIST WITHOUT THE ATTESTATION IT EXPLAINS.
--
-- Not decoration: without it, a note could be written on a row nobody has attested, and it would then
-- read on the admin surface as an explanation of a resolution that never happened. The note is
-- evidence ABOUT the claim and has no meaning apart from it. (The reverse is allowed — an attestation
-- with a NULL note is legal in the schema. The API requires a non-empty note, because an unexplained
-- claim is nearly useless to whoever has to check it later, but that is an API-level rule about new
-- writes and not something to impose on the schema, which must also accept whatever a future
-- backfill or manual repair legitimately needs.)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'plaid_orphaned_items_operator_note_needs_an_attestation'
          AND conrelid = 'plaid_orphaned_items'::regclass
    ) THEN
        ALTER TABLE plaid_orphaned_items
            ADD CONSTRAINT plaid_orphaned_items_operator_note_needs_an_attestation
            CHECK (operator_resolved_at IS NOT NULL OR operator_resolution_note IS NULL);
    END IF;
END $$;

COMMENT ON COLUMN plaid_orphaned_items.operator_resolved_at IS
    'When an OPERATOR CLAIMED to have removed this Item by hand in Plaid''s own dashboard. '
    'AN UNVERIFIED HUMAN ATTESTATION, AND DELIBERATELY NOT resolved_at: that column means an '
    'automated /item/remove came back success or ITEM_NOT_FOUND -- Plaid itself answered -- whereas '
    'this one means somebody typed a sentence. Nothing checked it and nothing can check it '
    'retroactively, so the two facts are kept in separate columns forever; recording a claim in a '
    'proof''s column is how linked_items.status = ''replaced'' came to be believed as proof of removal '
    'at Plaid, which is the defect this table exists to undo. Consequences, all deliberate: it does '
    'NOT purge access_token_encrypted, it does NOT set token_purged_at, it does NOT set '
    'plaid_removed_at anywhere, and it does NOT remove the row from the automated retry sweep''s '
    'selection (GetRetryablePage) -- the sweep is the only thing that can ever upgrade this claim into '
    'a proof, and it self-heals via ITEM_NOT_FOUND if the operator really did remove the Item. NULL '
    'means no operator has attested, which is true of every row written before this migration and of '
    'almost every row after it. First-wins: once set it is never overwritten, so "who said this, and '
    'when" stays answerable. Read by domain.PlaidOrphanedItem.AdminState as the fourth state, '
    '''operator_attested'', ranked below ''resolved'' and above every manual-intervention check. '
    'Issue #1497 item 2.';

COMMENT ON COLUMN plaid_orphaned_items.operator_resolved_by IS
    'Clerk user id of the MEMBER OF STAFF who made the attestation in operator_resolved_at. '
    'NOT the erased data subject''s id, and therefore not in tension with this table''s no-user-id '
    'rule (see the table comment): that rule exists so the table cannot remain a pointer to the '
    'person whose data was erased, while this is accountability for an action WE took, of the same '
    'species as audit_logs.actor_user_id, naming a person whose data is not being erased. Plaintext '
    'rather than encrypted for exactly that reason -- it is not the subject''s data, the admin surface '
    'displays it, and encrypting it would buy nothing. MANDATORY WHENEVER operator_resolved_at IS SET '
    '(CHECK plaid_orphaned_items_operator_attestation_is_attributed): the whole mitigation for '
    'accepting an unverified claim is that somebody is answerable for it, and an anonymous attestation '
    'is the unaccountable green tick this feature was designed not to produce. Issue #1497 item 2.';

COMMENT ON COLUMN plaid_orphaned_items.operator_resolution_note IS
    'Free text the attesting operator wrote about what they actually did at Plaid -- the evidence by '
    'which the claim in operator_resolved_at can be checked later. Only meaningful alongside an '
    'attestation, which CHECK plaid_orphaned_items_operator_note_needs_an_attestation enforces, '
    'because a note on an unattested row would read as the explanation of a resolution that never '
    'happened. NULLABLE in the schema (a legitimate repair or backfill may have no note) though the '
    'admin API requires a non-empty one for new writes. '
    '⚠ THE ONE COLUMN IN THIS TABLE WHERE THE ERASED USER''S PII COULD RE-ENTER BY ACCIDENT, since a '
    'human types it and could type a name. Not schema-preventable and deliberately not filtered in '
    'code either (a filter would give false assurance while mangling legitimate notes); the '
    'mitigations are that it is written and read only through an admin-authenticated surface, is never '
    'exported to a data subject or an analytics pipeline, and carries data-ph-mask on the element '
    'that renders it (and on the textarea that writes it), so PostHog session replay never records '
    'it -- narrower than the page as a whole, most of which is deliberately left legible. It is the '
    'smaller risk than what it replaces -- an unexplained resolution nobody could ever check. '
    'Issue #1497 item 2.';
