-- #1569 gap G12: record the tags a whole-array replace destroys.
--
-- WHAT WAS UNRECORDED. TransactionRepository.UpdateTags is `SET tags = $1, tags_source = $2` — a
-- REPLACE, not a merge. Every tag dropped from the array is gone the moment it returns, and nothing
-- else in the system holds the previous set. It carried no capture at all, and the guard that
-- exists to notice that (TestAnnotationCaptureIsUsedByEveryAnnotationWritingStatement) could not
-- see it: its tracked-column universe is derived from annotationCapturedColumns, which listed only
-- the category and review columns. UpdateTags was in neither the captured set nor the exemption
-- list — not excused, invisible.
--
-- WHY TAGS COUNT AS AN ANNOTATION, when #1579's own text says "category/review overwrite". The
-- codebase had already answered it: the settlement move (#1578) carries `tags` and `tags_source`
-- onto the posted twin as part of the user's work, two of the twelve columns it moves, and
-- PullForwardSettledPendingAnnotations (#1586) carries them the other way. A column worth following
-- a transaction from one row to another because the user authored it is worth recording when it is
-- overwritten.
--
-- WHY A FIFTH change_source AND NOT 'category'. Same reason 'settlement' and 'erasure' are their
-- own: change_source answers "what did the user ask for". They asked to retag; they did not ask to
-- recategorise, and every category column on a 'tags' row is unchanged by construction.
--
-- JSONB, matching transactions.tags / transactions.tags_source exactly (migration 000018). NULLABLE
-- rather than DEFAULT '[]': a row written before this migration has no recorded tag state, and NULL
-- is the honest value for "not recorded" — '[]' would claim the user had no tags.
ALTER TABLE transaction_annotation_history
    ADD COLUMN IF NOT EXISTS prior_tags        JSONB,
    ADD COLUMN IF NOT EXISTS new_tags          JSONB,
    ADD COLUMN IF NOT EXISTS prior_tags_source JSONB,
    ADD COLUMN IF NOT EXISTS new_tags_source   JSONB;

-- squawk-ignore prefer-robust-stmts
ALTER TABLE transaction_annotation_history
    DROP CONSTRAINT IF EXISTS chk_transaction_annotation_history_source;
-- squawk-ignore constraint-missing-not-valid
ALTER TABLE transaction_annotation_history
    ADD CONSTRAINT chk_transaction_annotation_history_source
    CHECK (change_source IN ('category', 'review', 'settlement', 'erasure', 'tags'));
