-- Reverse 000186 by restoring the parent_pfc_primary comment as 000065 ORIGINALLY shipped it,
-- verbatim -- the same convention 000184, 000175, 000141, 000142, 000136 and 000129 follow: "down"
-- for a comment-only migration means putting the previous wording back, not dropping the comment or
-- the column it describes (both are owned by 000065 and its own down migration).
--
-- READ THIS BEFORE RUNNING IT. The text restored below is the wording 000186 exists to replace, and
-- it is false in both directions: it calls the column a display hint when budget roll-up has always
-- resolved through it, and it says "does not restrict usage" when, since #1609, nesting a category
-- under TRANSFER_IN or TRANSFER_OUT removes its transactions from cash flow. Rolling this back
-- reinstates that claim. Roll back only as part of rolling 000186 out of the tree.
--
-- ⚠ DO NOT grep 000065 FOR THE TEXT BELOW -- IT IS NO LONGER THERE. That file's own up migration was
-- corrected in place in the same commit that added 000186, so "as it ORIGINALLY shipped" means the
-- text 000065 APPLIED to the databases this rollback targets, not the text the file holds today.
--
-- Guarded exactly as up.sql is, so this is a no-op rather than an error on a database where 000065
-- was rolled back first and the table or column is gone.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'custom_categories'
          AND column_name = 'parent_pfc_primary'
    ) THEN
        COMMENT ON COLUMN custom_categories.parent_pfc_primary IS
        'Optional PFCv2 primary hint for icon/color fallback resolution. Does not restrict usage.';
    END IF;
END $$;
