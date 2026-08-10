-- Reverse 000142 by restoring the sweep_cursors.exclusive comment as 000140 shipped it, verbatim --
-- the same convention 000141, 000136 and 000129 follow: "down" for a comment-only migration means
-- putting the previous wording back, not dropping the comment or the column it describes (both are
-- owned by 000140 and its own down migration).
--
-- READ THIS BEFORE RUNNING IT. The text restored below is the wording 000142 exists to replace. It is
-- not merely terser: 000140's narrative around it claimed the sacrificed rows of an over-cap tie group
-- were reached "until the next wrap", and a wrap restarts at the beginning and re-reads the same first
-- cap-worth, so those rows are reached on no run at all. Rolling this back reinstates the catalogue
-- comment that belongs to that false recovery claim. Roll back only as part of rolling 000142 out of
-- the tree, never as a way of "restoring" the column documentation.
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
          AND column_name = 'exclusive'
    ) THEN
        COMMENT ON COLUMN sweep_cursors.exclusive IS
        'FALSE = resume at this timestamp key (re-visits boundary ties, never skips one). TRUE = resume strictly '
        'after it; set only when a whole capped run was consumed by a single timestamp key, to guarantee forward progress.';
    END IF;
END $$;
