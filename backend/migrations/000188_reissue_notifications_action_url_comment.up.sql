-- Issue #1609: RE-ISSUE the COMMENT on notifications.action_url. The wording 000014 shipped
-- described the reconnect action URL as carrying a link_token:
--
--     'URL for user to take action (e.g., /accounts/reconnect?link_token=TOKEN&item_id=ITEM_ID)'
--
-- That was already misleading in two ways independent of the route-existence bug #1609 fixes
-- elsewhere: (1) /accounts/reconnect never existed on the frontend, so the token this comment
-- described was always delivered to a dead page; and (2) even once the route exists, embedding a
-- Plaid Link token in this column is itself wrong -- the token expires in ~4h while this row is
-- retained indefinitely (Financial History Is Retained by Default), so a notification read the
-- next morning carried a dead token, the token sat in a DB row and was re-sent on every
-- notification fetch, and email_service.go logged action_url verbatim, putting the token in
-- server logs. item_error_service.go's three Handle* methods no longer embed one; the reconnect
-- page now mints a fresh update-mode token on demand via POST /api/v1/plaid/reconnect-link-token.
--
-- WHY A NEW FILE AND ALSO AN EDIT TO 000014. Correcting an already-applied COMMENT takes TWO
-- edits, the convention 000174/000175/000184/000186 follow and TestReissuedColumnCommentsDoNotDrift
-- enforces: the ORIGINAL file (000014) is what a FRESH database gets and has been edited in place
-- in the same commit as this file; THIS file is what every already-migrated database gets, since
-- golang-migrate never re-runs an applied version. The guard compares the two bodies with
-- whitespace COLLAPSED -- re-wrapping across lines is not drift, but any change of word or
-- punctuation is. Edit both or neither.
--
-- SCOPE: one comment. No column, constraint, index, default, type or datum is touched, and no
-- behaviour changes here -- the behaviour change is entirely in Go (item_error_service.go and the
-- new reconnect endpoints). Squawk: COMMENT ON COLUMN takes only the ordinary catalogue lock on
-- the relation, rewrites no heap or index, adds no constraint, sets no default and validates
-- nothing, so every rule this repo excludes is INAPPLICABLE here rather than merely silenced.
--
-- Idempotent: COMMENT is inherently repeatable, and the statement is guarded on the column still
-- existing, so this is a clean no-op on a database where 000014 was rolled back.

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
        'URL for user to take action (e.g., /accounts/reconnect?item_id=ITEM_ID). Issue #1609: '
        'deliberately does NOT carry a link_token. A Plaid Link token expires in ~4h while this row '
        'is retained indefinitely, so a token embedded here could be stale by the time the user acts '
        'on the notification, would be persisted in this row, and would be logged verbatim by the '
        'email step (email_service.go). The reconnect page mints a fresh update-mode token on demand '
        'instead (POST /api/v1/plaid/reconnect-link-token).';
    END IF;
END $$;
