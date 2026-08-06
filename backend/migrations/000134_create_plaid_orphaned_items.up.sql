-- Issue #1494: the record that OUTLIVES a GDPR erasure, so unblocking the erasure of a user whose
-- Plaid Item could not be removed does not mean losing all knowledge of that Item.
--
-- THE PROBLEM THIS EXISTS FOR. DeleteUserCascade DELETES the user's linked_items rows. Every marker
-- on such a row — plaid_removed_at (000131), plaid_removal_failed_at (000133) — dies with it. So the
-- moment C2 (uncleanedLinkedItemExistsClause) is allowed to stop blocking on a permanently
-- unremovable item, the cascade erases the only local record that a live, still-billing Plaid Item
-- ever existed: the exact outcome the C2 guard was tightened to prevent. This table is the
-- precondition that makes unblocking legitimate rather than a silent leak. The cascade writes it
-- INSIDE its own transaction, BEFORE the delete, so there is no ordering in which the row vanishes
-- and the record does not exist.
--
-- WHY A DEDICATED TABLE AND NOT audit_logs. audit_logs was the considered alternative and it has a
-- trap that disqualifies the obvious spelling: DeleteUserCascade's own erasure redaction is
-- `UPDATE audit_logs SET changes = NULL WHERE resource_id = $1 OR user_id = $1`, so an audit row
-- keyed to the USER would have its payload nulled by the very cascade it must outlive. Keying it to
-- the item instead would work, but a dedicated table is better on three counts: the cascade cannot
-- touch it at all (no user_id column exists here to match on), it holds a QUERYABLE work list rather
-- than a free-form JSON payload, and it gives the admin view a natural backing.
--
-- NO FOREIGN KEY, AND NO USER ID AT ALL. Not merely "no FK to users": there is deliberately no
-- user_id column, because the row must survive that user's erasure and must not be a surviving
-- pointer to an erased person. The GDPR position taken here, and it is narrow on purpose: ONE
-- opaque vendor resource identifier (plus the institution that issued it) outlives the erasure
-- because we remain obliged to act on that resource — to get the Item removed and the billing
-- stopped. Nothing else about the person is retained. linked_item_id is a random UUID that no
-- longer resolves to any row and identifies no person.
--
-- CIPHERTEXT, NOT PLAINTEXT, and the column names say so. linked_items.item_id, institution_id and
-- institution_name are encrypted at rest by the application (repository.FieldEncryptor —
-- linked_item_repository.go's encryptLinkedItemPIIFields), so the cascade's INSERT ... SELECT copies
-- the ENCRYPTED values verbatim. That is the point: the copy neither weakens nor strengthens the
-- protection those values already had, and it needs no key material inside a SQL statement. Readers
-- decrypt through the same FieldEncryptor. Do NOT "fix" these columns by storing plaintext.
--
-- WHAT IS DELIBERATELY NOT STORED: the access token. It is the one field that would let an automated
-- re-attempt call /item/remove after the erasure, and it is still the wrong thing to keep. First,
-- the population that reaches this table via the unblocked path is precisely the population whose
-- token is known unusable (INVALID_ACCESS_TOKEN, or no token at all) — the credential would be
-- retained to perform an operation it has already been proven incapable of performing. Second, a
-- bank-connection credential belonging to an erased user is a far larger retention commitment than
-- an opaque item identifier, and the narrow GDPR argument above would not cover it. The consequence,
-- stated rather than hidden: re-attempting removal for a row in THIS table is an operator action at
-- Plaid (dashboard/support, using plaid_item_id), not an API call this system can make. Automated
-- re-attempts remain available for the whole window BEFORE erasure, where the linked_items row and
-- its token still exist — that is what the cleanup sweep and the stranded-cleanup backstop keep
-- doing, indefinitely, precisely because plaid_removal_failed_at does not remove a row from their
-- candidate sets.
--
-- NO resolved_at / resolution_note COLUMN YET, deliberately: nothing would write or read it in this
-- migration's change, and an untested column that an operator is expected to trust is worse than an
-- absent one. It belongs with the admin surface (the follow-up ticket for the dashboard view and the
-- re-attempt action), which is where a writer for it exists.
--
-- NO INDEX beyond the two constraints below. This table is expected to hold single digits of rows
-- (it takes a permanently-unremovable Plaid Item AND a completed erasure to produce one); the admin
-- listing is a full scan of that, and a covering index would cost more to maintain in prose than it
-- could ever save in I/O. Revisit if it ever grows — which would itself be the alarming signal.
--
-- LOCK COST: CREATE TABLE takes no lock on any existing table. The UNIQUE constraint below is
-- created as part of the table, on an empty table, so Squawk's disallowed-unique-constraint rule
-- (which is about ALTER TABLE ADD CONSTRAINT UNIQUE taking ACCESS EXCLUSIVE while it builds an index
-- over existing rows) has nothing to fire on here. Stated from the semantics rather than from a
-- green lint run — see 000133's note on why a green Squawk run is not evidence in this repo.
--
-- Idempotent: CREATE TABLE IF NOT EXISTS. The COMMENTs are unconditional, which is safe: they are
-- idempotent by nature (setting a comment twice leaves the same comment).
CREATE TABLE IF NOT EXISTS plaid_orphaned_items (
    id                            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    linked_item_id                UUID        NOT NULL,
    plaid_item_id_encrypted       TEXT        NOT NULL,
    institution_id_encrypted      TEXT,
    institution_name_encrypted    TEXT,
    linked_item_status            VARCHAR(50),
    last_error_code               VARCHAR(100),
    last_attempted_at             TIMESTAMPTZ,
    orphaned_at                   TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT plaid_orphaned_items_unique_linked_item
        UNIQUE (linked_item_id)
);

COMMENT ON TABLE plaid_orphaned_items IS
    'Plaid Items that may still exist -- and may still bill -- at Plaid, whose local '
    'linked_items row was deleted by a GDPR erasure before removal could be proved. Written '
    'by DeleteUserCascade, inside the cascade transaction, for every linked_items row with '
    'plaid_removed_at IS NULL, BEFORE anything is deleted. This is a work list of outstanding '
    'vendor obligations, not an audit log: a row here means someone must still get this Item '
    'removed at Plaid. Deliberately carries NO user_id and no foreign key -- it must outlive '
    'the erased user and must not be a surviving pointer to them. Issue #1494.';

COMMENT ON COLUMN plaid_orphaned_items.linked_item_id IS
    'The linked_items.id the record was copied from. NOT a foreign key: the referenced row is '
    'deleted moments later by the same transaction, by design. Retained as the idempotency key '
    '(see the UNIQUE constraint) and as a correlation handle for log lines written before the '
    'erasure, which name that id. A random UUID -- it identifies no person and no longer '
    'resolves to any row.';

COMMENT ON COLUMN plaid_orphaned_items.plaid_item_id_encrypted IS
    'ENCRYPTED (FieldEncryptor) copy of linked_items.item_id -- the Plaid Item identifier, and '
    'the one value an operator needs to remove this Item at Plaid. Ciphertext because the '
    'source column is ciphertext and the cascade copies it verbatim; decrypt through the same '
    'FieldEncryptor. Do NOT store plaintext here.';

COMMENT ON COLUMN plaid_orphaned_items.institution_id_encrypted IS
    'ENCRYPTED copy of linked_items.institution_id (Plaid ''ins_...''), or NULL if the source '
    'row had none. Says which institution to contact; carries no personal data even in '
    'plaintext, but is kept encrypted so the copy neither weakens nor strengthens the source '
    'column''s protection.';

COMMENT ON COLUMN plaid_orphaned_items.institution_name_encrypted IS
    'ENCRYPTED copy of linked_items.institution_name, or NULL if the source row had none. '
    'Human-readable institution name for the admin view.';

COMMENT ON COLUMN plaid_orphaned_items.linked_item_status IS
    'linked_items.status at the moment of erasure, copied as plaintext (a fixed vocabulary, '
    'not user data). Diagnostic: ''replaced'' means the Item was a re-link leftover that no '
    'code path had ever sent to Plaid, which is a different operator story from an ''active'' '
    'row whose removal was attempted and failed.';

COMMENT ON COLUMN plaid_orphaned_items.last_error_code IS
    'linked_items.plaid_removal_error_code at the moment of erasure -- the Plaid error code '
    'that made removal terminal, or NULL if removal was never attempted for this row (the '
    '''replaced'' case above). Fixed-vocabulary diagnostic, not user data.';

COMMENT ON COLUMN plaid_orphaned_items.last_attempted_at IS
    'linked_items.plaid_removal_failed_at at the moment of erasure -- when the last terminal '
    'removal attempt happened, or NULL if removal was never attempted.';

COMMENT ON COLUMN plaid_orphaned_items.orphaned_at IS
    'When the erasure that stranded this Item ran. The only timestamp here that is a '
    'measurement of this table''s own write.';

COMMENT ON CONSTRAINT plaid_orphaned_items_unique_linked_item ON plaid_orphaned_items IS
    'Makes the cascade''s INSERT ... ON CONFLICT DO NOTHING idempotent. A second erasure for the '
    'same linked_items row cannot normally happen (the row is gone after the first), so this is '
    'a cheap guarantee against a partial-retry shape rather than a routine path -- and it means '
    'the INSERT can never be the statement that fails a retried cascade.';
