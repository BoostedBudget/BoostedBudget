-- Reverse 000188 by restoring the action_url comment as 000014 ORIGINALLY shipped it, verbatim --
-- the same convention 000184, 000186, 000175, 000141, 000142, 000136 and 000129 follow: "down"
-- for a comment-only migration means putting the previous wording back, not dropping the comment
-- or the column it describes (both are owned by 000014 and its own down migration).
--
-- READ THIS BEFORE RUNNING IT. The text restored below is the wording 000188 exists to replace,
-- and it describes a link_token query parameter that Handle* no longer embeds and that the
-- /accounts/reconnect page never accepts. Roll back only as part of rolling 000188 out of the
-- tree.
--
-- DO NOT grep 000014 FOR THE TEXT BELOW -- IT IS NO LONGER THERE. That file's own up migration was
-- corrected in place in the same commit that added 000188, so "as it ORIGINALLY shipped" means the
-- text 000014 APPLIED to the databases this rollback targets, not the text the file holds today.
--
-- Guarded exactly as up.sql is, so this is a no-op rather than an error on a database where 000014
-- was rolled back first and the table or column is gone.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'notifications'
          AND column_name = 'action_url'
    ) THEN
        COMMENT ON COLUMN notifications.action_url IS
        'URL for user to take action (e.g., /accounts/reconnect?link_token=TOKEN&item_id=ITEM_ID)';
    END IF;
END $$;
