-- Reverse 000141 by restoring the sweep_cursors.ts2 comment as 000140 ORIGINALLY shipped it,
-- verbatim -- the same convention 000136 and 000129 follow: "down" for a comment-only migration
-- means putting the previous wording back, not dropping the comment or the column it describes
-- (both are owned by 000140 and its own down migration).
--
-- READ THIS BEFORE RUNNING IT. The text restored below is the INCOMPLETE wording 000141 exists to
-- replace: it says ts2 is "NULL for the two user sweeps" when three of the four sweeps leave it NULL,
-- and it enumerates by count -- the formulation that rotted in the first place. Rolling this back
-- therefore reinstates a catalogue comment that under-describes the column. Roll back only as part of
-- rolling 000141 out of the tree, never as a way of "restoring" the column documentation.
--
-- ⚠ DO NOT grep 000140 FOR THE TEXT BELOW -- IT IS NO LONGER THERE. 000140's own file has
-- since been corrected in place to carry the up migration's wording (see 000140's header), so
-- "as 000140 originally shipped it" means the text 000140 APPLIED to the databases this rollback
-- targets, not the text that file holds today. That is also why this rollback must keep the old
-- wording: on an already-migrated database it is the genuine previous catalogue state. On a
-- database created from the corrected 000140 it is not a reversal but a regression, so run it
-- only as part of rolling this migration out of the tree, exactly as the paragraph above says.
--
-- Guarded exactly as up.sql is, so this is a no-op rather than an error on a database where 000140 was
-- rolled back first and the table or column is gone.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'sweep_cursors'
          AND column_name = 'ts2'
    ) THEN
        COMMENT ON COLUMN sweep_cursors.ts2 IS
        'Second non-identity ORDER BY component, used only by the item_cleanup sweep (last_synced_at); NULL for the two user sweeps.';
    END IF;
END $$;
