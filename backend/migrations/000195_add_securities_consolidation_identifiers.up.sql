-- #1631: Store the two identifiers Plaid gives us for consolidating forked security identities.
--
-- institution_security_id: "the identifier given to the security by the institution" (Plaid
-- docs). Independent of plaid_security_id (our unique key, see the comment on
-- domain.Security.PlaidSecurityID), which is only "typically" stable and can change on a
-- corporate action — silently forking one real security into multiple `securities` rows, each
-- with its own transaction history. This column gives a future consolidation something to join
-- forked identities on. Actually merging identities (rewriting investment_transactions.security_id)
-- is explicitly OUT of scope for this migration — the retention ADR forbids merging history as a
-- side effect of a display fix.
--
-- proxy_security_id: "the ID of another security whose performance resembles this security,
-- typically when the original security has low volume, or when a private security can be modeled
-- with a publicly traded security" (Plaid docs). Stored for future use.
--
-- Both columns are stored, unindexed, and NOT joined to anything by this migration.

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'securities' AND column_name = 'institution_security_id'
    ) THEN
        ALTER TABLE securities ADD COLUMN institution_security_id VARCHAR(255);
        COMMENT ON COLUMN securities.institution_security_id IS 'Identifier given to the security by the institution (Plaid). A second, institution-scoped identity independent of plaid_security_id -- stored so a future consolidation of corporate-action-forked security identities has something to join on. Nothing merges on this column today.';
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'securities' AND column_name = 'proxy_security_id'
    ) THEN
        ALTER TABLE securities ADD COLUMN proxy_security_id VARCHAR(255);
        COMMENT ON COLUMN securities.proxy_security_id IS 'The ID of another security whose performance resembles this one (Plaid), typically when the original has low volume or is a private security modeled with a publicly traded proxy. Stored for future use; nothing reads it today.';
    END IF;
END $$;
